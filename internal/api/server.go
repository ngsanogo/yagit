// Package api exposes repository state over HTTP.
//
// Requests become calls on the repository registry, the history store and the
// git runner, and errors become JSON responses that hide nothing. Every route
// requires the authentication token.
//
// The history is never read through the runner here: it comes from the store,
// which holds the assignment between requests and would be paid for twice if a
// handler ran `git log` beside it. The working directory is the opposite case
// — nothing caches a `git status`, and staging is a command with an effect —
// so those routes drive the runner directly.
//
// Depends on repo, history and git. Nothing depends back on it.
package api

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/history"
	"github.com/ngsanogo/yagit/internal/repo"
	"github.com/ngsanogo/yagit/internal/watch"
)

// Options gathers the server's dependencies. A struct rather than a long
// argument list: at the call site, every value is named.
type Options struct {
	Registry       *repo.Registry
	Runner         *git.Runner
	Token          string
	AllowedOrigins []string
	Logger         *slog.Logger

	// Events is the one push channel of ADR 0007. Built by the caller rather
	// than here, because the git Runner publishes into it and the Runner
	// exists before this server does.
	Events *EventStream

	// Watcher follows the git directory of each open repository, so a commit
	// made in the user's own terminal reaches the interface. Optional: a
	// daemon whose watcher could not start still serves, and says so — an
	// interface that refreshes only when asked is worth more than none.
	Watcher *watch.Watcher

	// SecureCookies sets the Secure attribute on every session cookie. True
	// when the daemon serves HTTPS. False on plain HTTP, where a Secure cookie
	// would never be sent back — unless the request itself says the browser
	// reached yagit over TLS through a proxy in front, which earns the
	// attribute one cookie at a time (see reachedOverTLS).
	SecureCookies bool

	// Frontend serves everything that is not under /api: a proxy to Vite in
	// development, the embedded frontend in production. Required — a daemon
	// that serves no interface has no reason to exist, and a built-in
	// courtesy page would end up hiding a missing frontend.
	Frontend http.Handler

	// Development says the Frontend above is a proxy to a dev server rather
	// than the embedded bundle. It exists for one reason: the Content-Security
	// -Policy has to allow the inline module Vite injects into the HTML it
	// generates, and must not allow it anywhere else. See securityHeaders.
	Development bool
}

type Server struct {
	registry *repo.Registry

	// runner is the working directory's half of the package: status, diffs,
	// staging, discarding, committing, and one commit read whole. Never the
	// history — see the package doc.
	runner *git.Runner

	// history holds each repository's assigned commit graph between requests.
	//
	// Built here from the Runner rather than taken as an Option, unlike every
	// other collaborator: a store built on a different Runner would read the
	// history through commands the log panel never sees, and the interface's
	// promise that it shows every git command yagit runs would be quietly
	// false. Deriving it removes the way to get that wrong.
	history *history.Store

	// unwatched remembers which open repositories are not refreshing on their
	// own, so the interface can say so. See unwatched.go.
	unwatched *unwatched

	// deleted remembers the branch each open repository most recently deleted,
	// because that is the one undo the reflog cannot answer. See deleted.go.
	deleted *deletions

	events  *EventStream
	watcher *watch.Watcher

	// credentials counts attempts to present one: the session exchange, and
	// every request that arrives without a valid token. Built by rateLimit,
	// which is where the reasoning is, and read by requireToken.
	credentials *rateLimiter

	token          string
	allowedOrigins []string
	secureCookies  bool
	logger         *slog.Logger
	frontend       http.Handler
	development    bool
}

