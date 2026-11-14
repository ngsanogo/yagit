package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
)

// The shape of error responses enforces the project's central rule: when git
// fails, the interface must be able to show the exact command, its exit code
// and its raw stderr. Never "Something went wrong".

type errorPayload struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Message string      `json:"message"`
	Git     *gitFailure `json:"git,omitempty"`
}

type gitFailure struct {
	// Command is the line as it would be typed in a terminal, so the user can
	// replay it and work out what happened on their own.
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	ExitCode int      `json:"exit_code"`
	Stderr   string   `json:"stderr"`
}

// gitFailureOf pulls git's own account out of an error chain, or nil when git
// was not what failed.
//
// One function rather than one per response shape: a git failure carries the
// same four things wherever it surfaces, and a second copy of this mapping is
// how one of them ends up missing from one of the routes.
func gitFailureOf(cause error) *gitFailure {
	var gitError *git.Error
	if !errors.As(cause, &gitError) {
		return nil
	}
	return &gitFailure{
		Command:  gitError.CommandLine(),
		Args:     gitError.Args,
		ExitCode: gitError.ExitCode,
		Stderr:   gitError.Stderr,
	}
}

// maxLoggedValue bounds what one untrusted field may add to a log line.
//
// One bound for the two kinds that reach forLog, sized for the larger: a
// branch name is never near it, and a git failure's stderr — the useful half
// of the line it appears in — usually is not either. Without a bound, a name
// of a megabyte is a megabyte in the log, once per attempt, from a mistake
// nobody would notice until the disk filled.
const maxLoggedValue = 512

// forLog makes an untrusted string safe to put in a log line.
//
// A log line is a LINE. A branch name holding a newline writes a second entry
// of somebody else's choosing into the daemon's log, and the reader of that
// log has no way to tell it from one yagit wrote. slog's own handlers quote most
// of it, but which handler is configured is a property of how the daemon was
// started, and a guarantee that depends on that is not a guarantee.
//
// Two kinds of string reach it, and the second is the less obvious one: a git
// failure's text carries the command's raw stderr, and the command carried a
// name that came from a request. So the value travels back out of git and into
// the same line. Sanitised here rather than at the source, because the raw
// stderr is what the project promises a USER — on screen, in a JSON error,
// where a newline is a newline and not a forged record.
//
// Replaced with a space rather than dropped: a name written to trick a reader
// should still show that something was there.
func forLog(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	if len(value) > maxLoggedValue {
		return value[:maxLoggedValue] + "…"
	}
	return value
}

// writeError answers in JSON while keeping everything the cause said.
func writeError(writer http.ResponseWriter, logger *slog.Logger, status int, cause error) {
	detail := errorDetail{Message: cause.Error(), Git: gitFailureOf(cause)}

	logger.Warn("error response", "status", status, "error", cause)
	writeJSON(writer, logger, status, errorPayload{Error: detail})
}

// writeJSON serializes into a buffer before writing anything out.
//
// Encoding straight into the ResponseWriter would send the status header
// before an encoding failure surfaced, and the client would get a truncated
// body along with a 200 — the worst kind of response, because it looks fine.
func writeJSON(writer http.ResponseWriter, logger *slog.Logger, status int, payload any) {
	var body bytes.Buffer

	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		logger.Error("cannot encode JSON", "error", err, "payload", fmt.Sprintf("%T", payload))
		http.Error(writer, "cannot encode the response", http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)

	if _, err := body.WriteTo(writer); err != nil {
		// The client left mid-write. There is nobody left to answer, but the
		// incident gets recorded rather than swallowed.
		logger.Warn("response write interrupted", "error", err)
	}
}

