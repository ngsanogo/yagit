package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// The linked checkouts a repository has, over HTTP.
//
// Read as a collection, written as two operations — the split the rest of the
// route table already uses. Making one takes a destination under YAGIT_ROOT,
// so it goes through the same PrepareNewRepositoryPath that clone and init do:
// a worktree is a directory this daemon creates, and where it may create one is
// one question with one answer.
//
// Removing one is destructive and shows its command first, like every other
// operation that deletes something a person made.

type worktreeView struct {
	git.Worktree

	// Current marks the checkout this request is about. The list is the same
	// for every worktree of one repository — they share a git directory — so
	// without it the interface cannot say which row is the tab you are on.
	Current bool `json:"current"`
}

type addWorktreeRequest struct {
	// Path is the absolute destination under YAGIT_ROOT. The parent must
	// exist; the destination itself must not.
	Path string `json:"path"`

	// Ref is what to check out there: a branch that exists, or — with Detach —
	// a tag or a commit. Empty means HEAD, which is git's own default.
	Ref string `json:"ref"`

	// NewBranch makes a branch at Ref rather than checking out an existing
	// one. `git worktree add -b`.
	NewBranch string `json:"new_branch"`

	// Detach checks out Ref with no branch on it, which is the only way a tag
	// or a remote-tracking name can be checked out at all.
	Detach bool `json:"detach"`
}

type removeWorktreeRequest struct {
	Path  string `json:"path"`
	Force bool   `json:"force"`
}

// handleWorktrees lists every checkout of the repository.
func (s *Server) handleWorktrees(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}
	s.answerWithWorktrees(writer, request, opened)
}

// handlePlanAddWorktree says what making one would run, without making it.
func (s *Server) handlePlanAddWorktree(writer http.ResponseWriter, request *http.Request) {
	// Asked first, as on every other plan route: a command shown for a
	// repository nobody has open is a confirmation whose button then answers
	// 404, and the refusal belongs before the line rather than after it.
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	body, destination, ok := s.readAddWorktreeRequest(writer, request)
	if !ok {
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, map[string]any{
		"command": git.CommandLine(
			git.AddWorktreeArgs(destination, body.Ref, body.NewBranch, body.Detach)),
		"path": destination,
	})
}

// handleAddWorktree makes another checkout of the repository.
//
// The new checkout is NOT opened as a repository afterwards, unlike clone and
// init. It shares this repository's history and its identity is the same git
// directory, so opening it would either be a second tab drawing the same graph
// or the registry answering with the tab that is already there. Opening it is
// a separate click, through the route that opens any path.
func (s *Server) handleAddWorktree(writer http.ResponseWriter, request *http.Request) {
	body, destination, ok := s.readAddWorktreeRequest(writer, request)
	if !ok {
		return
	}

	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	// Re-check immediately before git runs: Prepare confirmed absence earlier
	// in this request, and a symlink planted since would make worktree add
	// write outside the root. git refuses an existing path, so we cannot
	// Claim (mkdir) the way clone does — only shrink the window and verify
	// afterwards.
	if _, err := os.Lstat(destination); err == nil {
		writeError(writer, s.logger, http.StatusConflict,
			fmt.Errorf("%q: %w", destination, repo.ErrDestinationExists))
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	if err := s.runner.AddWorktree(
		uninterrupted(request), opened.Path, destination, body.Ref, body.NewBranch, body.Detach,
	); err != nil {
		writeError(writer, s.logger, statusForWorktreeError(err), err)
		return
	}

	if _, err := s.registry.PathWithinRoot(destination); err != nil {
		// git wrote through a symlink race. Remove the linked checkout if git
		// recorded it; the outside tree is left alone — deleting past the root
		// is exactly what this check exists to refuse.
		if removeErr := s.runner.RemoveWorktree(
			uninterrupted(request), opened.Path, destination, true,
		); removeErr != nil {
			s.logger.Warn("could not remove a worktree that resolved outside the root",
				"path", destination, "error", removeErr)
		}
		writeError(writer, s.logger, statusForWorktreeError(err), err)
		return
	}

	s.answerWithWorktrees(writer, request, opened)
}

// handlePlanRemoveWorktree says what removing one would run.
func (s *Server) handlePlanRemoveWorktree(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body removeWorktreeRequest
	if !decodeBody(writer, s.logger, request, &body, `{"path": "…", "force": false}`) {
		return
	}
	if err := s.removableWorktree(request, opened, body.Path); err != nil {
		writeError(writer, s.logger, statusForWorktreeError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.RemoveWorktreeArgs(strings.TrimSpace(body.Path), body.Force)),
	})
}

// handleRemoveWorktree deletes a linked checkout.
func (s *Server) handleRemoveWorktree(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body removeWorktreeRequest
	if !decodeBody(writer, s.logger, request, &body, `{"path": "…", "force": false}`) {
		return
	}
	if err := s.removableWorktree(request, opened, body.Path); err != nil {
		writeError(writer, s.logger, statusForWorktreeError(err), err)
		return
	}

	if err := s.runner.RemoveWorktree(
		uninterrupted(request), opened.Path, strings.TrimSpace(body.Path), body.Force,
	); err != nil {
		writeError(writer, s.logger, statusForWorktreeError(err), err)
		return
	}

	s.answerWithWorktrees(writer, request, opened)
}

