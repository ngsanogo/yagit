package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// pageSize is how many commits one request of the history carries.
//
// Defined here and nowhere else. The interface never sends it: it asks for a
// page by number and reads the size out of the answer, so this number can be
// changed on its own without a second definition somewhere having to agree.
//
// Roughly three screens, so a fast scroll crosses a boundary about once a
// second, and a few tens of kilobytes over a loopback socket. It is the first
// number to revisit if scrolling stutters.
const pageSize = 200

// commitRow is a commit with the column its dot sits in.
//
// The lane is embedded into the commit rather than sent as a parallel array:
// two arrays that have to be read together are two arrays that can come apart,
// and the one thing this payload must not do is attach a commit to the wrong
// line.
type commitRow struct {
	git.Commit
	Lane int `json:"lane"`
}

// edgeRow is one line of the graph, in coordinates the interface can draw
// without holding the rest of the history.
//
// Rows are absolute indices into the whole history, not into the page: an edge
// crossing the page has its ends elsewhere, and this is what lets a page be
// drawn on its own. The columns of both ends travel with it for the same
// reason — the row they belong to is often not in the page at all.
//
// A line whose parent is not in the history — a shallow clone, a history cut
// short — carries -1 in both To and ToLane. It is a real line and it leaves
// the bottom of the picture, which is what the renderer has to draw rather
// than pretend the commit is a root.
type edgeRow struct {
	From     int `json:"from"`
	FromLane int `json:"from_lane"`
	To       int `json:"to"`
	ToLane   int `json:"to_lane"`
	Lane     int `json:"lane"`
}

type commitsPayload struct {
	Commits []commitRow `json:"commits"`
	Edges   []edgeRow   `json:"edges"`

	First    int `json:"first"`     // index of Commits[0] in the whole history
	PageSize int `json:"page_size"` // rows per page, the daemon's to decide
	Total    int `json:"total"`     // commits in the whole history
	Width    int `json:"width"`     // columns the picture needs
}

// headPayload is where HEAD sits, in the shape the interface reads.
//
// git.HEAD carries no JSON tags: internal/git describes git, not the wire.
// The mapping is written here, with every other payload's.
type headPayload struct {
	SHA      string `json:"sha"`
	Name     string `json:"name"`
	Detached bool   `json:"detached"`
}

type refsPayload struct {
	Refs []git.Ref `json:"refs"`

	// Head is absent in a repository with no commit yet. There is nothing to
	// mark then, and a zero-valued object would read as a branch with an
	// empty name.
	Head *headPayload `json:"head,omitempty"`
}

// commitDetailPayload is one commit: everything it says and changed, plus
// where it sits in the picture being drawn.
type commitDetailPayload struct {
	// Embedded, so the commit's own fields sit at the top level of the answer
	// rather than under a "detail" key. What the daemon knows about a commit
	// is one object; the two fields below are where it sits in the picture,
	// which is the only thing about it that depends on the walk.
	git.Detail

	// Row is the commit's index in the walk — the row the list scrolls to. The
	// interface cannot work it out: it holds a handful of pages and the commit
	// a reference names is usually in none of them.
	Row int `json:"row"`

	// Lane is the column its dot is drawn in.
	Lane int `json:"lane"`
}

func (s *Server) handleCommits(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	page, err := requestedPage(request)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	scope, selected, err := requestedWalk(request)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	window, err := s.history.Window(
		request.Context(), opened, page*pageSize, pageSize, scope, selected)
	if err != nil {
		writeError(writer, s.logger, statusForWalkError(err), err)
		return
	}

	// Built with make rather than declared: a nil slice marshals to null, and
	// an interface handed null where it expected a list is an interface that
	// crashes on an empty repository.
	rows := make([]commitRow, 0, len(window.Commits))
	for offset, commit := range window.Commits {
		rows = append(rows, commitRow{Commit: commit, Lane: window.LaneOf(window.First + offset)})
	}

	edges := make([]edgeRow, 0, len(window.Edges))
	for _, edge := range window.Edges {
		edges = append(edges, edgeRow{
			From:     edge.From,
			FromLane: window.LaneOf(edge.From),
			To:       edge.To,
			// An edge with no parent in the history keeps Absent at both
			// ends, which is LaneOf's answer for a row that is not there.
			ToLane: window.LaneOf(edge.To),
			Lane:   edge.Lane,
		})
	}

	writeJSON(writer, s.logger, http.StatusOK, commitsPayload{
		Commits:  rows,
		Edges:    edges,
		First:    window.First,
		PageSize: pageSize,
		Total:    window.Total,
		Width:    window.Width,
	})
}

// maxPage is the largest page number that can be asked for.
//
// Not a limit on how long a history may be — at this page size it is a
// hundred billion commits — but on what page*pageSize is allowed to be. Any
// larger and that multiplication overflows, and the row it lands on is
// negative: the clamp in the history store reads that as zero and answers the
// FIRST page. So `?page=4611686018427387903` would quietly return the newest
// commits, which is the one answer that looks right and is wrong.
const maxPage = 1 << 40