// statusForOperationError turns a refusal into an HTTP code.
//
// Only the cases the interface acts on differently are told apart. Everything
// else is a git failure, and a git failure travels whole — command, exit code,
// raw stderr — whatever number it is wrapped in.
func statusForOperationError(err error) int {
	switch {
	case errors.Is(err, errDiffMoved):
		// Not a bad request and not a server fault: the state on disk moved
		// under a request that was correct when it was made.
		return http.StatusConflict
	case errors.Is(err, git.ErrCombinedDiff):
		// Not a bad request: the path is real and the side is one of the
		// three. It is the repository that is in a state where the question
		// has no answer, which is what 409 is for — and what the interface
		// turns into an offer to resolve the conflict instead.
		return http.StatusConflict
	case errors.Is(err, errUnresolvedConflict):
		// The request is well formed and the path is real; the file is in a
		// state where staging it would record something nobody meant. The
		// same reading as the combined diff above, and the same code.
		return http.StatusConflict
	case errors.Is(err, git.ErrNoOperation),
		errors.Is(err, errOperationMoved),
		errors.Is(err, errHeadMoved),
		errors.Is(err, errPlanChanged),
		errors.Is(err, git.ErrPlanNotTheRange),
		errors.Is(err, errRepositoryBusy),
		errors.Is(err, git.ErrWriteInProgress):
		// Nothing is in progress, something else now is, HEAD has left the
		// branch the operation was aimed at, or the operation it described has
		// become a different one. All of them are the repository having moved
		// under a dialog drawn from a reading up to two seconds old, and all
		// of them are answered by reading it again — which is what the
		// interface does with a 409 here. Three of them are shared by every
		// operation on the branch HEAD is on; see branchop.go, which is what
		// keeps this arm from growing with each one. A rebase plan that is no
		// longer the commits after its base is the same fact said about a
		// list: somebody committed, or a commit left the branch, while the
		// plan was open.
		return http.StatusConflict
	case errors.Is(err, git.ErrUnrelatedHistories),
		errors.Is(err, git.ErrCherryPickMerge),
		errors.Is(err, git.ErrRevertMerge),
		errors.Is(err, git.ErrRevertRoot),
		errors.Is(err, git.ErrRevertNotOnBranch),
		errors.Is(err, git.ErrResetNotOnBranch),
		errors.Is(err, git.ErrRangeHoldsMerge),
		errors.Is(err, git.ErrRangeEmpty),
		errors.Is(err, git.ErrPlanTooLong),
		errors.Is(err, git.ErrNothingToUndo),
		errors.Is(err, git.ErrCannotUndoRoot),
		errors.Is(err, git.ErrUndoStale):
		// Two branches that were started separately, a merge commit offered
		// for cherry-pick or revert without a mainline, a root that has no
		// parent to invert against, a revert of a commit the branch never
		// held, a reset onto foreign history, or a range that cannot be
		// planned over — a merge commit inside it, nothing in it at all, or
		// more commits than one list can be read as. The request is well
		// formed and nothing moved; it is the repository that holds a shape
		// git will not act on without being told more, and no amount of
		// re-reading changes that — but it is still the state answering rather
		// than the client, which is what keeps it beside the others.
		return http.StatusConflict
	case errors.Is(err, git.ErrBranchIsBack),
		errors.Is(err, git.ErrDeletedWorkIsGone):
		// The undo yagit remembered has been overtaken: somebody made a
		// branch of that name again, or `git gc` collected the commits the
		// deleted one held. Beside the pair above rather than a 400, for the
		// same reason — the request was right when the dialog drew it, and it
		// is the repository that answers differently now. Nothing to re-read
		// into a working request, so the interface reports it and drops the
		// offer.
		return http.StatusConflict
	case errors.Is(err, git.ErrActionBlocked):
		// git would take the instruction and yagit will not give it: a rebase
		// with a `squash` still ahead of it needs an editor the daemon has
		// nowhere to show. The repository is what refuses, not the request, so
		// it reads as a conflict and the reason travels with it.
		return http.StatusConflict
	case errors.Is(err, git.ErrActionUnavailable):
		// The instruction is not one this operation takes: `git merge --skip`
		// does not exist. Told from the pair above because it is the request
		// that is wrong rather than the moment it arrived in.
		return http.StatusBadRequest
	case errors.Is(err, git.ErrNoStash),
		errors.Is(err, git.ErrStashMoved):
		// The stack is shorter than the list on screen said, or the position
		// the confirmation named has come to hold a different stash. Both are
		// the repository having moved under a dialog — a stash pushed or
		// dropped in another window renumbers every entry below it — and both
		// are answered by reading the list again, which is what the interface
		// does with a 409. Beside errPlanChanged above rather than folded into
		// it, because a stash's plan is checked against a POSITION rather than
		// against an object name; see the note at the top of stash.go.
		return http.StatusConflict
	case errors.Is(err, git.ErrDetachedHEAD),
		errors.Is(err, git.ErrNoUpstream),
		errors.Is(err, git.ErrNothingToStash),
		errors.Is(err, git.ErrNoCommits):
		// The request is well formed and the repository exists; it is in a
		// state where what was asked for has no meaning. A detached HEAD has
		// no branch to push, a branch that follows nothing has nowhere to pull
		// from, and a work tree that matches HEAD has nothing to stash — and
		// the interface answers each with an offer rather than with a message,
		// which is why they are told apart at all.
		return http.StatusConflict
	case errors.Is(err, git.ErrNoRemote),
		errors.Is(err, git.ErrNoRemoteURL),
		errors.Is(err, git.ErrNothingSelected),
		errors.Is(err, git.ErrNotLineAddressable),
		errors.Is(err, git.ErrNoPaths),
		errors.Is(err, git.ErrNoRef),
		errors.Is(err, git.ErrNoBranchName),
		errors.Is(err, git.ErrNoTagName),
		errors.Is(err, git.ErrNoTagMessage),
		errors.Is(err, git.ErrBadRevision),
		errors.Is(err, git.ErrBadDiffContext),
		errors.Is(err, git.ErrStashMessageLine),
		errors.Is(err, errSameBranch),
		errors.Is(err, errNotAFile),
		errors.Is(err, git.ErrUnknownInstruction),
		errors.Is(err, git.ErrEmptyPlan),
		errors.Is(err, git.ErrCombineWithoutTarget),
		errors.Is(err, git.ErrEmptyMessage):
		// The body itself is the problem: a field left empty, a revision git
		// would read as an option, two fields that cannot both be true, or a
		// rebase plan that no reading of any repository could make sense of —
		// a verb yagit does not have, no steps at all, or a combine with
		// nothing above it to combine into.
		// Nothing was read from the repository to decide this, so reading it
		// again would answer the same way — which is what tells these from the
		// 409s above.
		return http.StatusBadRequest
	case errors.Is(err, git.ErrDiffTooLarge):
		// The request is correct and so is the daemon; what it asked for does
		// not fit in an answer. 413 is the code that says exactly that.
		return http.StatusRequestEntityTooLarge
	default:
		var gitError *git.Error
		if errors.As(err, &gitError) {
			// git refused. The command, its exit code and its stderr are on
			// the error and reach the interface intact; what the daemon adds
			// is that the fault is not the daemon's.
			return http.StatusUnprocessableEntity
		}
		return http.StatusInternalServerError
	}
}