func NewServer(options Options) (*Server, error) {
	switch {
	case options.Registry == nil:
		return nil, errors.New("api: missing repository registry")
	case options.Runner == nil:
		return nil, errors.New("api: missing git runner")
	case options.Token == "":
		return nil, errors.New("api: empty token, which amounts to having none")
	case options.Logger == nil:
		return nil, errors.New("api: missing logger")
	case options.Frontend == nil:
		return nil, errors.New("api: no frontend handler")
	case options.Events == nil:
		return nil, errors.New("api: missing event stream")
	}

	return &Server{
		registry:       options.Registry,
		runner:         options.Runner,
		history:        history.NewStore(options.Runner),
		deleted:        newDeletions(),
		unwatched:      newUnwatched(),
		events:         options.Events,
		watcher:        options.Watcher,
		token:          options.Token,
		allowedOrigins: options.AllowedOrigins,
		secureCookies:  options.SecureCookies,
		logger:         options.Logger,
		frontend:       options.Frontend,
		development:    options.Development,
	}, nil
}

// Handler assembles the full routing.
//
// Wrapper order matters: logging sits on the outside, so that requests
// refused for a missing token show up in the traces too.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Session exchange sits outside the token gate: it is how a browser gets
	// its first credential without putting the secret in the URL.
	mux.HandleFunc("POST /api/session", s.handleCreateSession)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/health", s.handleHealth)
	// Before the {id} route, and it has to be: ServeMux would otherwise have
	// to decide whether "discover" is an identifier, and the more specific
	// pattern is the one that must win.
	protected.HandleFunc("GET /api/repos/discover", s.handleDiscoverRepos)
	protected.HandleFunc("GET /api/repos", s.handleListRepos)
	protected.HandleFunc("POST /api/repos", s.handleOpenRepo)
	protected.HandleFunc("POST /api/repos/clone/plan", s.handlePlanClone)
	protected.HandleFunc("POST /api/repos/clone", s.handleClone)
	// The other way to get a repository that was not there before. Beside
	// clone rather than under /repos alone, because POST /api/repos already
	// means "open one that exists" and a flag deciding between the two would
	// be one route doing two jobs.
	protected.HandleFunc("POST /api/repos/init/plan", s.handlePlanInit)
	protected.HandleFunc("POST /api/repos/init", s.handleInit)
	protected.HandleFunc("DELETE /api/repos/{id}", s.handleCloseRepo)
	protected.HandleFunc("GET /api/repos/{id}/commits", s.handleCommits)
	protected.HandleFunc("GET /api/repos/{id}/commits/{sha}", s.handleCommit)
	protected.HandleFunc("GET /api/repos/{id}/files/history", s.handleFileHistory)
	protected.HandleFunc("GET /api/repos/{id}/files/blame", s.handleBlame)
	protected.HandleFunc("GET /api/repos/{id}/files/line-history", s.handleLineHistory)
	protected.HandleFunc("GET /api/repos/{id}/refs", s.handleRefs)

	// A GET beside the commits it searches, and not a page of them: a search
	// answers with the commits that match and nothing about how they are
	// drawn, because a graph assigned over a filtered set would draw
	// connections the repository does not have (see search.go).
	protected.HandleFunc("GET /api/repos/{id}/search", s.handleSearch)

	// Moving HEAD. Named after the command it runs rather than after
	// "checkout", which in this API already means something else: /resolve
	// runs `git checkout --ours`, and two routes sharing a word for two
	// operations is how one ends up wired to the other.
	protected.HandleFunc("POST /api/repos/{id}/switch", s.handleSwitch)
	protected.HandleFunc("POST /api/repos/{id}/branches", s.handleCreateBranch)
	protected.HandleFunc("POST /api/repos/{id}/branches/rename", s.handleRenameBranch)
	protected.HandleFunc("POST /api/repos/{id}/branches/delete/plan", s.handlePlanDeleteBranch)
	protected.HandleFunc("POST /api/repos/{id}/branches/delete", s.handleDeleteBranch)
	protected.HandleFunc("POST /api/repos/{id}/tags", s.handleCreateTag)
	protected.HandleFunc("POST /api/repos/{id}/tags/delete/plan", s.handlePlanDeleteTag)
	protected.HandleFunc("POST /api/repos/{id}/tags/delete", s.handleDeleteTag)
	protected.HandleFunc("POST /api/repos/{id}/tags/push/plan", s.handlePlanPushTag)
	protected.HandleFunc("POST /api/repos/{id}/tags/push", s.handlePushTag)
	protected.HandleFunc("POST /api/repos/{id}/merge/plan", s.handlePlanMerge)
	protected.HandleFunc("POST /api/repos/{id}/merge", s.handleMerge)
	protected.HandleFunc("POST /api/repos/{id}/rebase/plan", s.handlePlanRebase)
	protected.HandleFunc("POST /api/repos/{id}/rebase", s.handleRebase)

	// Under /rebase because it is one, and a pair of its own because the two
	// take different questions: a rebase is aimed at a BRANCH and answers with
	// an outcome, while this is aimed at a COMMIT and answers with the range
	// after it, for a plan the client writes. See interactive.go.
	protected.HandleFunc("POST /api/repos/{id}/rebase/interactive/plan", s.handlePlanInteractiveRebase)
	protected.HandleFunc("POST /api/repos/{id}/rebase/interactive", s.handleInteractiveRebase)
	protected.HandleFunc("POST /api/repos/{id}/cherry-pick/plan", s.handlePlanCherryPick)
	protected.HandleFunc("POST /api/repos/{id}/cherry-pick", s.handleCherryPick)
	protected.HandleFunc("POST /api/repos/{id}/revert/plan", s.handlePlanRevert)
	protected.HandleFunc("POST /api/repos/{id}/revert", s.handleRevert)
	protected.HandleFunc("POST /api/repos/{id}/reset/plan", s.handlePlanReset)
	protected.HandleFunc("POST /api/repos/{id}/reset", s.handleReset)
	protected.HandleFunc("GET /api/repos/{id}/undo", s.handleUndoOffer)
	protected.HandleFunc("POST /api/repos/{id}/undo/plan", s.handlePlanUndo)
	protected.HandleFunc("POST /api/repos/{id}/undo", s.handleUndo)

	// The stash. Read as a collection, written as three operations, and the
	// two spellings are the split the rest of this table already uses:
	// /branches and /remotes are lists, /merge and /fetch are things done.
	//
	// `stash/push` shares a word with the `push` route further down and means
	// something else entirely — one saves the work tree, the other talks to a
	// server. They are told apart by the namespace rather than by renaming
	// either, because both are git's own verb for what they do and `git stash
	// save` is the spelling git deprecated. The handlers carry the namespace
	// too: handleStashPush is never one keystroke from handlePush.
	// The linked checkouts one repository has. A collection to read, two
	// operations to write, and a prune — the same split the stash below uses,
	// and for the same reason: a list is a noun and the rest are verbs.
	protected.HandleFunc("GET /api/repos/{id}/worktrees", s.handleWorktrees)
	protected.HandleFunc("POST /api/repos/{id}/worktrees/plan", s.handlePlanAddWorktree)
	protected.HandleFunc("POST /api/repos/{id}/worktrees", s.handleAddWorktree)
	protected.HandleFunc("POST /api/repos/{id}/worktrees/remove/plan", s.handlePlanRemoveWorktree)
	protected.HandleFunc("POST /api/repos/{id}/worktrees/remove", s.handleRemoveWorktree)
	protected.HandleFunc("POST /api/repos/{id}/worktrees/prune", s.handlePruneWorktrees)

	// The repositories this one pins. Same split again, and one route more
	// than the worktrees have: `git submodule sync` exists because .gitmodules
	// is under version control and the config that clones from it is not.
	protected.HandleFunc("GET /api/repos/{id}/submodules", s.handleSubmodules)
	protected.HandleFunc("POST /api/repos/{id}/submodules/plan", s.handlePlanAddSubmodule)
	protected.HandleFunc("POST /api/repos/{id}/submodules", s.handleAddSubmodule)
	protected.HandleFunc("POST /api/repos/{id}/submodules/update", s.handleUpdateSubmodules)
	protected.HandleFunc("POST /api/repos/{id}/submodules/sync", s.handleSyncSubmodules)
	protected.HandleFunc("POST /api/repos/{id}/submodules/remove/plan", s.handlePlanRemoveSubmodule)
	protected.HandleFunc("POST /api/repos/{id}/submodules/remove", s.handleRemoveSubmodule)

	// Large files. One read composed of two facts — whether git-lfs is on this
	// machine, and which patterns this repository's .gitattributes routes
	// through it — and the two commands that change the second. Nothing here
	// transfers anything: LFS is a git filter, so every fetch and push yagit
	// already drives carries its content.
	protected.HandleFunc("GET /api/repos/{id}/lfs", s.handleLFS)
	protected.HandleFunc("POST /api/repos/{id}/lfs/track/plan", s.handlePlanTrackLFS)
	protected.HandleFunc("POST /api/repos/{id}/lfs/track", s.handleTrackLFS)
	protected.HandleFunc("POST /api/repos/{id}/lfs/untrack/plan", s.handlePlanUntrackLFS)
	protected.HandleFunc("POST /api/repos/{id}/lfs/untrack", s.handleUntrackLFS)

	protected.HandleFunc("GET /api/repos/{id}/stashes", s.handleStashes)
	protected.HandleFunc("GET /api/repos/{id}/stashes/{index}", s.handleStashDetail)
	protected.HandleFunc("POST /api/repos/{id}/stash/push/plan", s.handlePlanStashPush)
	protected.HandleFunc("POST /api/repos/{id}/stash/push", s.handleStashPush)
	protected.HandleFunc("POST /api/repos/{id}/stash/apply/plan", s.handlePlanStashApply)
	protected.HandleFunc("POST /api/repos/{id}/stash/apply", s.handleStashApply)
	protected.HandleFunc("POST /api/repos/{id}/stash/drop/plan", s.handlePlanStashDrop)
	protected.HandleFunc("POST /api/repos/{id}/stash/drop", s.handleStashDrop)

	// The network. Fetch first because it is the one that changes nothing but
	// what the repository knows; the other two move a branch, and the plan
	// route beside them answers with the line a push would run rather than
	// running it — the same shape the discard and delete confirmations use,
	// for the same reason.
	protected.HandleFunc("GET /api/repos/{id}/remotes", s.handleRemotes)
	protected.HandleFunc("POST /api/repos/{id}/remotes", s.handleAddRemote)
	protected.HandleFunc("POST /api/repos/{id}/remotes/rename", s.handleRenameRemote)
	protected.HandleFunc("POST /api/repos/{id}/remotes/set-url/plan", s.handlePlanSetRemoteURL)
	protected.HandleFunc("POST /api/repos/{id}/remotes/set-url", s.handleSetRemoteURL)
	protected.HandleFunc("POST /api/repos/{id}/remotes/remove/plan", s.handlePlanRemoveRemote)
	protected.HandleFunc("POST /api/repos/{id}/remotes/remove", s.handleRemoveRemote)
	protected.HandleFunc("POST /api/repos/{id}/upstream/plan", s.handlePlanSetUpstream)
	protected.HandleFunc("POST /api/repos/{id}/upstream", s.handleSetUpstream)
	protected.HandleFunc("POST /api/repos/{id}/upstream/unset/plan", s.handlePlanUnsetUpstream)
	protected.HandleFunc("POST /api/repos/{id}/upstream/unset", s.handleUnsetUpstream)
	protected.HandleFunc("POST /api/repos/{id}/fetch", s.handleFetch)
	protected.HandleFunc("POST /api/repos/{id}/pull", s.handlePull)
	protected.HandleFunc("POST /api/repos/{id}/push/plan", s.handlePlanPush)
	protected.HandleFunc("POST /api/repos/{id}/push", s.handlePush)

	protected.HandleFunc("GET /api/repos/{id}/status", s.handleStatus)
	protected.HandleFunc("GET /api/repos/{id}/diff", s.handleDiff)
	protected.HandleFunc("POST /api/repos/{id}/stage", s.handleStage)
	protected.HandleFunc("POST /api/repos/{id}/unstage", s.handleUnstage)
	protected.HandleFunc("POST /api/repos/{id}/discard", s.handleDiscard)
	// The same body as the route above, answered with the commands it would
	// run instead of running them. The confirmation dialog is the only caller:
	// it has to show the exact command, and the exact command is this one's to
	// know.
	protected.HandleFunc("POST /api/repos/{id}/discard/plan", s.handleDiscardPlan)
	protected.HandleFunc("POST /api/repos/{id}/commit", s.handleCreateCommit)

	// The commit message git has already written for the operation in
	// progress — MERGE_MSG and its siblings. Read once when an operation
	// starts rather than folded into the status, which is polled; see the
	// handler.
	protected.HandleFunc("GET /api/repos/{id}/prepared-message", s.handlePreparedMessage)

	// Editing a work-tree file, which is not a git operation and does not
	// pretend to be: GET reads what is on disk, PUT puts it back. PUT rather
	// than POST because saving the same content twice is the same file.
	protected.HandleFunc("GET /api/repos/{id}/file", s.handleReadFile)
	protected.HandleFunc("PUT /api/repos/{id}/file", s.handleSaveFile)

	// Taking one side of a conflict whole, which IS a git operation and shows
	// up in the log panel as the two commands it runs.
	protected.HandleFunc("POST /api/repos/{id}/resolve", s.handleResolve)

	// Finishing or calling off what the repository is in the middle of. The
	// plan first, because aborting a rebase discards every conflict resolved
	// since it began and the confirmation has to show the line that does it.
	protected.HandleFunc("POST /api/repos/{id}/operation/plan", s.handleOperationPlan)
	protected.HandleFunc("POST /api/repos/{id}/operation", s.handleOperation)

	// The two halves of one promise, split the way HTTP splits things: the
	// backlog is a collection, the stream is what happens next.
	protected.HandleFunc("GET /api/log", s.handleLog)
	protected.HandleFunc("GET /api/events", s.handleEvents)

	// Any unrecognized /api path answers 404 in JSON. Without this route it
	// would fall through to the frontend, which would render an HTML page in
	// reply to an API call — the worst possible clue for a typo in a URL.
	protected.HandleFunc("/api/", s.handleUnknownAPI)

	protected.Handle("/", s.frontend)

	mux.Handle("/", s.requireToken(protected))

	return s.logRequests(securityHeaders(s.development)(s.rateLimit(mux)))
}

