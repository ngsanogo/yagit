package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// The network, over HTTP: what the repository talks to, and the three commands
// that talk to it.
//
// Each of the three answers with the references and where HEAD sits — the
// payload GET /refs sends — for the reason the branch routes do: that is what
// they changed, and a sidebar that had to ask again would draw one frame of
// the state it just left. A fetch moves the remote-tracking branches, a pull
// moves the current branch, and a push moves what the ahead and behind counts
// are measured against; all three are in that answer.
//
// What is NOT in it is the working directory, which a pull changes as much as
// a checkout does. Sending it would cost a second `git status` on every one of
// these, and the interface polls that twice a second anyway (ADR 0015); the
// client drops what it holds instead, which is one request rather than two.
//
// Where the operation is decided is worth naming. The client says which of the
// three it wants and, at most, which remote to publish to; everything else —
// the branch, the upstream it follows, the refspec, whether this is a publish
// — is read here, from git, at the moment of the request. A browser that
// worked out its own refspec would be a second definition of the destination,
// and the stale half of it would push a branch somewhere nobody chose.

// fetchRequest names what to fetch from.
type fetchRequest struct {
	// Remote is the one to reach, or empty for every one of them. Empty is
	// what a fetch button with no picker beside it means, and it is the
	// ordinary case: a repository with one remote and a person who wants to
	// know what has happened.
	Remote string `json:"remote"`
}

// pullRequest names how to integrate what comes back.
type pullRequest struct {
	// Strategy is one of ff-only, merge or rebase, and is required. git's own
	// answer to this question is a setting; a button cannot read a setting and
	// still say what it does, so the choice travels with the request.
	Strategy string `json:"strategy"`
}

// pushRequest names where to send the current branch, and how hard.
type pushRequest struct {
	// Remote is used only when the branch follows nothing — publishing it.
	// A branch with an upstream goes to that upstream whatever is sent here:
	// the follow is the answer, and second-guessing it is how a repository
	// ends up with two branches for one line of work.
	Remote string `json:"remote"`

	// Force is `--force-with-lease --force-if-includes`, never a bare
	// `--force`. The interface asks first, and what it shows it asks with is
	// the command handlePlanPush answers.
	Force bool `json:"force"`

	// LocalBranch and Ref are what the confirmation showed, echoed back so a
	// run cannot push a different branch than the one approved. Empty on the
	// ordinary push button, which shows no confirmation; required when Force
	// is set or when the push is a publish (see handlePush).
	LocalBranch string `json:"local_branch"`
	Ref         string `json:"ref"`
}

// pushOperation is the lease wording for a push whose destination moved.
var pushOperation = branchOperation{gerund: "pushing", preposition: "on"}

// errPushLeaseRequired: a force push or publish ran without the destination
// the confirmation showed. The ordinary push button sends neither field; the
// two that ask first must echo both.
var errPushLeaseRequired = errors.New(
	"force push and publish require the planned local_branch and ref")

type remotesPayload struct {
	Remotes []git.Remote `json:"remotes"`
}

// handleRemotes lists what the repository is configured to talk to.
//
// The URLs come back redacted — a token pasted into a remote URL is a password,
// and this route draws it on a screen. The redaction happens in internal/git,
// at the parse, so that no route can forget it.
func (s *Server) handleRemotes(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	remotes, err := s.runner.Remotes(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// An empty list is `[]`, never `null`: the interface asks "are there any"
	// of the array itself, and JSON null would answer that question wrongly in
	// a language where it is falsy for a second reason.
	if remotes == nil {
		remotes = []git.Remote{}
	}
	writeJSON(writer, s.logger, http.StatusOK, remotesPayload{Remotes: remotes})
}

// Editing the remote list over HTTP.
//
// Add, rename and remove answer with the remotes list — the thing they
// changed — for the same reason the branch routes answer with the refs list.
// Rename and remove also move or delete refs under refs/remotes/, so the
// client drops its held references rather than drawing one frame of names that
// no longer exist.

type addRemoteRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type renameRemoteRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type removeRemoteRequest struct {
	Name string `json:"name"`
}

// handleAddRemote records a remote by name and URL.
func (s *Server) handleAddRemote(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body addRemoteRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…", "url": "…"}`) {
		return
	}

	if err := s.runner.AddRemote(uninterrupted(request), opened.Path, body.Name, body.URL); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRemotes(writer, request, opened)
}

// handleRenameRemote changes a remote's name.
func (s *Server) handleRenameRemote(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body renameRemoteRequest
	if !decodeBody(writer, s.logger, request, &body, `{"from": "…", "to": "…"}`) {
		return
	}

	if err := s.runner.RenameRemote(uninterrupted(request), opened.Path, body.From, body.To); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRemotes(writer, request, opened)
}

