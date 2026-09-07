package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Branches over HTTP: the routes that move HEAD, and the routes that make and
// unmake the branches it moves between.
//
// It answers with the references and where HEAD now sits — the same payload
// GET /refs sends — because that is what it changed, and because a sidebar
// that had to ask again would draw one frame of the branch it just left.
//
// The working directory changed too and is NOT in the answer. Sending it would
// cost a second `git status` on every checkout, and the interface polls that
// status twice a second anyway (ADR 0015); what it does instead is drop what it
// holds, which is one request rather than two.

// switchRequest names what to check out.
//
// Two fields rather than one, and the boolean is not a mode flag on a single
// operation: `git switch main` and `git switch --detach main` leave the
// repository in two different places, and only one of them is what "check out
// main" means. A client that could not say which it wanted would be asking the
// daemon to guess from the shape of the string it sent.
type switchRequest struct {
	// Ref is the branch to move onto, or the commit-ish to detach at. It
	// reaches git after `--`, so anything git accepts as a revision works
	// here: a tag, `origin/main`, `HEAD~3`, a raw SHA.
	Ref string `json:"ref"`

	// Detach asks for the commit a reference names rather than the reference
	// itself. Required for anything that is not a local branch — a tag and a
	// remote-tracking branch are not places HEAD can sit — and the honest
	// answer for a row of the history, which is a commit and nothing else.
	Detach bool `json:"detach"`
}

// handleSwitch moves HEAD.
//
// No confirmation, and none is missing: a checkout destroys nothing. git
// carries uncommitted work across when it can, and refuses the whole switch
// when carrying it would overwrite something — with the list of files, in its
// own words, which travel to the interface intact. The one state this leaves
// that anybody could be surprised by is a detached HEAD, and the interface
// says so on screen for as long as it lasts rather than in a dialog nobody
// reads.
func (s *Server) handleSwitch(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body switchRequest
	if !decodeBody(writer, s.logger, request, &body, `{"ref": "…"}`) {
		return
	}

	if body.Detach {
		err = s.runner.Detach(uninterrupted(request), opened.Path, body.Ref)
	} else {
		err = s.runner.Switch(uninterrupted(request), opened.Path, body.Ref)
	}
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// createRequest names a branch to make.
type createRequest struct {
	// Name is the branch to create. It reaches git where git reads a name and
	// never where it reads an option; see internal/git/branch.go.
	Name string `json:"name"`

	// Start is where the branch begins — any revision git accepts. Absent
	// means HEAD, which is git's own default and is left to git rather than
	// resolved here, so the command in the log panel is the one a person would
	// have typed.
	Start string `json:"start"`

	// Switch asks to stand on the new branch. Two different commands, not a
	// flag on one: `git branch` makes a branch and leaves you where you are,
	// `git switch --create` makes it and moves you onto it, and the second
	// cannot half-happen the way running the two in sequence can.
	Switch bool `json:"switch"`
}

// renameRequest names a branch and what to call it instead.
type renameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// deleteRequest names a branch to remove.
type deleteRequest struct {
	Name string `json:"name"`

	// Force is `-D` rather than `-d`: it deletes a branch holding commits no
	// other branch can reach. The interface asks first, and what it shows it
	// asks with is the command plannedDelete answers.
	Force bool `json:"force"`
}

// plannedCommand is what a destructive operation would run.
//
// Answered before it runs, so the confirmation shows the exact line rather
// than a sentence about it — and answered by the daemon rather than assembled
// in the browser, because the line the user is shown and the line git receives
// have to have one definition between them. The discard plan of workingdirectory.go is
// the same idea, for the same reason.
type plannedCommand struct {
	Command string `json:"command"`
}

// handleCreateBranch makes a local branch, and stands on it when asked to.
func (s *Server) handleCreateBranch(writer http.ResponseWriter, request *http.Request) {
	// The work tree, not just the repository: creating a branch needs none,
	// but standing on one does, and a bare repository has to be refused with
	// the sentence that says so rather than by git failing halfway.
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body createRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}

	if body.Switch {
		err = s.runner.CreateAndSwitch(uninterrupted(request), opened.Path, body.Name, body.Start)
	} else {
		err = s.runner.CreateBranch(uninterrupted(request), opened.Path, body.Name, body.Start)
	}
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// handleRenameBranch changes a local branch's name.
//
// No confirmation: a rename destroys nothing, and git refuses the one case
// that would — a name already taken — rather than overwriting it.
func (s *Server) handleRenameBranch(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body renameRequest
	if !decodeBody(writer, s.logger, request, &body, `{"from": "…", "to": "…"}`) {
		return
	}

	if err := s.runner.RenameBranch(uninterrupted(request), opened.Path, body.From, body.To); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// handlePlanDeleteBranch says what deleting would run, without running it.
func (s *Server) handlePlanDeleteBranch(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body deleteRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoBranchName)
		return
	}

	// A line, not an argument list: the same shape the discard plan answers
	// with, and the shape GitCommand draws. git.CommandLine is what makes it
	// something a person can also paste into their own terminal.
	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.DeleteBranchArgs(strings.TrimSpace(body.Name), body.Force)),
	})
}

