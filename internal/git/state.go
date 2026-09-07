package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// What a repository is in the middle of.
//
// A merge that stopped on a conflict, a rebase halfway through its list, a
// cherry-pick waiting to be committed: each is a state the user is IN, and
// none of them appears in `git status --porcelain=v2`. The porcelain format
// reports files and a branch header, and says nothing about the operation
// those files belong to — so a panel built on it alone shows seven conflicted
// files and no hint of what produced them.
//
// git records the operation as files in the work tree's git directory, and
// `git status` in a terminal reads exactly these to print "You are currently
// merging". There is no plumbing command that answers the question, so this
// reads what git reads.
//
// It is the one file in this package that opens the git directory rather than
// running the binary, and it belongs here anyway: knowing that
// `rebase-merge/end` holds the number of commits being replayed is knowledge
// about git, and this package is where that knowledge lives.

// Operation is the unfinished thing a repository is in the middle of.
type Operation string

const (
	// OperationNone: nothing is in progress. The zero value, so a State that
	// was never read reads as an ordinary repository rather than as a claim.
	OperationNone Operation = ""

	OperationMerge      Operation = "merge"
	OperationRebase     Operation = "rebase"
	OperationCherryPick Operation = "cherry-pick"
	OperationRevert     Operation = "revert"
	OperationBisect     Operation = "bisect"

	// OperationApply is `git am`, applying a mailbox of patches. It shares
	// rebase's directory, which is why it is told apart by a marker file
	// rather than by one of its own.
	OperationApply Operation = "am"
)

// State is what git would print above the file list in a terminal.
type State struct {
	Operation Operation `json:"operation"`

	// Identity names which instance of the operation is in progress — the
	// object in MERGE_HEAD, the onto of a rebase, and so on. Echoed on the
	// abort confirmation so a finished rebase cannot be aborted as the next
	// one that started under the same kind. Empty when nothing is in progress.
	Identity string `json:"identity,omitempty"`

	// Branch is the branch being replayed, short — "main", not
	// "refs/heads/main". Set for a rebase, which is the operation that has
	// one; a merge happens on whatever is checked out, which the status
	// header already names.
	Branch string `json:"branch,omitempty"`

	// Step and Total are how far a rebase has got. Both zero when the
	// operation does not count, and they are only ever shown together — "3"
	// on its own says nothing.
	Step  int `json:"step,omitempty"`
	Total int `json:"total,omitempty"`

	// Actions is what can be done about this operation, in the order to draw
	// them. Derived rather than read — see operation.go for the table it comes
	// from — and carried here so that the interface never keeps a second copy
	// of it. A button drawn from this list is a command the daemon accepts.
	Actions []Action `json:"actions,omitempty"`

	// Blocked names the actions in Actions this particular repository will not
	// take, against the reason, and it is the reason the user reads: the
	// tooltip on the disabled button and the refusal from the route are the
	// same sentence, so the explanation cannot drift from the enforcement.
	//
	// Nil for almost every state. Actions is a fact about the KIND of
	// operation — `git merge --skip` does not exist — and this is a fact about
	// the one in this directory: a rebase with a `squash` in its plan is still
	// a rebase that takes --continue, and continuing it from here would commit
	// a message nobody was shown. See blockedActions.
	Blocked map[Action]string `json:"blocked,omitempty"`
}

// ErrActionBlocked: git would take the instruction and yagit will not give
// it.
//
// Told apart from ErrActionUnavailable, which is the pair being impossible.
// This one is possible, and refused — the reason travels with it, and it is
// the sentence the button already showed.
var ErrActionBlocked = errors.New("this instruction cannot be run from here")

// Refuse answers why this state will not take an action, or nil.
//
// A method rather than a map lookup at the call site so that the route and the
// interface cannot come to disagree about what "blocked" means, and so that
// the refusal carries an error the HTTP layer already classifies.
func (s State) Refuse(action Action) error {
	if reason, blocked := s.Blocked[action]; blocked {
		return fmt.Errorf("%w: %s", ErrActionBlocked, reason)
	}
	return nil
}

// InProgress says something is unfinished.
func (s State) InProgress() bool { return s.Operation != OperationNone }

