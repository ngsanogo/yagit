package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// The stash, over HTTP.
//
// Four things can be done to a stack of stashes and they are three shapes.
// Pushing makes one out of the work tree; applying puts one back, keeping or
// removing it; dropping throws one away. Reading the stack and reading what
// one entry holds are the two GETs beside them.
//
// Every route that WRITES names a stash twice: by position, which is what git
// takes, and by object name, which is what the position meant when the plan
// was drawn. Both are read again here through git.StashTarget, and a position
// that has come to hold something else is refused. That is this family's
// version of agreesOnCommit, and it needs its own function because the two
// halves cannot be collapsed: `git stash drop` refuses an object name, so the
// position is what has to run, and the position is the part that moves. See
// docs/adr/0028.
//
// The refusals that come before any of it — a bare repository, a repository
// already in the middle of something — are workTree's and repositoryIdle's.
// Notably NOT branchUnderfoot's: stashing works on a detached HEAD, and
// demanding a branch would refuse a real operation for an unrelated reason.

// stashesPayload is the answer to every write in this file, and to the list.
//
// The stack rather than an acknowledgement, for the reason answerWithRefs
// sends the references: the operation changed the list, the client is drawing
// it, and a client that has to ask again is a client drawing a stale one in
// between.
type stashesPayload struct {
	Stashes []git.Stash `json:"stashes"`
}

// handleStashes lists the stack.
func (s *Server) handleStashes(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	s.answerWithStashes(writer, request.Context(), opened.Path)
}

// answerWithStashes reads the stack and sends it.
//
// One function because five routes end this way, and because the alternative
// — each of them reading the list and writing the payload — is five places for
// the shape to drift.
func (s *Server) answerWithStashes(writer http.ResponseWriter, ctx context.Context, dir string) {
	stashes, err := s.runner.Stashes(ctx, dir)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, stashesPayload{Stashes: stashes})
}

// stashDetail is one stash and everything it holds.
type stashDetail struct {
	git.Stash
	Files []git.FileDiff `json:"files"`
}

// handleStashDetail reads what one stash holds.
//
// By position, because that is what the row in the list knows, and this is the
// one route in the file that does not also demand the object name back. It is
// a read: nothing is at stake if the stack shifted between the click and the
// answer, and the answer carries the stash it actually read — its object name,
// its message, its branch — so the panel titles itself from what came back
// rather than from the row that was clicked.
//
// A position past the end is a 404 rather than a refusal about a plan, because
// that is what it is: there is no such thing to look at.
func (s *Server) handleStashDetail(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	index, err := stashIndexFrom(request)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	stashes, err := s.runner.Stashes(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if index >= len(stashes) {
		writeError(writer, s.logger, http.StatusNotFound,
			errors.New("there is no stash at that position: the stack holds "+
				strconv.Itoa(len(stashes))))
		return
	}

	stash := stashes[index]
	files, err := s.runner.ShowStash(request.Context(), opened.Path, stash.SHA)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashDetail{Stash: stash, Files: files})
}

// stashIndexFrom reads the {index} of a stash route.
//
// Refused rather than defaulted to zero, which is a real position: a URL with
// a typo in it would otherwise read the top of the stack and answer 200.
func stashIndexFrom(request *http.Request) (int, error) {
	raw := request.PathValue("index")
	index, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errUnreadableStashIndex(raw)
	}
	if index < 0 {
		return 0, errUnreadableStashIndex(raw)
	}
	return index, nil
}

func errUnreadableStashIndex(raw string) error {
	return errors.New("the stash position must be a number from 0 upwards, not " + strconv.Quote(raw))
}

// stashPushPlanRequest asks what setting the work tree aside would save.
type stashPushPlanRequest struct {
	// Untracked is whether files git does not track yet come along.
	//
	// Part of the question rather than of the answer, because it changes
	// whether there is anything to save at all: a work tree holding nothing but
	// untracked files is a stash with the box ticked and a command that
	// silently does nothing without it. The plan describes that work tree
	// rather than refusing it — the refusal is PushStash's — because the dialog
	// that can offer the flag has to be able to open first.
	Untracked bool `json:"untracked"`
}

// stashPushRequest is the stash the dialog described, sent back to be made.
type stashPushRequest struct {
	// Message is what the stash is called, or empty for the one git writes.
	//
	// Not echoed back from a plan, because no plan ever held it: a message is
	// typed, and a round trip per keystroke to keep a command on screen
	// current would be a round trip per keystroke. See stashPushPlan for why
	// this family's create route shows no command at all.
	Message string `json:"message"`

	// Untracked is the choice the dialog showed, and it is what builds the
	// command. Judged again by PushStash, which refuses the combination that
	// would have succeeded and saved nothing.
	Untracked bool `json:"untracked"`
}