// handleDeleteBranch removes a local branch.
func (s *Server) handleDeleteBranch(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body deleteRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}

	// Read where the branch stands BEFORE deleting it, because afterwards
	// nothing can: `git branch -d` leaves no HEAD-reflog entry and removes the
	// branch's own reflog with the branch. This is the whole of what makes the
	// deletion undoable — docs/adr/0032. A failure to read it is not a failure
	// to delete: the delete goes ahead and the undo offer is what is missing,
	// which is the right way round for a user who asked for the deletion.
	deleted, remembered := s.branchBeingDeleted(request, opened, body.Name)

	if err := s.runner.DeleteBranch(uninterrupted(request), opened.Path, body.Name, body.Force); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if remembered {
		s.deleted.record(opened.ID, deleted)
	}

	s.answerWithRefs(writer, request, opened)
}

// branchBeingDeleted reads the two objects a restore would need: the tip the
// branch holds, and where HEAD stands while it is deleted.
//
// Both are read together because a record with one of them is worth nothing —
// the tip is what the branch comes back at, and HEAD is what says the deletion
// is still the most recent thing that happened here (see deleted.go).
func (s *Server) branchBeingDeleted(
	request *http.Request, opened *repo.Repo, name string,
) (deletedBranch, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return deletedBranch{}, false
	}

	tip, err := s.runner.BranchTip(request.Context(), opened.Path, name)
	if err != nil {
		// Logged rather than swallowed, and not reported: the request is a
		// delete, and it is about to happen whatever this read did.
		s.logger.Warn("branch tip unread before delete, so its deletion is not undoable",
			"branch", forLog(name), "error", forLog(err.Error()))
		return deletedBranch{}, false
	}

	head, err := s.runner.ReadHEAD(request.Context(), opened.Path)
	if err != nil {
		s.logger.Warn("HEAD unread before a branch delete, so its deletion is not undoable",
			"branch", forLog(name), "error", forLog(err.Error()))
		return deletedBranch{}, false
	}

	return deletedBranch{Name: name, SHA: tip, Head: head.SHA}, true
}

// answerWithRefs sends the reference list, which is what every operation here
// changed. See the note at the top of the file for why it is the answer rather
// than something the client asks for afterwards.
func (s *Server) answerWithRefs(writer http.ResponseWriter, request *http.Request, opened *repo.Repo) {
	payload, err := s.readRefs(request.Context(), opened)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, payload)
}

// decodeBody reads a JSON request body, bounded, refusing anything it does not
// recognise.
//
// One function because five handlers in this file need the same four lines and
// the same sentence when they fail — and because a body limit that one of them
// forgot would be the one that mattered.
func decodeBody(writer http.ResponseWriter, logger *slog.Logger, request *http.Request, into any, shape string) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeError(writer, logger, http.StatusBadRequest,
			fmt.Errorf("unreadable request body, a %s object is expected: %w", shape, err))
		return false
	}
	return true
}