// handlePlanRemoveRemote says what removing would run, without running it.
func (s *Server) handlePlanRemoveRemote(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body removeRemoteRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoRemote)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.RemoveRemoteArgs(strings.TrimSpace(body.Name))),
	})
}

// handleRemoveRemote forgets a remote and its remote-tracking branches.
func (s *Server) handleRemoveRemote(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body removeRemoteRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}

	if err := s.runner.RemoveRemote(uninterrupted(request), opened.Path, body.Name); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRemotes(writer, request, opened)
}

// answerWithRemotes lists what the repository talks to, after a change to that list.
func (s *Server) answerWithRemotes(writer http.ResponseWriter, request *http.Request, opened *repo.Repo) {
	remotes, err := s.runner.Remotes(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if remotes == nil {
		remotes = []git.Remote{}
	}
	writeJSON(writer, s.logger, http.StatusOK, remotesPayload{Remotes: remotes})
}

type setRemoteURLRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// handleSetRemoteURL changes where a remote is fetched from.
func (s *Server) handleSetRemoteURL(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body setRemoteURLRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…", "url": "…"}`) {
		return
	}

	if err := s.runner.SetRemoteURL(uninterrupted(request), opened.Path, body.Name, body.URL); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRemotes(writer, request, opened)
}

// handlePlanSetRemoteURL says what set-url would run, without running it.
//
// The URL on the command is redacted the way a remote list is: a token in the
// confirmation would be a password drawn on a screen.
func (s *Server) handlePlanSetRemoteURL(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body setRemoteURLRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…", "url": "…"}`) {
		return
	}
	name, url := strings.TrimSpace(body.Name), strings.TrimSpace(body.URL)
	if name == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoRemote)
		return
	}
	if url == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoRemoteURL)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.SetRemoteURLArgs(name, git.RedactURL(url))),
	})
}

type setUpstreamRequest struct {
	// Branch is the local branch. Empty means the one HEAD is on.
	Branch string `json:"branch"`
	Remote string `json:"remote"`
	// Upstream is the short branch name on the remote: "main", not
	// "refs/heads/main" and not "origin/main".
	Upstream string `json:"upstream"`
}

type unsetUpstreamRequest struct {
	Branch string `json:"branch"`
}

type upstreamPlan struct {
	Command  string `json:"command"`
	Branch   string `json:"branch"`
	Remote   string `json:"remote,omitempty"`
	Upstream string `json:"upstream,omitempty"`
}

// handlePlanSetUpstream says what setting the follow would run.
func (s *Server) handlePlanSetUpstream(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body setUpstreamRequest
	if !decodeBody(writer, s.logger, request, &body, `{"branch": "…", "remote": "…", "upstream": "…"}`) {
		return
	}

	branch, remote, upstream, err := s.readUpstreamChange(request, opened, body)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, upstreamPlan{
		Command:  git.CommandLine(git.SetUpstreamArgs(branch, remote, upstream)),
		Branch:   branch,
		Remote:   remote,
		Upstream: upstream,
	})
}

// handleSetUpstream records that a local branch follows a remote-tracking one.
func (s *Server) handleSetUpstream(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body setUpstreamRequest
	if !decodeBody(writer, s.logger, request, &body, `{"branch": "…", "remote": "…", "upstream": "…"}`) {
		return
	}

	branch, remote, upstream, err := s.readUpstreamChange(request, opened, body)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.SetUpstream(uninterrupted(request), opened.Path, branch, remote, upstream); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// handlePlanUnsetUpstream says what unsetting the follow would run.
func (s *Server) handlePlanUnsetUpstream(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body unsetUpstreamRequest
	if !decodeBody(writer, s.logger, request, &body, `{"branch": "…"}`) {
		return
	}

	branch, err := s.branchForUpstream(request.Context(), opened, body.Branch)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, upstreamPlan{
		Command: git.CommandLine(git.UnsetUpstreamArgs(branch)),
		Branch:  branch,
	})
}

