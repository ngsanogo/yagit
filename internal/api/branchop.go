package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// What every operation on the branch HEAD is on has to establish first.
//
// Merge brings another branch in, rebase replays onto one, and cherry-pick and
// revert take a commit onto the one HEAD is on. They are different commands
// asking the same questions: is HEAD on a branch and the repository free to
// start anything, is HEAD still where the dialog said it was, and is the plan
// the user approved still true. Merge and rebase also ask whether the other
// branch is a different one from HEAD's. Each answered them once, in prose
// that differed only by the word for what was about to happen — four sentinel
// errors apiece, two of them character for character the same, and one arm of
// statusForOperationError growing by three every time an operation was added.
//
// So the question is asked here, once, and the word is passed in. What stays
// in merge.go, rebase.go, cherrypick.go and revert.go is what genuinely
// differs: the shape of the request, the reading, and the command.

// errRepositoryBusy: the repository is already in the middle of something.
//
// git refuses an operation started on top of a stopped merge or a
// half-finished rebase — "You have not concluded your merge" — and it refuses
// it after a dialog has promised one. The state is read before any plan is
// described, so no confirmation ever offers an operation that cannot happen,
// and it is named here rather than left to git because what the user has to do
// about it is finish or abort the other operation, which the interface already
// offers.
var errRepositoryBusy = errors.New("the repository is in the middle of another operation")

// errHeadMoved: HEAD is no longer on the branch the request names.
//
// The same guard as operationRequest.Operation, and it exists for the same
// sequence: the dialog says "Merge pickup into main", the user checks out
// release in a terminal or a second tab, and the button they have been looking
// at acts on a branch its own title never mentioned. The references are re-read
// from the event stream, but a dialog already open holds what it was given.
var errHeadMoved = errors.New("HEAD is not on the branch this operation names")

// errPlanChanged: the operation is no longer the one the dialog described.
//
// The two branches are read again before the command runs, and this is what
// says the reading came back different: the sentence the user approved — a
// pointer moving, a commit under their signature, three commits rewritten,
// nothing at all — is not what would happen now. Refused rather than run,
// because the alternative is doing the right thing to a repository while
// telling somebody it did the other thing.
var errPlanChanged = errors.New("this operation is no longer what the plan described")

// errSameBranch: the branch named is the one HEAD is already on.
//
// Refused rather than handed to git, which would answer "Already up to date" or
// "Current branch is up to date" — both true, and neither the sentence that
// explains why the button should not have been there. An API refusal rather
// than a git one, like errHeadMoved above it: what makes it wrong is the
// request, not anything in the repository.
var errSameBranch = errors.New("the branch named is the one HEAD is already on")

// branchOperation is what one of these calls itself in a refusal.
//
// Two words, because two is all that differs. Everything else a refusal has to
// say — which branch, which outcome, what to do about it — is a fact read from
// the repository, and facts do not need an operation to name them.
type branchOperation struct {
	// gerund is the operation as it happens: "merging", "rebasing". Every
	// refusal below is written around it.
	gerund string

	// preposition is where the other branch stands. A merge comes INTO the
	// branch HEAD is on; a rebase goes ONTO another.
	preposition string
}

var (
	mergeOperation  = branchOperation{gerund: "merging", preposition: "into"}
	rebaseOperation = branchOperation{gerund: "rebasing", preposition: "onto"}
)

// branchUnderfoot is the branch an operation would act on, and whether one can
// be started at all.
//
// Both routes of both operations ask this first, and they ask it for the same
// reason: a plan describing something the run route would refuse is a dialog
// that exists to be disappointed. Two things make the answer no. HEAD may be on
// no branch, which currentBranch reports — there is nothing to act on. And the
// repository may already be in the middle of a merge, a rebase or a
// cherry-pick, which git refuses to start another operation on top of; the
// interface has a banner for that state and buttons that finish or abort it, so
// the refusal names the operation and leaves the user beside them.
//
// No branchOperation, because nothing in the answer depends on which operation
// is asking. A merge, a rebase, a cherry-pick or a revert already in progress
// all refuse the same way.
func (s *Server) branchUnderfoot(request *http.Request, opened *repo.Repo) (string, error) {
	branch, err := s.currentBranch(request.Context(), opened)
	if err != nil {
		return "", err
	}
	if err := s.repositoryIdle(opened); err != nil {
		return "", err
	}
	return branch, nil
}