// requestedPage reads ?page=, which defaults to the first one.
//
// A page that is not a number, or is negative, is refused rather than rounded
// to something plausible: it can only come from a client that computed it
// wrong, and answering the first page instead would hide that for good.
// Asking past the end is not the same mistake — a history shrinks whenever a
// branch is deleted — and answers an empty page.
func requestedPage(request *http.Request) (int, error) {
	raw := request.URL.Query().Get("page")
	if raw == "" {
		return 0, nil
	}

	page, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("page=%q is not a number", raw)
	}
	if page < 0 {
		return 0, fmt.Errorf("page=%d is before the first page", page)
	}
	if page > maxPage {
		return 0, fmt.Errorf("page=%d is past any history there could be", page)
	}
	return page, nil
}

// requestedWalk reads the two parameters that say which commits a walk covers:
// ?scope= and, under the scope that names them, ?ref=.
//
// One function because they are one question. A scope read without its refs is
// a walk that would silently be drawn from something else, and the pair
// travels together on all three routes that read a history — the pages, one
// commit's position in them, and the search — so reading it in one place is
// what keeps those three from drifting apart.
func requestedWalk(request *http.Request) (git.Scope, []string, error) {
	scope, err := requestedScope(request)
	if err != nil {
		return "", nil, err
	}
	selected, err := requestedRefs(request, scope)
	if err != nil {
		return "", nil, err
	}
	return scope, selected, nil
}

// requestedScope reads ?scope=, which names the refs the graph is drawn from.
//
// Absent is the default picture — what is checked out — and not a client
// mistake: a request naming no scope is one that has not chosen, which is what
// a default is for. yagit's own interface sends the parameter every time
// anyway, because the choice is on screen and the request can say so.
//
// A value that is none of the three is refused rather than read as the
// default, for the reason a nonsense page number is: answering a plausible
// history to a client that asked for something else hides the defect for good,
// and this one would hide it behind a picture that looks right.
func requestedScope(request *http.Request) (git.Scope, error) {
	switch raw := request.URL.Query().Get("scope"); raw {
	case "", string(git.ScopeHead):
		return git.ScopeHead, nil
	case string(git.ScopeAll):
		return git.ScopeAll, nil
	case string(git.ScopeRefs):
		return git.ScopeRefs, nil
	default:
		return "", fmt.Errorf("scope=%q names no set of refs; use %q, %q or %q",
			raw, git.ScopeHead, git.ScopeAll, git.ScopeRefs)
	}
}

// requestedRefs reads the repeated ?ref=, which is the walk itself under
// git.ScopeRefs and meaningless under the other two.
//
// Repeated rather than one comma-separated value. A ref name may hold a comma
// — git forbids a short list of bytes and that is not among them — and a
// separator a name can contain is a separator that eventually splits one ref
// into two that do not exist (docs/adr/0033).
//
// Both mismatches are refused rather than quietly corrected:
//
//   - No ref under scope=refs. An empty walk drawn is indistinguishable from
//     an empty repository, so a blank graph would be a correct-looking answer
//     to a question nobody asked.
//   - A ref under a scope that ignores it. The client thinks it is narrowing
//     a walk and would get the whole repository back with no sign of it —
//     the same failure as an unknown scope, and refused for the same reason.
//
// The names themselves are checked in internal/git, where they become
// arguments: both walks that take a chosen set go through one function there,
// and a check written here as well would be a second definition of what a ref
// name is.
func requestedRefs(request *http.Request, scope git.Scope) ([]string, error) {
	selected := request.URL.Query()["ref"]

	if scope != git.ScopeRefs {
		if len(selected) > 0 {
			return nil, fmt.Errorf(
				"ref= names the walk only under scope=%q; scope=%q walks %s",
				git.ScopeRefs, scope, scope.Describe())
		}
		return nil, nil
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("scope=%q needs at least one ref=; a walk over no reference "+
			"draws an empty picture, which is not an empty repository", git.ScopeRefs)
	}
	return selected, nil
}

func (s *Server) handleRefs(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	payload, err := s.readRefs(request.Context(), opened)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, payload)
}