// ReadState reads what the work tree is in the middle of.
//
// stateDir is the work tree's OWN git directory — repo.Repo.StateDir — and
// not the common one it shares with its siblings. A merge belongs to the
// checkout that started it.
//
// The order the markers are tested in is git's own, from wt_status_get_state,
// and it is not arbitrary: two of them are routinely present at once, and each
// pair has a right answer that git already made. See readMarkers.
func ReadState(stateDir string) (State, error) {
	state, err := readMarkers(stateDir)
	if err != nil {
		return State{}, err
	}

	// Filled once, here, rather than at each of the returns below: a marker
	// this function learns to read later gets its buttons without anybody
	// remembering to add them.
	state.Actions = Actions(state.Operation)
	state.Blocked = blockedActions(stateDir, state.Operation)
	return state, nil
}

// readMarkers is ReadState without the derived half.
//
// Early returns rather than a switch, and the order is git's own from
// wt_status_get_state. Two of the pairs really do occur, and getting either
// backwards puts the wrong banner and the wrong buttons over a repository:
//
//   - MERGE_HEAD and a leftover sequencer directory. A cherry-pick committed
//     by hand leaves `sequencer/todo` behind, git lets a merge start on top of
//     it, and `git status` then says "use git merge --abort". Reading the
//     sequencer first would draw Cherry-picking over a conflicted merge, and
//     its Abort would run `git cherry-pick --abort`.
//
//   - A rebase directory and CHERRY_PICK_HEAD. Every rebase stop has both,
//     because replaying a commit is how a rebase moves. Reading the marker
//     first would report a cherry-pick nobody started.
//
// The file is opened only where the answer depends on it: this runs on every
// GET /status, which the panel polls every two seconds per open repository.
func readMarkers(stateDir string) (State, error) {
	if stateDir == "" {
		return State{}, errors.New("no state directory given")
	}

	if exists(filepath.Join(stateDir, "MERGE_HEAD")) {
		return State{
			Operation: OperationMerge,
			Identity:  fileIdentity(filepath.Join(stateDir, "MERGE_HEAD")),
		}, nil
	}

	// One directory, two operations. `git am` leaves an `applying` file in it
	// and a rebase leaves `rebasing`; without the distinction, a stalled `git
	// am` would offer to continue a rebase that does not exist.
	if applyDir := filepath.Join(stateDir, "rebase-apply"); exists(applyDir) {
		operation := OperationRebase
		if exists(filepath.Join(applyDir, "applying")) {
			operation = OperationApply
		}
		return withProgress(State{
			Operation: operation,
			Identity:  rebaseIdentity(applyDir),
		}, applyDir, "next", "last"), nil
	}

	if mergeDir := filepath.Join(stateDir, "rebase-merge"); exists(mergeDir) {
		state := State{
			Operation: OperationRebase,
			Identity:  rebaseIdentity(mergeDir),
		}
		name := readStateLine(filepath.Join(mergeDir, "head-name"))
		state.Branch = strings.TrimPrefix(name, headsPrefix)
		return withProgress(state, mergeDir, "msgnum", "end"), nil
	}

	if exists(filepath.Join(stateDir, "CHERRY_PICK_HEAD")) {
		return State{
			Operation: OperationCherryPick,
			Identity:  fileIdentity(filepath.Join(stateDir, "CHERRY_PICK_HEAD")),
		}, nil
	}

	if exists(filepath.Join(stateDir, "REVERT_HEAD")) {
		return State{
			Operation: OperationRevert,
			Identity:  fileIdentity(filepath.Join(stateDir, "REVERT_HEAD")),
		}, nil
	}

	// The sequence outlives the marker for the commit it stopped on.
	//
	// CHERRY_PICK_HEAD names the ONE commit being applied, and git removes it
	// the moment that commit lands — including when it lands from yagit's own
	// commit box, which is the ordinary way out of a conflict here. A
	// cherry-pick or revert of several commits then has no marker at all and
	// several commits still to go, while `git status` in a terminal goes on
	// printing "Cherry-pick currently in progress".
	//
	// Read after the two markers above, because they name the commit and this
	// only names the operation: where both exist they agree, and where they
	// disagree the specific one is the better answer. Without it the banner
	// vanishes at the exact moment it is needed — conflict resolved, two
	// commits unapplied, nothing on screen saying so, and a Continue button
	// that had disappeared along with it.
	if sequenced := readSequencerOperation(stateDir); sequenced != OperationNone {
		return State{
			Operation: sequenced,
			Identity:  fileIdentity(filepath.Join(stateDir, "sequencer", "todo")),
		}, nil
	}

	if exists(filepath.Join(stateDir, "BISECT_LOG")) {
		return State{
			Operation: OperationBisect,
			Identity:  fileIdentity(filepath.Join(stateDir, "BISECT_LOG")),
		}, nil
	}

	return State{}, nil
}