// recordingWriter captures the status code on the way through.
// http.ResponseWriter does not expose it, and a request log without a status
// cannot say whether the request succeeded — about the only thing it is asked
// for.
//
// Wrapping a ResponseWriter has a trap: the wrapper hides the interfaces the
// original writer implemented. Anything that needs more than "write bytes"
// then stops working — the WebSocket upgrade behind hot reload, flushing an
// SSE stream — with error messages that never point at the middleware to
// blame. The three methods below hand those capabilities back.
type recordingWriter struct {
	http.ResponseWriter
	status int
}

// These assertions fail at compile time if a capability is lost, which beats
// finding out at the first WebSocket upgrade.
var (
	_ http.Hijacker = (*recordingWriter)(nil)
	_ http.Flusher  = (*recordingWriter)(nil)
)

func (w *recordingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *recordingWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

// Hijack hands the raw connection to the caller, which every switch to
// another protocol needs — WebSocket in particular.
func (w *recordingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, capable := w.ResponseWriter.(http.Hijacker)
	if !capable {
		return nil, nil, fmt.Errorf(
			"the underlying ResponseWriter (%T) cannot hand over the connection", w.ResponseWriter)
	}
	w.status = http.StatusSwitchingProtocols
	return hijacker.Hijack()
}

// Flush pushes out what has been written right away. Essential to an event
// stream, which is worth nothing unless it arrives as it happens.
func (w *recordingWriter) Flush() {
	if flusher, capable := w.ResponseWriter.(http.Flusher); capable {
		flusher.Flush()
	}
	// A writer with no flushing is not an error: httptest.ResponseRecorder is
	// one, and everything there is already in memory.
}

// Unwrap lets http.ResponseController reach the original writer, for the
// capabilities this wrapper does not expose explicitly.
func (w *recordingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		recorder := &recordingWriter{ResponseWriter: writer}

		defer s.recoverPanic(recorder, request, started)

		next.ServeHTTP(recorder, request)

		s.logger.Info("request",
			"method", request.Method,
			"path", request.URL.Path,
			"status", recorder.status,
			"duration", time.Since(started).Round(time.Microsecond).String(),
		)
	})
}