// readRefs is every reference in a repository, and where HEAD sits among them.
//
// Two readers: the route above, and the checkout in branch.go, which answers
// with this rather than nothing so the sidebar never draws one frame of the
// branch it just left. One function because they must describe the same thing
// — a checkout that answered a payload assembled slightly differently would be
// a second definition of what the sidebar is, discovered the day one of them
// grew a field.
func (s *Server) readRefs(ctx context.Context, opened *repo.Repo) (refsPayload, error) {
	// Through the runner, not the history store. The store holds one
	// assignment per scope, so a refs request routed through it would have to
	// pick a scope nobody asked about and pay for that walk — and the refs are
	// a property of the repository, not of any one walk over it. Reading them
	// here is also strictly fresher: there is no held list to fall behind a
	// `git branch -u`, which changes an upstream without moving a ref.
	refs, err := s.runner.ForEachRef(ctx, opened.Path)
	if err != nil {
		return refsPayload{}, err
	}

	// HEAD is a second read because `for-each-ref` lists no HEAD, and which
	// branch is checked out — or that none is — is a fact the sidebar has no
	// other way to show. One `rev-parse`, beside a command that walks every
	// ref in the repository.
	head, err := s.runner.ReadHEAD(ctx, opened.Path)
	if err != nil {
		return refsPayload{}, err
	}

	payload := refsPayload{Refs: refs}
	if head.SHA != "" {
		payload.Head = &headPayload{SHA: head.SHA, Name: head.Name, Detached: head.Detached}
	}
	return payload, nil
}

// handleCommit answers everything the daemon knows about one commit: where it
// sits in the history, what it says, and what it changed.
//
// One route because it is one click. A reference is followed by scrolling to
// the row, and the commit that lands under the pointer is the one whose
// message and patch are shown — asking for those separately would be two
// requests for one answer.
//
// The scope is part of the question. A row is a position in one walk, so the
// same commit sits at a different row under the other scope and at no row at
// all under one that does not reach it.
func (s *Server) handleCommit(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	scope, selected, err := requestedWalk(request)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	sha, err := checkedSHA(request.PathValue("sha"))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	located, found, err := s.history.Locate(request.Context(), opened, sha, scope, selected)
	if err != nil {
		writeError(writer, s.logger, statusForWalkError(err), err)
		return
	}
	if !found {
		// Named rather than a bare 404, because under the default scope this
		// is an ordinary thing to ask for and there is something to do about
		// it: a branch nobody has merged is reachable from a ref and from no
		// checkout, so the commit exists and the walk being drawn does not
		// reach it. The interface offers the other scope on the strength of
		// this sentence.
		writeError(writer, s.logger, http.StatusNotFound, fmt.Errorf(
			"commit %q is not in the history drawn from %s", sha, scope.Describe()))
		return
	}

	// Read for the SHA the history holds, never for the one in the URL. The
	// lookup above is what establishes that this object exists and is a commit
	// in this repository, so nothing unverified reaches git.
	detail, err := s.runner.Show(request.Context(), opened.Path, located.Commit.SHA)
	if err != nil {
		writeError(writer, s.logger, statusForCommitError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, commitDetailPayload{
		Detail: detail,
		Row:    located.Row,
		Lane:   located.Lane,
	})
}

// checkedSHA refuses anything that is not an object name.
//
// This route is only ever given a SHA the daemon itself put on screen — a row
// of the history, or a reference's target — so the shape is known exactly:
// lowercase hex, forty characters for SHA-1 and sixty-four for SHA-256.
// Nothing else is a commit in this repository, and saying so is a better
// answer than scanning the history for it and reporting that it is not there.
//
// It also means no string from the network is ever handed to git, whatever
// happens to the lookup below it. `--output=/tmp/written` is a revision no
// more than `-f` is a filename, and a check that depends on a scan finding
// nothing is a check one refactor away from being absent.
func checkedSHA(candidate string) (string, error) {
	if len(candidate) != 40 && len(candidate) != 64 {
		return "", fmt.Errorf(
			"%q is not a commit name: expected 40 or 64 hexadecimal characters, got %d",
			candidate, len(candidate))
	}
	for _, letter := range candidate {
		hexadecimal := (letter >= '0' && letter <= '9') || (letter >= 'a' && letter <= 'f')
		if !hexadecimal {
			return "", fmt.Errorf("%q is not a commit name: %q is not a hexadecimal digit",
				candidate, letter)
		}
	}
	return candidate, nil
}

// statusForWalkError tells a walk that cannot be asked for from a daemon that
// broke.
//
// A ref name git must never be given is the client's mistake, not the
// daemon's: it can only come from a request naming a reference the interface
// never listed. Everything else a walk can fail with — git missing, a
// repository torn out from under an open handle — is this machine's problem
// and says so with a 500.
func statusForWalkError(err error) int {
	if errors.Is(err, git.ErrBadRefName) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// statusForCommitError tells a name git does not know from a daemon that
// broke. Asking for a commit that is not there is a request to fix, not a
// fault to report.
func statusForCommitError(err error) int {
	// Before the git.Error case below, and it has to be: a diff too large to
	// hold arrives wrapped in one, and answering 404 would say the commit is
	// not there when it is — and is exactly the one the reader wants.
	if errors.Is(err, git.ErrDiffTooLarge) {
		return http.StatusRequestEntityTooLarge
	}
	var gitError *git.Error
	if errors.As(err, &gitError) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}