// fileIdentity names one instance of a marker file.
//
// Content alone is not enough: aborting a merge of `side` and starting another
// of the same tip writes the same SHA into MERGE_HEAD. The mtime distinguishes
// the two, so an Abort dialog opened over the first cannot destroy the second.
func fileIdentity(path string) string {
	content := readStateLine(path)
	info, err := os.Lstat(path)
	if err != nil {
		return content
	}
	return content + "@" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

// rebaseIdentity is enough of a stopped rebase to tell two apart.
//
// onto is what the rewrite aims at; orig-head is where it started. Either can
// be absent on exotic backends. fileIdentity's mtime keeps two identical
// rewrites apart the way it does for MERGE_HEAD.
func rebaseIdentity(dir string) string {
	ontoPath := filepath.Join(dir, "onto")
	origPath := filepath.Join(dir, "orig-head")
	onto := readStateLine(ontoPath)
	orig := readStateLine(origPath)
	switch {
	case onto != "" && orig != "":
		return fileIdentity(ontoPath) + ":" + fileIdentity(origPath)
	case onto != "":
		return fileIdentity(ontoPath)
	case orig != "":
		return fileIdentity(origPath)
	default:
		info, err := os.Lstat(dir)
		if err != nil {
			return ""
		}
		return strconv.FormatInt(info.ModTime().UnixNano(), 10)
	}
}

// withProgress fills in how far through its list an operation is.
//
// Both counters or neither: "3" with no total says nothing, and a total with
// no step says less. They come from two files, and a rebase whose backend
// wrote only one of them would otherwise be drawn as "step 3 of 0".
func withProgress(state State, dir, stepName, totalName string) State {
	step := stateCountOrZero(filepath.Join(dir, stepName))
	total := stateCountOrZero(filepath.Join(dir, totalName))
	if step == 0 || total == 0 {
		return state
	}

	state.Step, state.Total = step, total
	return state
}

// exists says a path is there, and treats anything else as absent.
//
// A directory the daemon cannot stat is not a rebase in progress: it is a
// permission problem, and reporting "you are rebasing" because of one would
// put the interface into a state the repository is not in. The honest answer
// to "is a rebase running" when the answer cannot be read is no.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readStateLine reads a one-line marker file, newline trimmed.
//
// Anything it cannot read is the empty string, and the error is dropped
// deliberately rather than by omission. A missing file is the ordinary case:
// git writes head-name only for some rebase backends, and a rebase without a
// branch name is a detached one. A file that is there and unreadable — a
// permission problem, a torn read while git rewrites the directory mid-step —
// is still a rebase in progress, and the caller has no better answer to give
// for it than the one it gives for a detached rebase.
//
// What returning the error cost was the whole route. ReadState is read by
// GET /status, which the panel polls every two seconds, so an unreadable
// head-name emptied the working directory on screen for as long as the
// condition lasted — over a branch name that decorates a banner. The same
// judgement as stateCountOrZero below, and for the same reason: the operation
// is what the banner is FOR.
func readStateLine(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(content), "\n"), "\r")
}

// stateCountOrZero reads a counter file, answering zero for anything it
// cannot read as a number.
//
// A rebase that cannot say where it is up to is still a rebase. The count
// decorates the banner; the operation is what the banner is FOR, and hiding
// it behind an error about a file nobody has heard of would cost the user the
// only sentence that explains their seven conflicted files. The same
// judgement as parseCountOrZero beside the ref parser, for the same reason.
func stateCountOrZero(path string) int {
	count, err := strconv.Atoi(strings.TrimSpace(readStateLine(path)))
	if err != nil {
		return 0
	}
	return count
}