// handleUnsetUpstream forgets what a local branch follows.
func (s *Server) handleUnsetUpstream(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body unsetUpstreamRequest
	if !decodeBody(writer, s.logger, request, &body, `{"branch": "…"}`) {
		return
	}

	branch, err := s.branchForUpstream(request.Context(), opened, body.Branch)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.UnsetUpstream(uninterrupted(request), opened.Path, branch); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// readUpstreamChange settles the three names a set-upstream needs.
func (s *Server) readUpstreamChange(
	request *http.Request, opened *repo.Repo, body setUpstreamRequest,
) (branch, remote, upstream string, err error) {
	branch, err = s.branchForUpstream(request.Context(), opened, body.Branch)
	if err != nil {
		return "", "", "", err
	}
	remote = strings.TrimSpace(body.Remote)
	upstream = strings.TrimSpace(body.Upstream)
	if remote == "" {
		return "", "", "", git.ErrNoRemote
	}
	if upstream == "" {
		return "", "", "", git.ErrNoUpstream
	}
	return branch, remote, upstream, nil
}

// branchForUpstream is the local branch an upstream change acts on.
//
// An empty name means HEAD's branch — the ordinary case from the remote bar.
// A name is required when the sidebar asks about a different branch.
func (s *Server) branchForUpstream(ctx context.Context, opened *repo.Repo, named string) (string, error) {
	named = strings.TrimSpace(named)
	if named != "" {
		return named, nil
	}
	return s.currentBranch(ctx, opened)
}

// handleFetch brings the remote-tracking branches up to date.
//
// No work tree required, unlike the two below: a fetch writes under
// refs/remotes and nowhere else, which is exactly what makes it the safe thing
// to do first — and a bare repository is a perfectly ordinary thing to keep
// up to date.
//
// Progress rides this response as NDJSON (ADR 0030), the same shape clone
// uses. Validation that fails before git starts is still an ordinary JSON
// error — there is nothing to stream yet.
func (s *Server) handleFetch(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body fetchRequest
	if !decodeBody(writer, s.logger, request, &body, `{"remote": "…"}`) {
		return
	}

	emit, err := beginProgressStream(writer)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	// The pump keeps the writing off the goroutine os/exec copies stderr on,
	// so a browser that stops reading cannot stall git. Stopped before
	// anything else is emitted: until it returns, a second goroutine is
	// writing to this same response.
	pump := startProgressPump(emitProgressLine(emit))
	defer pump.stop()

	err = s.runner.Fetch(uninterrupted(request), opened.Path, body.Remote, pump.line)
	pump.stop()
	if err != nil {
		emitProgressError(emit, err)
		return
	}

	s.finishProgressWithRefs(emit, request, opened)
}

// handlePull fetches the upstream and integrates it into the current branch.
//
// A conflict is not an error this hides: git stops, writes the markers into
// the work tree and exits non-zero, and that reaches the interface as the
// failure it is — with git's own account, and with the repository in a state
// the banner names and the conflict screen resolves.
//
// Progress rides this response as NDJSON, for the same reason fetch's does.
func (s *Server) handlePull(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	// Same refusal merge and stash make first: a pull writes the index, and
	// starting one on top of a stopped rebase is a 409 before NDJSON rather
	// than git's later refusal mid-stream.
	if err := s.repositoryIdle(opened); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	var body pullRequest
	if !decodeBody(writer, s.logger, request, &body, `{"strategy": "ff-only|merge|rebase"}`) {
		return
	}

	strategy, err := git.ParsePullStrategy(body.Strategy)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	upstream, err := s.upstreamOfHEAD(request.Context(), opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	emit, err := beginProgressStream(writer)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	// Off the copy goroutine, for the reason fetch's does it: see
	// progressPump. Stopped before anything else is emitted on this response.
	pump := startProgressPump(emitProgressLine(emit))
	defer pump.stop()

	err = s.runner.Pull(uninterrupted(request), opened.Path, upstream, strategy, pump.line)
	pump.stop()
	if err != nil {
		emitProgressError(emit, err)
		return
	}

	s.finishProgressWithRefs(emit, request, opened)
}

// handlePush sends the current branch to where it follows, or publishes it.
//
// Progress rides this response as NDJSON, for the same reason fetch's does.
func (s *Server) handlePush(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body pushRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"remote": "…", "force": false, "local_branch": "…", "ref": "…"}`) {
		return
	}

	destination, err := s.pushDestination(request.Context(), opened, body.Remote)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// A force push or a publish showed a confirmation. The destination it
	// named must still be the one about to run — the same lease reset and
	// merge hold to — or the button would approve a different `git push` than
	// the line on screen. The ordinary push button shows no confirmation and
	// sends neither field.
	if body.Force || destination.SetUpstream {
		if strings.TrimSpace(body.LocalBranch) == "" || strings.TrimSpace(body.Ref) == "" {
			writeError(writer, s.logger, http.StatusBadRequest, errPushLeaseRequired)
			return
		}
	}
	if err := s.agreesOnPushDestination(body, destination); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	emit, err := beginProgressStream(writer)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	// Off the copy goroutine, for the reason fetch's does it: see
	// progressPump. Stopped before anything else is emitted on this response.
	pump := startProgressPump(emitProgressLine(emit))
	defer pump.stop()

	err = s.runner.Push(uninterrupted(request), opened.Path, destination, body.Force, pump.line)
	pump.stop()
	if err != nil {
		emitProgressError(emit, err)
		return
	}

	s.finishProgressWithRefs(emit, request, opened)
}

// handlePlanPush says what pushing would run, without running it.
//
// The same body as the route above, answered with the command instead of the
// consequence. Two callers, and neither could assemble this line for itself:
// the force confirmation has to show the exact command, and the publish dialog
// has to name a destination that is decided here from an upstream the browser
// never reads.
func (s *Server) handlePlanPush(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body pushRequest
	if !decodeBody(writer, s.logger, request, &body, `{"remote": "…", "force": false}`) {
		return
	}

	destination, err := s.pushDestination(request.Context(), opened, body.Remote)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, pushPlan{
		Command:     git.CommandLine(git.PushArgs(destination, body.Force)),
		Remote:      destination.Remote,
		Ref:         destination.Ref,
		Publishing:  destination.SetUpstream,
		LocalBranch: destination.Branch,
	})
}

// pushPlan is what a push would do, before it does it.
//
// The command is the part that has to be exact — it is drawn on a confirmation
// — and the rest is the same fact said in the terms a sentence needs: "publish
// feature to origin" reads better above a button than a refspec does, and both
// have to come from the same reading of the repository or one of them will be
// describing a different push.
type pushPlan struct {
	Command     string `json:"command"`
	Remote      string `json:"remote"`
	Ref         string `json:"ref"`
	LocalBranch string `json:"local_branch"`
	Publishing  bool   `json:"publishing"`
}

// currentBranch is the branch these operations act on, or the reason there is
// none.
//
// Read here rather than taken from the request, because it is what makes the
// rest answerable: `git for-each-ref refs/heads/HEAD` is a pattern that matches
// nothing, so a detached HEAD would come back as "this branch follows nothing"
// rather than as "there is no branch" — and the two need different sentences on
// screen, since one is published with a button and the other is a state to
// leave first.
//
// Three answers rather than two, because ReadHEAD gives an empty repository and
// a detached HEAD the same shape from a distance: no branch name. Only one of
// them is fixed by making a commit, and telling somebody their HEAD is detached
// in a repository they created a minute ago sends them looking for a checkout
// nobody made.
func (s *Server) currentBranch(ctx context.Context, opened *repo.Repo) (string, error) {
	head, err := s.runner.ReadHEAD(ctx, opened.Path)
	switch {
	case err != nil:
		return "", err
	case head.SHA == "":
		return "", git.ErrNoCommits
	case head.Detached || head.Name == "" || head.Name == "HEAD":
		return "", git.ErrDetachedHEAD
	}
	return head.Name, nil
}

// upstreamOfHEAD reads what the checked-out branch follows, and refuses if it
// follows nothing: there is nowhere to pull from.
func (s *Server) upstreamOfHEAD(ctx context.Context, opened *repo.Repo) (git.Upstream, error) {
	branch, err := s.currentBranch(ctx, opened)
	if err != nil {
		return git.Upstream{}, err
	}

	upstream, err := s.runner.UpstreamOf(ctx, opened.Path, branch)
	if err != nil {
		return git.Upstream{}, err
	}
	if !upstream.Configured() {
		return git.Upstream{}, git.ErrNoUpstream
	}
	return upstream, nil
}

// pushDestination works out where the current branch would go.
//
// Unlike upstreamOfHEAD above, a branch that follows nothing is not an error
// here: it is the publish, and the remote the client named is what completes
// it.
func (s *Server) pushDestination(
	ctx context.Context, opened *repo.Repo, remote string,
) (git.PushDestination, error) {
	branch, err := s.currentBranch(ctx, opened)
	if err != nil {
		return git.PushDestination{}, err
	}

	upstream, err := s.runner.UpstreamOf(ctx, opened.Path, branch)
	if err != nil {
		return git.PushDestination{}, err
	}

	return git.DestinationFor(branch, upstream, remote)
}

// agreesOnPushDestination refuses a run whose echoed plan no longer matches.
//
// Empty lease fields mean no confirmation was shown (the ordinary push
// button). When either field is set, both must match — a half-filled lease is
// how a short SHA became a different object on other routes.
func (s *Server) agreesOnPushDestination(body pushRequest, destination git.PushDestination) error {
	claimedBranch := strings.TrimSpace(body.LocalBranch)
	claimedRef := strings.TrimSpace(body.Ref)
	if claimedBranch == "" && claimedRef == "" {
		return nil
	}
	if err := pushOperation.agreesOnBranch(claimedBranch, destination.Branch); err != nil {
		return err
	}
	if claimedRef != destination.Ref {
		return fmt.Errorf("%w: the destination is now %s, not %s — read the plan again",
			errHeadMoved, destination.Ref, claimedRef)
	}
	return nil
}