// repositoryIdle refuses an operation started on top of one already running.
//
// The second half of branchUnderfoot, on its own, because not everything that
// has to ask this needs a branch. Stashing is the case: `git stash` works
// perfectly well on a detached HEAD — it records "(no branch)" and says so —
// so demanding a branch name first would refuse a real operation for a reason
// that has nothing to do with what is in the way. What IS in the way is the
// same for both: git will not stash during a conflicted merge either, and it
// answers "a.txt: needs merge" rather than anything about the merge.
//
// Read from the git directory rather than run — see git.ReadState — so this
// costs a handful of stats and no subprocess.
func (s *Server) repositoryIdle(opened *repo.Repo) error {
	state, err := git.ReadState(opened.StateDir())
	if err != nil {
		return err
	}
	if state.Operation != git.OperationNone {
		return fmt.Errorf("%w: finish or abort the %s first", errRepositoryBusy, state.Operation)
	}
	return nil
}

// otherBranch is the name after the two checks that belong to the API rather
// than to git: there has to be one, and it must not be the branch HEAD is on.
func (op branchOperation) otherBranch(raw, current string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", git.ErrNoBranchName
	}
	if name == current {
		return "", fmt.Errorf("%w: %s %s itself does nothing",
			errSameBranch, op.gerund, op.preposition)
	}
	return name, nil
}

// agreesOnBranch refuses an operation aimed at a branch HEAD has left.
//
// The empty string is not a wildcard and is refused like any other mismatch,
// for the reason agreesOnOperation refuses one: a client that names no branch
// is a client that did not look, and these routes write to whatever HEAD is on.
//
// Trimmed like every other name on these routes. A name that arrives with a
// newline on it is the branch it names, and comparing it raw answers "HEAD is
// now on main, not main " — a refusal that reads as a contradiction, over a
// repository that never moved.
func (op branchOperation) agreesOnBranch(claimed, actual string) error {
	claimed = strings.TrimSpace(claimed)
	if claimed == actual {
		return nil
	}
	if claimed == "" {
		return fmt.Errorf("%w: the request names none, and HEAD is on %s", errHeadMoved, actual)
	}
	return fmt.Errorf("%w: HEAD is now on %s, not %s — read the references again before %s",
		errHeadMoved, actual, claimed, op.gerund)
}

// agreesOnCommit refuses a run that names a different object from the plan's.
//
// The plan answers with the full object name git resolved. A run that sends
// back a short form, or a name that has become another commit since, is not
// the operation anybody approved — and it would be carried out looking
// entirely correct. Shared because cherry-pick and revert ask it character for
// character.
func agreesOnCommit(claimed, resolved string) error {
	if claimed == resolved {
		return nil
	}
	return fmt.Errorf("%w: the commit is now %s, not %s — read the plan again",
		errPlanChanged, resolved, claimed)
}

// agreesOnPlan refuses an operation that has become a different one.
//
// Generic over the outcome, because the two enumerations are two vocabularies
// for one comparison and neither route has anything to add to it. `subject` is
// the operation said in words — "merging pickup into main", "rebasing feature
// onto main" — assembled by the caller, since the order the two branches are
// named in is the one thing that differs between them.
//
// The counts are deliberately not compared. A commit landing on either branch
// changes "brings 3 commits" without changing what the command does, and a
// confirmation that had to be reopened every time anybody committed anywhere
// would teach people to click through it.
func agreesOnPlan[Outcome ~string](approved, actual Outcome, subject string) error {
	if approved == actual {
		return nil
	}
	return fmt.Errorf("%w: %s would now be %q, not %q — read the plan again",
		errPlanChanged, subject, actual, approved)
}