// recoverPanic turns a panicking handler into an answer and a log line.
//
// net/http already recovers a panic, and what it does with it is close the
// connection: the client gets an EOF with no status, no body and no
// explanation, and this middleware's own log line never runs — the request
// vanishes from the journal as if it had never arrived. A daemon that fails
// silently is the one thing this project does not allow.
//
// Deferred rather than wrapped so it also catches a panic raised by the log
// line itself, and so the timing is recorded either way.
func (s *Server) recoverPanic(recorder *recordingWriter, request *http.Request, started time.Time) {
	raised := recover()
	if raised == nil {
		return
	}

	// ErrAbortHandler is net/http's own way of saying "drop this connection,
	// deliberately". Swallowing it would turn an intentional abort into a 500.
	// The type assertion is guarded: panic takes any value, and panic("boom")
	// is perfectly legal.
	if failure, isError := raised.(error); isError && errors.Is(failure, http.ErrAbortHandler) {
		panic(raised)
	}

	s.logger.Error("handler panicked",
		"method", request.Method,
		"path", request.URL.Path,
		"panic", fmt.Sprint(raised),
		"stack", string(debug.Stack()),
		"duration", time.Since(started).Round(time.Microsecond).String(),
	)

	// Nothing is echoed back to the client: a panic message can carry
	// anything that was in scope. The journal has the details.
	if recorder.status == 0 {
		writeError(recorder, s.logger, http.StatusInternalServerError,
			errors.New("the daemon hit an unexpected error; its log has the details"))
	}
}