// stashPushPlan is what a stash would save.
//
// No command, and that is deliberate rather than an omission. This project
// shows the exact line for operations that take something away — see
// ConfirmDialog, whose destructive arm cannot be used without naming the loss
// — and a stash takes nothing away: the work is saved, and the whole feature
// exists to get it back. What the dialog needs instead is what would be
// SAVED, and which of it would be left behind, which is what these counts are.
//
// The command still reaches the user. Every git invocation this daemon makes
// appears in the log panel, this one included, with the message on it exactly
// as it ran.
type stashPushPlan struct {
	// Branch is the branch the stash would be filed under, empty on a detached
	// HEAD. Not a refusal: git records "(no branch)" and stashing there works.
	Branch string `json:"branch"`

	// IncludeUntracked echoes the question back, so a dialog drawing two
	// answers at once cannot show one request's counts under the other's box.
	IncludeUntracked bool `json:"include_untracked"`

	// Tracked and Untracked are counted apart because the command treats them
	// apart. One number would let the dialog promise to save files that stay
	// exactly where they are.
	//
	// Both zero for what the flags in force would save is not an error: it is
	// the dialog's cue to disable its own button and say why, with the tick box
	// still there to change the answer.
	Tracked   int `json:"tracked"`
	Untracked int `json:"untracked"`
}

// handlePlanStashPush says what stashing would save, without saving it.
func (s *Server) handlePlanStashPush(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.stashable(request)
	if err != nil {
		writeError(writer, s.logger, statusForStashableError(err), err)
		return
	}

	var body stashPushPlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"untracked": true|false}`) {
		return
	}

	preview, err := s.runner.PreviewStashPush(request.Context(), opened.Path, body.Untracked)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashPushPlan{
		Branch:           preview.Branch,
		IncludeUntracked: preview.IncludeUntracked,
		Tracked:          preview.Tracked,
		Untracked:        preview.Untracked,
	})
}

// handleStashPush sets the work tree aside.
func (s *Server) handleStashPush(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.stashable(request)
	if err != nil {
		writeError(writer, s.logger, statusForStashableError(err), err)
		return
	}

	var body stashPushRequest
	if !decodeBody(writer, s.logger, request, &body, `{"message": "…", "untracked": true|false}`) {
		return
	}

	// No reading of the work tree here, and none is missing: PushStash reads it
	// itself and refuses the combination that would exit 0 having saved
	// nothing. Doing it twice would be two `git status` calls to answer one
	// question, and two places for the answer to be judged differently.
	stashes, err := s.runner.PushStash(
		uninterrupted(request), opened.Path, body.Message, body.Untracked)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashesPayload{Stashes: stashes})
}

// stashRequest names the stash a plan or a run is about.
//
// Both halves are required and both are checked. The position is what git
// takes; the object name is what the position meant when the user was looking
// at it. See git.StashTarget.
type stashRequest struct {
	Index int    `json:"index"`
	SHA   string `json:"sha"`
}

// stashApplyPlanRequest adds which of the two commands is meant.
type stashApplyPlanRequest struct {
	stashRequest
	Mode git.StashApplyMode `json:"mode"`
}

// stashApplyPlan is what putting one stash back would run.
type stashApplyPlan struct {
	Command string             `json:"command"`
	Index   int                `json:"index"`
	SHA     string             `json:"sha"`
	Message string             `json:"message"`
	Branch  string             `json:"branch"`
	Mode    git.StashApplyMode `json:"mode"`

	// Files is how many paths the stash holds.
	Files int `json:"files"`

	// DirtyFiles is how many tracked paths currently differ from HEAD or from
	// the index. Not a refusal — putting a stash back onto work in progress is
	// ordinary, and git merges the two — but it is the whole of the difference
	// between an apply that lands silently and one that stops on a conflict,
	// so the confirmation says which of the two it is walking into.
	DirtyFiles int `json:"dirty_files"`
}

// handlePlanStashApply says what applying or popping would run.
func (s *Server) handlePlanStashApply(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.stashable(request)
	if err != nil {
		writeError(writer, s.logger, statusForStashableError(err), err)
		return
	}

	var body stashApplyPlanRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"index": 0, "sha": "…", "mode": "apply|pop"}`) {
		return
	}

	mode, err := git.ParseStashApplyMode(string(body.Mode))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	stash, err := s.stashTarget(request, opened.Path, body.stashRequest)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	files, err := s.runner.CountStashFiles(request.Context(), opened.Path, stash.SHA)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	status, err := s.runner.Status(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashApplyPlan{
		Command:    git.CommandLine(git.StashApplyArgs(mode, stash.Index)),
		Index:      stash.Index,
		SHA:        stash.SHA,
		Message:    stash.Message,
		Branch:     stash.Branch,
		Mode:       mode,
		Files:      files,
		DirtyFiles: status.DirtyTracked(),
	})
}