// readSequencerOperation says which of the two replaying commands left a
// sequence unfinished, from the file git reads to answer the same question.
//
// `sequencer/todo` holds one line per commit still to apply, each beginning
// with the verb that applies it: `pick` for a cherry-pick, `revert` for a
// revert. One sequence is one verb, so the first instruction decides.
//
// An unreadable or absent file is no sequence, which is the same judgement the
// rest of this file makes: the honest answer to "is something in progress"
// when it cannot be read is no.
//
// An interactive rebase is not confused with either. Its todo list is
// `rebase-merge/git-rebase-todo`, a different path, and the rebase cases are
// tested before this one regardless.
func readSequencerOperation(stateDir string) Operation {
	for _, instruction := range instructionsIn(filepath.Join(stateDir, "sequencer", "todo")) {
		verb, _, _ := strings.Cut(instruction, " ")
		switch verb {
		// git writes the long form and parses the short one. Both are read
		// here because accepting only what git happens to write today makes
		// this a bet on a format git documents as taking either.
		case "pick", "p":
			return OperationCherryPick
		case "revert":
			return OperationRevert
		}
	}

	return OperationNone
}

// instructionsIn reads a todo-style file and returns the lines that are
// instructions.
//
// git writes blank lines and `#` comments into these files, and neither is
// one: a reader that took line one would find a comment and report nothing in
// progress. Shared by the sequencer's list and the rebase's, which have the
// same shape because they are written by the same code in git.
//
// An unreadable or absent file is no instructions. The error is dropped
// deliberately: every caller here is asking "is something waiting to be done",
// and a file that cannot be read is not an answer of yes.
func instructionsIn(path string) []string {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var instructions []string
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		instructions = append(instructions, line)
	}
	return instructions
}

// blockedActions names what this repository will not be told to do, and why.
//
// One case today, and it is the one the daemon cannot do honestly: a rebase
// whose plan contains a step that opens an editor over a commit message. See
// rebaseStepWritingAMessage, and Command.AcceptsPreparedMessage in command.go
// for the other half of the same rule.
func blockedActions(stateDir string, operation Operation) map[Action]string {
	if operation != OperationRebase {
		return nil
	}

	step, writes := rebaseStepWritingAMessage(filepath.Join(stateDir, "rebase-merge"))
	if !writes {
		return nil
	}

	return map[Action]string{ActionContinue: fmt.Sprintf(
		"This rebase has a %s in it, which writes a commit message. yagit has no editor "+
			"to show you that message in, so finish this one in your terminal.", step)}
}

// rebaseStepWritingAMessage names the first step of a rebase that would open
// an editor over a commit message, if there is one.
//
// Continuing runs the step the rebase is sitting on and then every step after
// it, so both files are read. `done` holds the instructions carried out, the
// last of which is the one it stopped in — a rebase that conflicts inside a
// `squash` writes the squash line there and leaves the todo list empty.
// `git-rebase-todo` holds what is left.
//
// Only the merge backend has these. `rebase-apply` is `git am`'s directory and
// replays patches under their own messages, with nothing to combine.
func rebaseStepWritingAMessage(mergeDir string) (string, bool) {
	done := instructionsIn(filepath.Join(mergeDir, "done"))
	if len(done) > 0 {
		if step, writes := writesAMessage(done[len(done)-1]); writes {
			return step, true
		}
	}

	for _, instruction := range instructionsIn(filepath.Join(mergeDir, "git-rebase-todo")) {
		if step, writes := writesAMessage(instruction); writes {
			return step, true
		}
	}

	return "", false
}

// writesAMessage says whether one rebase instruction opens an editor over a
// commit message that does not exist yet.
//
// `squash` and `reword` always do: one asks for the two messages joined, the
// other exists to change a message and nothing else. `fixup` and `merge` do it
// only with lower-case `-c` — `-C` takes the message as it stands and opens
// nothing. Everything else either commits under a message that already exists
// (pick, edit, fixup, merge) or does not commit at all (exec, drop, label,
// reset, break).
//
// The short spellings are here because git writes the long form and parses
// either, and a todo list edited by hand is the ordinary way an interactive
// rebase gets written.
func writesAMessage(instruction string) (string, bool) {
	verb, rest, _ := strings.Cut(instruction, " ")

	switch verb {
	case "squash", "s":
		return "squash", true
	case "reword", "r":
		return "reword", true
	case "fixup", "f":
		if editsTheMessage(rest) {
			return "fixup -c", true
		}
	case "merge", "m":
		if editsTheMessage(rest) {
			return "merge -c", true
		}
	}

	return "", false
}

// editsTheMessage spots the `-c` that turns fixup and merge into steps that
// ask for a message. Its upper-case twin takes one commit's message whole and
// opens nothing, so the two cannot be folded together.
func editsTheMessage(arguments string) bool {
	field, _, _ := strings.Cut(strings.TrimSpace(arguments), " ")
	return field == "-c"
}