// healthPayload is the answer to "what is this, and what is it running on".
//
// It is the first thing to ask for when somebody reports that yagit is
// misbehaving on a machine nobody else has. Everything that changes the
// answer to "does this operation work here" is in it: which yagit, which
// git, which platform. Nothing in it names a path, a repository or a branch —
// it is meant to be pasted into a bug report by someone who has not read it
// first.
type healthPayload struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Repos   int    `json:"repos"`

	// Platform is GOOS/GOARCH. Half the reports that are hard to reproduce are
	// hard because of it.
	Platform string `json:"platform"`

	// Git is the version of the binary yagit drives, or empty when it could
	// not be asked — in which case GitError says why. yagit is a front end
	// over that binary, so "which git" is as much a part of the answer as
	// "which yagit".
	Git      string `json:"git,omitempty"`
	GitError string `json:"gitError,omitempty"`

	// Unavailable names the operations this git is too old for, in the words a
	// user would use, not the flags. Empty on any reasonably current git.
	//
	// This is the field that turns a confusing failure into a sentence: a
	// rebase that refuses on a machine where everything else works is
	// bewildering when discovered mid-operation and obvious when it is written
	// here beforehand. yagit still starts, and still fails loudly on exactly
	// that operation and nowhere else — this only means the user can find out
	// before they hit it rather than after.
	Unavailable []string `json:"unavailable,omitempty"`
}

