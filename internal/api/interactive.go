package api

import (
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Rewriting the commits after a selected one, from a plan, over HTTP.
//
// Two routes with the shape reset uses: one says what could be planned, the
// other carries a plan out. What differs from every other operation here is
// where the decision lives. A merge or a rebase onto a branch is one of three
// things the daemon READS and the user approves; an interactive rebase is a
// list the user WRITES, and the daemon's job is to refuse the lists that are
// not about this repository.
//
// So the plan route answers with a range rather than with an outcome: the base
// it resolved, and the commits after it, oldest first, in the order a todo
// list is executed. The client arranges them. The run route reads the range
// again and refuses any plan that is not exactly those commits, each once —
// which is the lease of docs/adr/0022 in its strongest form here, because a
// commit landing on the branch while the dialog was open makes the plan a list
// about a history that no longer exists.
//
// The client never assembles a command and never writes a todo list. It sends
// object names and verbs; git.TodoList writes the file, from the subjects the
// daemon read, and git.CheckPlan is what stands between a request and a
// rewrite of somebody's branch.
//
// The refusals that come before any of it — HEAD on no branch, a repository
// already busy, HEAD somewhere other than the dialog said — are in branchop.go,
// where merge and rebase ask them in the same words.

// No preposition, unlike merge's and rebase's: those name where the OTHER
// branch stands, and there is no other branch here. What a plan is written
// after is a commit on the branch HEAD is already on, so the only refusal this
// borrows is the one about HEAD having moved.
var interactiveRebaseOperation = branchOperation{gerund: "rewriting"}

// interactiveRebasePlanRequest names the commit the plan is about.
type interactiveRebasePlanRequest struct {
	// Commit is a revision, and it is the BASE: the plan covers what comes
	// after it, never the commit itself. checkRevision refuses anything git
	// could read as an option before it reaches git.
	Commit string `json:"commit"`
}

// interactiveRebaseRequest is the plan the user wrote, sent back to be run.
type interactiveRebaseRequest struct {
	// Base is the commit the plan was built after. Sent back as the full name
	// the plan resolved, so a short SHA that became ambiguous since cannot
	// silently name a different object.
	Base string `json:"base"`

	// From is the branch the dialog said this was rewriting, and it is here to
	// be disagreed with. `git rebase` acts on wherever HEAD happens to be,
	// which is exactly the fact a dialog cannot keep watching.
	From string `json:"from"`

	// Steps is the plan, in the order it will be executed. Every commit after
	// the base appears exactly once — checked here against a fresh reading of
	// the range, never assumed. See git.CheckPlan.
	Steps []git.RebaseStep `json:"steps"`
}

// interactiveRebasePlan is the range a plan may be written over.
//
// No outcome and no counts, because there is nothing yet to count: what this
// answers is the material, and the operation does not exist until the client
// has arranged it. The command is here all the same — it is the same line
// whatever the plan turns out to be, and the dialog shows it beside the rows.
type interactiveRebasePlan struct {
	Command string `json:"command"`

	// Base is the full object name of the commit the plan starts after, and
	// Subject is what it says. The dialog names it: a plan is "the commits
	// after this one", and a hash on its own does not say which one.
	Base    string `json:"base"`
	Subject string `json:"subject"`

	// From is the branch HEAD is on, read here rather than sent. A name the
	// browser worked out for itself would be a second definition of what is
	// being rewritten.
	From string `json:"from"`

	// Commits are what the plan covers, oldest first — the order a todo list
	// is written and executed in. Drawn in that order too, so the list the
	// user reorders and the list git runs are one list.
	Commits []git.Commit `json:"commits"`
}

// interactiveRebaseResult is what happened, and it is not only the references.
//
// A plan holding an `edit` ends with git stopped in the middle of it, having
// exited zero — see git.RebaseInteractive, where that is spelled out. Nothing
// in a bare reference list distinguishes that from a rebase that finished, so
// the state is read once here and travels with the answer. The alternative is
// an interface that reports success over a repository sitting halfway through
// a rewrite, and finds out two seconds later from a poll.
type interactiveRebaseResult struct {
	// Embedded, so an answer that finished is byte for byte the answer every
	// other branch operation gives, with the two fields below absent.
	refsPayload

	// Stopped says the plan is not finished: git is waiting, and the banner
	// the changes panel draws is what carries on from here.
	Stopped bool `json:"stopped,omitempty"`

	// Step and Total are how far it got, from the same files the banner reads.
	// Only ever shown together — "3" on its own says nothing.
	Step  int `json:"step,omitempty"`
	Total int `json:"total,omitempty"`
}

// handlePlanInteractiveRebase answers with the commits a plan may cover.
func (s *Server) handlePlanInteractiveRebase(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body interactiveRebasePlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"commit": "…"}`) {
		return
	}

	from, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	base, subject, commits, err := s.runner.PlanRange(request.Context(), opened.Path, body.Commit)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, interactiveRebasePlan{
		Command: git.CommandLine(git.InteractiveRebaseArgs(base)),
		Base:    base,
		Subject: subject,
		From:    from,
		Commits: commits,
	})
}

// handleInteractiveRebase carries out a plan.
//
// A conflict is not an error this hides: git stops partway through the list,
// writes the markers into the work tree and exits non-zero, and that reaches
// the interface as the failure it is — with git's own account, and with the
// repository in a state the banner names and the conflict screen resolves.
//
// A stop at an `edit` is the same state reached deliberately, and it arrives
// as a success. Which of the two happened is read from the git directory
// afterwards rather than from the exit code, because the exit code cannot say.
func (s *Server) handleInteractiveRebase(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body interactiveRebaseRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"base": "…", "from": "…", "steps": [{"commit": "…", "instruction": "pick"}]}`) {
		return
	}

	from, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// Before anything about the range, because this is the question whose
	// answer changed: a request that named feature and arrived on main must
	// hear that HEAD moved, not something about commits it can no longer see.
	if err := interactiveRebaseOperation.agreesOnBranch(body.From, from); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The range again, from the repository rather than from the request. Every
	// refusal PlanRange makes is made a second time here, and it has to be:
	// the dialog was open while somebody could commit, merge or check out
	// something else.
	base, _, commits, err := s.runner.PlanRange(request.Context(), opened.Path, body.Base)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The resolved name, not the one the client typed. A plan answered with a
	// full SHA and a run that accepted a different short form of a different
	// object would be two definitions of what was approved.
	if err := agreesOnCommit(body.Base, base); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The lease, and it is a stronger one than any other operation here has.
	// Merge and rebase compare an outcome — one word — so a plan can go stale
	// in ways the word does not capture. This compares the plan against the
	// commits themselves, so a commit landing on the branch, or one leaving
	// it, is refused by name rather than by a count nobody was shown.
	if err := git.CheckPlan(body.Steps, commits); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.RebaseInteractive(uninterrupted(request), opened.Path, base,
		git.TodoList(body.Steps, commits)); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithPlanOutcome(writer, request, opened)
}

// answerWithPlanOutcome answers with the references and with whether git is
// still in the middle of the plan.
//
// The state is read from the git directory — a handful of stats, no subprocess
// — and it is read AFTER the command rather than polled for afterwards,
// because the gap between the two is a window in which the interface would be
// reporting a finished rebase over a stopped one.
//
// A state that cannot be read is not fatal here and does not become an error.
// The rebase has already run; refusing to answer would leave the client with
// no references either, over a question that only decides the wording of a
// toast. The banner reads the same files two seconds later and is the second
// chance this deliberately leans on.
func (s *Server) answerWithPlanOutcome(writer http.ResponseWriter, request *http.Request, opened *repo.Repo) {
	payload, err := s.readRefs(request.Context(), opened)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	result := interactiveRebaseResult{refsPayload: payload}

	if state, err := git.ReadState(opened.StateDir()); err != nil {
		s.logger.Warn("could not read what the repository is in the middle of after a plan ran",
			"error", err)
	} else if state.InProgress() {
		result.Stopped = true
		result.Step, result.Total = state.Step, state.Total
	}

	writeJSON(writer, s.logger, http.StatusOK, result)
}
