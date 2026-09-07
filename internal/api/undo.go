package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Undo over HTTP: reverse the most recent action yagit knows how to reverse.
//
// Plan then execute, leased by object names — docs/adr/0031. Works on a
// detached HEAD (checkout undo often returns from one), so it asks
// repositoryIdle rather than branchUnderfoot.
//
// Almost all of it is a reading of the HEAD reflog. The exception is a branch
// this repository deleted, which the reflog cannot see at all: that one is
// answered from what was read before the delete ran, and it wins while HEAD
// has not moved since (docs/adr/0032, and deleted.go for the record).

var undoOperation = branchOperation{gerund: "undoing", preposition: "on"}

type undoOffer struct {
	Kind    git.UndoKind `json:"kind"`
	Subject string       `json:"subject"`
	Head    string       `json:"head"`

	// Branch is sent with the offer and not only with the plan, because it is
	// what the button has to say: "Restore side" names the thing coming back,
	// where "Undo" beside a commit subject would name the commit it lands on.
	Branch string `json:"branch"`
}

type undoRequest struct {
	Kind   git.UndoKind `json:"kind"`
	Into   string       `json:"into"`
	Head   string       `json:"head"`
	To     string       `json:"to"`
	ToRef  string       `json:"to_ref"`
	Branch string       `json:"branch"`
	Detach bool         `json:"detach"`
}

// undoPreview is the one place that decides which of the two sources answers,
// so the offer, the plan and the run can never disagree about what Undo means
// at this moment.
//
// The remembered deletion is tried first and the reflog is the fallback,
// because a record that is still valid is by construction more recent than
// anything the reflog holds: it stops being valid the moment HEAD moves, and
// every reflog entry is a HEAD movement.
func (s *Server) undoPreview(ctx context.Context, opened *repo.Repo) (git.UndoPreview, error) {
	if deleted, ok := s.deleted.lookup(opened.ID); ok {
		preview, err := s.runner.PreviewUndoBranchDeletion(
			ctx, opened.Path, deleted.Name, deleted.SHA, deleted.Head)
		if err == nil {
			return preview, nil
		}
		// Spent: HEAD has moved on, the branch is back, or `git gc` took the
		// commits. Dropped rather than kept and refused every time it is read,
		// and the reflog gets the question.
		//
		// Those three and nothing else. Every other error here is git failing
		// to answer — ReadHEAD or resolveCommit could not run — and a
		// subprocess that did not start says nothing about whether the branch
		// can still come back. Forgetting on one would spend the only record
		// of a tip that exists nowhere git will name, and the offer would
		// quietly become whatever the reflog holds instead: a button that read
		// "Restore side" answering with a reset. The record is kept and the
		// error travels.
		if !errors.Is(err, git.ErrNothingToUndo) &&
			!errors.Is(err, git.ErrBranchIsBack) &&
			!errors.Is(err, git.ErrDeletedWorkIsGone) {
			return git.UndoPreview{}, err
		}
		s.deleted.forget(opened.ID)
	}
	return s.runner.PreviewUndo(ctx, opened.Path)
}

// handleUndoOffer says whether the tip can be undone.
func (s *Server) handleUndoOffer(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}
	if err := s.repositoryIdle(opened); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.undoPreview(request.Context(), opened)
	if err != nil {
		if errors.Is(err, git.ErrNothingToUndo) || errors.Is(err, git.ErrCannotUndoRoot) {
			writeJSON(writer, s.logger, http.StatusOK, map[string]any{"available": false})
			return
		}
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, map[string]any{
		"available": true,
		"offer": undoOffer{
			Kind:    preview.Kind,
			Subject: preview.Subject,
			Head:    preview.Head,
			Branch:  preview.Branch,
		},
	})
}

// handlePlanUndo says what undoing the tip would run.
func (s *Server) handlePlanUndo(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}
	if err := s.repositoryIdle(opened); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.undoPreview(request.Context(), opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, preview)
}

// handleUndo carries out the plan.
func (s *Server) handleUndo(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}
	if err := s.repositoryIdle(opened); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	var body undoRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"kind":"…","into":"…","head":"…","to":"…","to_ref":"…","branch":"…","detach":false}`) {
		return
	}

	preview, err := s.undoPreview(request.Context(), opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if preview.Kind != body.Kind {
		writeError(writer, s.logger, http.StatusConflict,
			errors.New("what undo would reverse has changed — read undo again"))
		return
	}
	if err := agreesOnCommit(body.Head, preview.Head); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if err := agreesOnCommit(body.To, preview.To); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if body.ToRef != preview.ToRef || body.Detach != preview.Detach || body.Branch != preview.Branch {
		writeError(writer, s.logger, http.StatusConflict,
			errors.New("what undo would reverse has changed — read undo again"))
		return
	}

	// Commit, amend and reset undo all move a branch tip: refuse if HEAD left
	// that branch. Checkout undo often starts detached, so an empty into is
	// allowed then — and a reset on a detached HEAD leaves one too.
	if movesTheBranch(preview.Kind) && preview.Into != "" {
		if err := undoOperation.agreesOnBranch(body.Into, preview.Into); err != nil {
			writeError(writer, s.logger, statusForOperationError(err), err)
			return
		}
	}

	if err := s.runner.Undo(uninterrupted(request), opened.Path, preview); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// A record that has been spent stops being an offer. The branch is back,
	// so the next read would refuse it and drop it anyway; doing it here means
	// the answer to THIS request already has the right Undo behind it.
	if preview.Kind == git.UndoBranchDelete {
		s.deleted.forget(opened.ID)
	}

	s.answerWithRefs(writer, request, opened)
}

// movesTheBranch says whether reversing this kind writes to the branch HEAD is
// on, rather than only to HEAD itself. Checkout undo is the exception, and it
// is why the question is asked at all: switching back is legitimate from a
// detached HEAD, where there is no branch name to agree about.
func movesTheBranch(kind git.UndoKind) bool {
	switch kind {
	case git.UndoCommit, git.UndoAmend, git.UndoReset:
		return true
	default:
		return false
	}
}