// gitFloors are the versions the documented operations need, and the sentence
// each one costs when it is missing.
//
// The numbers live here rather than beside the flags that need them because
// this is the only place that reports on all of them at once; where a flag is
// chosen, the version is read for that flag alone. Keeping both is the same
// value written twice, and the reason it is tolerated is that they answer
// different questions — one decides a command, this one describes a machine.
var gitFloors = []struct {
	major, minor int
	operation    string
}{
	{2, 30, "force push, which needs --force-if-includes"},

	// The real floor, and the one that was written down as 2.30 until it was
	// measured. Opening a repository at all runs `rev-parse
	// --path-format=absolute` — twice in internal/repo/registry.go and once in
	// internal/repo/discover.go — and that option arrived in 2.31. Under it
	// nothing works, and what the user is told is "not a git repository"
	// followed by git's complaint about an unknown option: a sentence that
	// blames their repository for the age of their git.
	{2, 31, "opening a repository at all, which needs `rev-parse --path-format`"},

	{2, 36, "worktree paths holding a newline, which need `worktree list -z`"},
	{2, 38, "rebase, which needs --no-update-refs to leave other branches alone"},
}

func (s *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	payload := healthPayload{
		Status:   "ok",
		Version:  Version,
		Repos:    len(s.registry.List()),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}

	// A git that cannot be asked is reported, not hidden: it is the single
	// most useful thing to know about a daemon where nothing works. The health
	// route still answers 200 — the daemon is up, and saying otherwise would
	// make a monitoring check report the wrong thing about itself.
	version, err := s.runner.GitVersion(request.Context())
	if err != nil {
		payload.GitError = err.Error()
	} else {
		payload.Git = version.String()
		for _, floor := range gitFloors {
			if !version.AtLeast(floor.major, floor.minor) {
				payload.Unavailable = append(payload.Unavailable,
					fmt.Sprintf("git %d.%d or newer is needed for %s", floor.major, floor.minor, floor.operation))
			}
		}
	}

	writeJSON(writer, s.logger, http.StatusOK, payload)
}