// handlePruneWorktrees forgets the checkouts whose directories are gone.
func (s *Server) handlePruneWorktrees(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	if err := s.runner.PruneWorktrees(uninterrupted(request), opened.Path); err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	s.answerWithWorktrees(writer, request, opened)
}

// removableWorktree refuses what `git worktree remove` would refuse, before
// the confirmation is drawn rather than after it is answered.
//
// The main working tree is the case that matters: git says "is a main working
// tree" and exits 128, which is a correct sentence about a button that should
// not have been offered. The list already knows which one it is.
//
// A path outside YAGIT_ROOT is refused too. Add is root-bounded; remove must
// be the same question: a linked checkout made in a terminal beyond the root
// is visible to `git worktree list`, and deleting it through this daemon would
// be directory-tree removal outside the security boundary.
func (s *Server) removableWorktree(request *http.Request, opened *repo.Repo, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return git.ErrNoWorktreePath
	}

	if _, err := s.registry.PathWithinRoot(path); err != nil {
		return err
	}

	worktrees, err := s.runner.Worktrees(request.Context(), opened.Path)
	if err != nil {
		return err
	}
	for _, worktree := range worktrees {
		if sameDirectory(worktree.Path, path) && worktree.Main {
			return git.ErrMainWorktree
		}
	}
	return nil
}

// answerWithWorktrees is the list every route here answers with, the read
// included: one reading of what a checkout is called and which one this tab is
// on, so a row cannot mean one thing after a GET and another after a remove.
func (s *Server) answerWithWorktrees(
	writer http.ResponseWriter, request *http.Request, opened *repo.Repo,
) {
	worktrees, err := s.runner.Worktrees(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	// Built with make rather than declared: a nil slice marshals to null, and
	// a list handed null is a list that crashes.
	views := make([]worktreeView, 0, len(worktrees))
	for _, worktree := range worktrees {
		if !s.worktreeVisible(worktree) {
			continue
		}
		views = append(views, worktreeView{
			Worktree: worktree,
			Current:  sameDirectory(worktree.Path, opened.Path),
		})
	}
	writeJSON(writer, s.logger, http.StatusOK, map[string]any{"worktrees": views})
}

// worktreeVisible says whether a checkout may appear in the list.
//
// Paths under the root are shown. A prunable record whose directory is gone
// is shown too — prune forgets git's metadata and deletes nothing on disk, so
// an outside path that no longer exists is safe to offer that button for.
// An outside checkout that still exists is omitted: the interface must not
// invite a remove this daemon refuses, and must not draw a path outside the
// boundary as if it were part of the allowed tree.
func (s *Server) worktreeVisible(worktree git.Worktree) bool {
	if _, err := s.registry.PathWithinRoot(worktree.Path); err == nil {
		return true
	}
	return worktree.Prunable
}

// readAddWorktreeRequest decodes the body and checks the destination against
// the root. Shared by the plan and the run so the confirmation cannot approve
// a path the run would then refuse.
func (s *Server) readAddWorktreeRequest(
	writer http.ResponseWriter, request *http.Request,
) (addWorktreeRequest, string, bool) {
	var body addWorktreeRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"path": "…", "ref": "…", "new_branch": "", "detach": false}`) {
		return addWorktreeRequest{}, "", false
	}

	body.Path = strings.TrimSpace(body.Path)
	body.Ref = strings.TrimSpace(body.Ref)
	body.NewBranch = strings.TrimSpace(body.NewBranch)

	if body.Path == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoWorktreePath)
		return addWorktreeRequest{}, "", false
	}

	destination, err := s.registry.PrepareNewRepositoryPath(body.Path)
	if err != nil {
		writeError(writer, s.logger, statusForClonePathError(err), err)
		return addWorktreeRequest{}, "", false
	}

	return body, destination, true
}

// sameDirectory compares two paths the way the filesystem would, without
// asking it: both come from git or from the registry, both are absolute, and
// neither needs resolving again. Cleaned rather than compared raw, because
// git's list and the registry's record can differ by a trailing separator.
func sameDirectory(one, other string) bool {
	return filepath.Clean(one) == filepath.Clean(other)
}

func statusForWorktreeError(err error) int {
	switch {
	case errors.Is(err, git.ErrNoWorktreePath):
		return http.StatusBadRequest
	case errors.Is(err, git.ErrMainWorktree):
		// The request names a real worktree and is well formed; it is the
		// repository that makes the answer no.
		return http.StatusConflict
	case errors.Is(err, repo.ErrOutsideRoot):
		return http.StatusForbidden
	case errors.Is(err, git.ErrBadRevision), errors.Is(err, git.ErrNoBranchName):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