// handleStashApply puts one stash back into the work tree.
//
// A conflict is not an error this hides: git stops, writes the markers into
// the work tree and exits non-zero, and that reaches the interface as the
// failure it is — with git's own account, which for this command says both
// which file conflicted and that the entry was kept. What it does not leave is
// an operation to abort: the conflict is finished in the changes view, by
// editing the file and staging it.
func (s *Server) handleStashApply(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.stashable(request)
	if err != nil {
		writeError(writer, s.logger, statusForStashableError(err), err)
		return
	}

	var body stashApplyPlanRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"index": 0, "sha": "…", "mode": "apply|pop"}`) {
		return
	}

	mode, err := git.ParseStashApplyMode(string(body.Mode))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	stash, err := s.stashTarget(request, opened.Path, body.stashRequest)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	stashes, err := s.runner.ApplyStash(uninterrupted(request), opened.Path, stash.Index, mode)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashesPayload{Stashes: stashes})
}

// stashDropPlan is what dropping one stash would run, and what it costs.
type stashDropPlan struct {
	Command string `json:"command"`
	Index   int    `json:"index"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Branch  string `json:"branch"`

	// Files is how many paths go with it — the loss the confirmation names.
	Files int `json:"files"`
}

// handlePlanStashDrop says what dropping would run, without running it.
func (s *Server) handlePlanStashDrop(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.stashable(request)
	if err != nil {
		writeError(writer, s.logger, statusForStashableError(err), err)
		return
	}

	var body stashRequest
	if !decodeBody(writer, s.logger, request, &body, `{"index": 0, "sha": "…"}`) {
		return
	}

	stash, err := s.stashTarget(request, opened.Path, body)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	files, err := s.runner.CountStashFiles(request.Context(), opened.Path, stash.SHA)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashDropPlan{
		Command: git.CommandLine(git.StashDropArgs(stash.Index)),
		Index:   stash.Index,
		SHA:     stash.SHA,
		Message: stash.Message,
		Branch:  stash.Branch,
		Files:   files,
	})
}

// handleStashDrop throws one stash away.
func (s *Server) handleStashDrop(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.stashable(request)
	if err != nil {
		writeError(writer, s.logger, statusForStashableError(err), err)
		return
	}

	var body stashRequest
	if !decodeBody(writer, s.logger, request, &body, `{"index": 0, "sha": "…"}`) {
		return
	}

	stash, err := s.stashTarget(request, opened.Path, body)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	stashes, err := s.runner.DropStash(uninterrupted(request), opened.Path, stash.Index)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, stashesPayload{Stashes: stashes})
}

// stashable resolves the repository and refuses one that cannot stash at all.
//
// Two conditions, and neither is about a branch. A bare repository has no work
// tree to save or restore. A repository in the middle of a merge or a rebase
// cannot stash either — git answers "a.txt: needs merge", which says nothing
// about the merge — and cannot usefully have one applied on top of the
// conflict it is already holding.
//
// What is NOT here is a branch. `git stash` on a detached HEAD records
// "(no branch)" and works, and every other operation in this API needing
// branchUnderfoot needs it because it writes to a branch. This one writes to
// the work tree.
func (s *Server) stashable(request *http.Request) (*repo.Repo, error) {
	opened, err := s.workTree(request)
	if err != nil {
		return nil, err
	}
	if err := s.repositoryIdle(opened); err != nil {
		return nil, err
	}
	return opened, nil
}

// statusForStashableError turns stashable's refusal into an HTTP code.
//
// Two error families meet in that one function and neither mapping knows about
// the other. A repository this daemon never opened, or a bare one, is
// workTree's answer and comes back 404 or 409 through statusForWorkTreeError.
// A repository in the middle of a merge is repositoryIdle's, and every other
// operation in this API reaches that sentinel through statusForOperationError.
//
// So this asks whichever one owns the sentinel rather than teaching either of
// them a case belonging to the other — which is how the busy refusal came back
// as a 404 the first time, saying exactly the right sentence under exactly the
// wrong code.
func statusForStashableError(err error) int {
	if errors.Is(err, errRepositoryBusy) {
		return statusForOperationError(err)
	}
	return statusForWorkTreeError(err)
}

// stashTarget resolves the position a request names and refuses one that moved.
//
// A method on the server only so the two fields arrive together: the refusal
// itself is git.StashTarget's, and the reason it cannot be agreesOnCommit is
// at the top of this file.
func (s *Server) stashTarget(
	request *http.Request, dir string, body stashRequest,
) (git.Stash, error) {
	return s.runner.StashTarget(request.Context(), dir, body.Index, body.SHA)
}