// Version is set by cmd/yagit at startup, from the value ./do build
// injects through -ldflags.
var Version = "dev"

func (s *Server) handleUnknownAPI(writer http.ResponseWriter, request *http.Request) {
	writeError(writer, s.logger, http.StatusNotFound,
		fmt.Errorf("unknown route: %s %s", request.Method, request.URL.Path))
}

// lookupRepo resolves the repository identifier carried by a route.
//
// This is where the security boundary applies on the HTTP side: an unknown
// identifier grants access to nothing, and no client-supplied path ever
// reaches the disk through here.
func (s *Server) lookupRepo(request *http.Request) (*repo.Repo, error) {
	identifier := request.PathValue("id")

	found, err := s.registry.Get(identifier)
	switch {
	case err == nil:
		return found, nil
	case errors.Is(err, repo.ErrUnknownRepo):
		// The only case where the advice applies. Telling someone to reopen a
		// repository that was replaced underneath sends them round in circles.
		return nil, fmt.Errorf("%w; open it first with POST /api/repos", err)
	default:
		return nil, err
	}
}

// statusForLookupError turns a refusal from the registry into an HTTP code.
//
// Not everything is a 404. A repository whose git directory has moved outside
// the root is a refusal to explain, and one that was swapped for another is a
// conflict with the state on disk — neither is fixed by asking again.
func statusForLookupError(err error) int {
	switch {
	case errors.Is(err, repo.ErrOutsideRoot):
		return http.StatusForbidden
	case errors.Is(err, repo.ErrRepoReplaced):
		return http.StatusConflict
	default:
		return http.StatusNotFound
	}
}
