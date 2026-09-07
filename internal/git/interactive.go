package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Rewriting the commits after a selected one, from a plan somebody wrote.
//
// `git rebase --interactive` is one command with two inputs: its arguments,
// and a todo list it obtains by opening an editor over a file. The second is
// why this file exists and why it is not in rebase.go, which says of itself
// that interactive rebase is not offered there — a daemon has no terminal to
// open an editor in.
//
// yagit opens none. It writes the todo list itself, from a plan assembled on
// screen, and points GIT_SEQUENCE_EDITOR at a program that copies that file
// into place. See sequenceEditor for which program and why it is that one.
//
// The vocabulary is git's own. An Instruction IS the verb written into the
// todo list, so the word on the row, the value on the wire and the word git
// parses are one string with one definition. A plan cannot describe one
// operation and perform another because there is nothing in between to
// disagree.
//
// Every instruction offered here commits under a message that already exists,
// and that is the rule the set was chosen by rather than a coincidence of it.
// `squash` and `reword` are absent for one reason: both ask git to open an
// editor over a message nobody has written yet, which is exactly the state
// blockedActions already refuses to continue — a daemon cannot show it, so a
// daemon must not commit it. Combining two commits is offered instead as the
// question that HAS an answer, which is whose message survives.
//
// What enforces that is not a second check but an editor that refuses. The run
// below points GIT_EDITOR at this program with a flag whose whole behaviour is
// to print why and exit non-zero, so a plan that ever reached git with a step
// needing a message fails at once, with yagit's own sentence in git's stderr.
//
// Leaving GIT_EDITOR unset was the first answer here, on the strength of git's
// "Terminal is dumb, but EDITOR unset". That is what git does on Linux and
// macOS. On Windows it is not: git falls back to an editor from its own
// bundled environment, which with no terminal to draw in WAITS — and a daemon
// whose rebase hangs for ten minutes and is then killed halfway through a
// sequence is very much worse than one that fails. An invariant defended by a
// platform's behaviour is defended on the platforms that behave; this one is
// defended by a program yagit ships.

// maxPlanCommits caps how many commits one plan may cover.
//
// A limit rather than a promise to draw anything: a plan is a list somebody
// reads, reorders and answers for, and a thousand rows is not a list — it is a
// wall that gets approved unread, holding a rewrite of a thousand commits. The
// graph makes the same judgement about width in docs/adr/0016 and says so
// rather than drawing a picture nobody can read.
//
// Two hundred and fifty is far past any branch anybody edits by hand and far
// short of a screen that cannot be reviewed. Past it the plan is refused, with
// the count, so the answer names the repository rather than the interface.
const maxPlanCommits = 250

// Instruction is what one commit's line in a rebase plan does, spelled the way
// git spells it in a todo list.
//
// The value is the verb. That is the point of the type: TodoList writes it
// straight into the file git executes, so there is no table mapping yagit's
// word to git's, and no chance of the two drifting apart.
type Instruction string

const (
	// InstructionPick keeps the commit. git writes it again on top of what
	// came before it, under a new hash — or fast-forwards over it, keeping the
	// hash, where everything below it in the plan is unchanged and in its
	// original order. Which of the two happens is git's own optimisation and
	// costs nothing either way: the commit and its message survive.
	InstructionPick Instruction = "pick"

	// InstructionFixup combines the commit into the one above it in the plan,
	// keeping the message of the one above. This commit's message is
	// discarded, which is what `fixup` MEANS — so git opens no editor, and
	// there is no message here that anybody has to be shown.
	InstructionFixup Instruction = "fixup"

	// InstructionFixupKeep combines the commit into the one above it and
	// replaces that commit's message with THIS commit's.
	//
	// git's `fixup -C`, and the case of the flag is the whole of it: `-C`
	// takes the message as it stands and opens nothing, while `-c` opens an
	// editor over the result. Only the upper-case one can be offered by a
	// daemon, and writesAMessage in state.go refuses the other by name.
	InstructionFixupKeep Instruction = "fixup -C"

	// InstructionEdit applies the commit and stops there, with the commit made
	// and the work tree clean, so it can be amended before the rest of the
	// plan runs.
	//
	// The one instruction that ends in a stop on purpose. What the user gets
	// is the state a conflict already leaves them in — the banner, the changes
	// panel, the commit box in its amend shape — and `git rebase --continue`
	// is the way on, which is the button that state already draws.
	InstructionEdit Instruction = "edit"

	// InstructionDrop leaves the commit out. Its changes do not survive the
	// rebase, and neither does its message.
	//
	// Written as a line rather than by leaving the commit out of the file.
	// git's todo format has the verb for exactly this, `done` then records what
	// was dropped, and the plan on screen and the plan in the file hold the
	// same number of rows — an omission is the one edit a reader cannot see.
	InstructionDrop Instruction = "drop"
)

// ErrUnknownInstruction names the five, because every refusal here is somebody
// being told which words the field takes.
//
// Exported where the reset mode's twin is not, and the difference is where it
// is read. A mode is one field the route parses at the edge and answers 400
// for; an instruction is one of a list, checked inside CheckPlan along with
// four refusals that are about the repository rather than the request. The
// route has to tell them apart to answer, and a sentinel is how.
var ErrUnknownInstruction = errors.New(
	`the instruction must be one of pick, fixup, "fixup -C", edit, drop`)

// ParseInstruction reads an instruction sent by a client.
//
// An unknown one is refused rather than read as a pick, for the reason
// ParseResetMode refuses an unknown mode: the five differ by whether a commit
// survives, whose message survives it, and whether git stops — and running the
// wrong one would look entirely correct to a client that asked for another.
//
// `squash` and `reword` are refused by this too, and they are refused as
// unknown words rather than as forbidden ones. They are not instructions
// yagit has and hides; they are instructions yagit cannot carry out
// honestly, and a refusal naming the five it can is the more useful sentence.
func ParseInstruction(raw string) (Instruction, error) {
	switch Instruction(raw) {
	case InstructionPick, InstructionFixup, InstructionFixupKeep,
		InstructionEdit, InstructionDrop:
		return Instruction(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", ErrUnknownInstruction)
	default:
		return "", fmt.Errorf("%w: %q is not one of them", ErrUnknownInstruction, raw)
	}
}

// Combines says whether an instruction folds its commit into the one above it.
//
// The two fixups differ by whose message survives and by nothing else that
// matters here, so every rule about what may sit at the top of a plan asks
// this rather than naming both.
func (i Instruction) Combines() bool {
	return i == InstructionFixup || i == InstructionFixupKeep
}

// RebaseStep is one line of a plan: a commit, and what to do with it.
type RebaseStep struct {
	// Commit is the full object name. Short forms are resolved before a plan
	// is built — see PlanRange — so that the commit a row named and the commit
	// git receives cannot be two objects.
	Commit string `json:"commit"`

	Instruction Instruction `json:"instruction"`
}

var (
	// ErrEmptyPlan: a plan with no steps at all.
	//
	// Refused rather than run as a no-op. `git rebase --interactive` over an
	// empty todo list is a command that rewinds the branch to the base and
	// says "Successfully rebased", which is a rewrite of everything under a
	// report of nothing.
	ErrEmptyPlan = errors.New("the plan names no commits")

	// ErrPlanNotTheRange: the plan is not the commits after the base, each
	// once.
	//
	// The check that makes every other one safe, and it is deliberately an
	// equality rather than a subset. A plan holding a commit from outside the
	// range would replay foreign history under a dialog that never named it; a
	// plan holding one twice would apply it twice; a plan missing one would
	// drop it silently, which is what the `drop` verb exists to say out loud.
	ErrPlanNotTheRange = errors.New("the plan is not the commits after the base, each exactly once")

	// ErrCombineWithoutTarget: the first commit the plan keeps asks to be
	// combined into the one above it, and there is none.
	//
	// git's own answer to this is "cannot 'fixup' without a previous commit",
	// and it gives that answer AFTER starting the rebase — leaving a
	// repository stopped in the middle of a plan that could never have run.
	// That is the whole reason this is checked here: a refusal before the
	// command costs a sentence, and a refusal after it costs an abort.
	ErrCombineWithoutTarget = errors.New(
		"the first commit a plan keeps cannot be combined into the one above it")

	// ErrRangeHoldsMerge: a merge commit sits between the base and HEAD.
	//
	// A plan is a list of lines, and a merge commit is not a line: replaying
	// one means recreating a second parent, which `--no-rebase-merges` does
	// not do and which no instruction in this file describes. git would
	// flatten it away without a word. Refused instead, naming the commit, for
	// the reason revert refuses a merge rather than inventing a mainline.
	ErrRangeHoldsMerge = errors.New("the commits after this one include a merge")

	// ErrRangeEmpty: nothing sits between the base and HEAD.
	//
	// The ordinary way to reach it is selecting the commit that is already the
	// tip. There is nothing to rewrite and nothing to put in a plan, and
	// answering with an empty list would be a dialog that exists to say no.
	ErrRangeEmpty = errors.New("no commit sits between this one and the tip of the branch")

	// ErrPlanTooLong: the range is longer than maxPlanCommits.
	ErrPlanTooLong = errors.New("too many commits to plan a rebase over")

	// ErrNoSequenceEditor: the program that writes the todo list cannot be
	// found.
	//
	// os.Executable is what fails, and it fails for reasons that have nothing
	// to do with git — a process whose binary was deleted underneath it, a
	// platform that cannot answer. Named rather than passed through, because
	// "readlink /proc/self/exe: no such file or directory" explains nothing
	// about a rebase somebody just asked for.
	ErrNoSequenceEditor = errors.New("cannot find the program that writes a rebase plan, which is yagit's own")
)

// PlanRange is the commits a plan may cover: everything after base, up to
// HEAD, oldest first.
//
// Oldest first because that is the order a todo list is written and executed
// in, and a plan drawn in the other order would be a second convention for the
// same list — the one the user reorders in, disagreeing with the one git runs.
//
// Four refusals, in the order that refuses earliest:
//
//  1. The revision must resolve to a commit. checkRevision has already refused
//     anything git could read as an option.
//  2. It must be an ancestor of HEAD. A commit the branch never held has no
//     "commits after it" on this branch at all, and reading the range anyway
//     would answer with the symmetric difference of two unrelated tips.
//  3. The range must hold at least one commit and no more than maxPlanCommits.
//  4. No commit in it may be a merge — see ErrRangeHoldsMerge.
//
// What this does NOT read is the work tree. A rebase is also refused by local
// changes it would overwrite, and finding out which files those are means
// doing the rebase; git's own refusal names them and travels whole. The same
// boundary PreviewRebase draws, for the same reason.
func (r *Runner) PlanRange(ctx context.Context, dir, base string) (baseSHA, baseSubject string, commits []Commit, err error) {
	baseSHA, _, baseSubject, err = r.resolveCommit(ctx, dir, base)
	if err != nil {
		return "", "", nil, err
	}

	head, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", nil, err
	}
	headSHA := strings.TrimSpace(string(head))

	ancestor, err := r.isAncestor(ctx, dir, baseSHA, headSHA)
	if err != nil {
		return "", "", nil, err
	}
	if !ancestor {
		// ErrResetNotOnBranch rather than an error of this file's own: it is
		// the same fact about the same repository — the commit is not on the
		// branch HEAD is on — and the interface already has a sentence for it.
		return "", "", nil, fmt.Errorf(
			"%w: %s is not reachable from HEAD", ErrResetNotOnBranch, ShortSHA(baseSHA))
	}

	// --reverse rather than reversing the slice afterwards: git walks newest
	// first and this is the order the plan is written in, so the one place
	// that decides it is the command.
	//
	// The trailing -- ends the revisions, for the reason LogScope carries one:
	// a repository holding a file named like the range would otherwise make
	// the walk ambiguous and git would refuse all of it.
	output, err := r.Run(ctx, dir,
		"log", "--reverse", "--topo-order", "--decorate=short",
		"--pretty=format:"+logFormat, baseSHA+".."+headSHA, "--")
	if err != nil {
		return "", "", nil, err
	}

	commits, err = ParseLog(output)
	if err != nil {
		return "", "", nil, err
	}

	if len(commits) == 0 {
		return "", "", nil, fmt.Errorf("%w: %s is the tip", ErrRangeEmpty, ShortSHA(baseSHA))
	}
	if len(commits) > maxPlanCommits {
		return "", "", nil, fmt.Errorf("%w: %d commits sit after %s, and %d is the most one plan may cover",
			ErrPlanTooLong, len(commits), ShortSHA(baseSHA), maxPlanCommits)
	}

	// Read from the parents already parsed rather than from a second walk with
	// --merges. The commits are in hand; asking git again would be a second
	// reading of the same range that can come back different.
	for _, commit := range commits {
		if len(commit.Parents) > 1 {
			return "", "", nil, fmt.Errorf("%w: %s has %d parents",
				ErrRangeHoldsMerge, ShortSHA(commit.SHA), len(commit.Parents))
		}
	}

	return baseSHA, baseSubject, commits, nil
}

// CheckPlan refuses a plan that is not a rearrangement of the range, or that
// git would stop halfway through.
//
// The range is passed in rather than read, and by the caller that just read
// it: this is a comparison of two lists, and a reading of its own would be a
// third list nobody was shown. See the interactive rebase route, which reads
// the range once and hands it to this and to the run.
//
// Order is what a plan is FOR, so the comparison is by set and not by
// sequence: every commit in the range appears in the plan exactly once, and
// nothing else does. What that leaves the client free to do is precisely
// reordering, which is the point.
func CheckPlan(steps []RebaseStep, inRange []Commit) error {
	if len(steps) == 0 {
		return ErrEmptyPlan
	}

	remaining := make(map[string]struct{}, len(inRange))
	for _, commit := range inRange {
		remaining[commit.SHA] = struct{}{}
	}

	for _, step := range steps {
		if _, err := ParseInstruction(string(step.Instruction)); err != nil {
			return err
		}
		if _, expected := remaining[step.Commit]; !expected {
			// One sentence for three shapes of the same mistake — a commit
			// from outside the range, a commit named twice, a stale plan
			// written before somebody committed — because the answer to all
			// three is the same: read the range again.
			return fmt.Errorf("%w: %s is not one of the %d commits after the base, or is named twice",
				ErrPlanNotTheRange, ShortSHA(step.Commit), len(inRange))
		}
		delete(remaining, step.Commit)
	}

	if len(remaining) > 0 {
		return fmt.Errorf("%w: %d of them are missing from it",
			ErrPlanNotTheRange, len(remaining))
	}

	return checkFirstKept(steps)
}

// checkFirstKept refuses a plan whose first surviving commit asks to be
// combined into the one above it.
//
// The first surviving commit, not the first line: a `drop` above a `fixup`
// leaves the fixup with nothing to fold into just as surely as being at the
// top does, and git answers both with the same "cannot 'fixup' without a
// previous commit" — after it has started, having left a repository stopped
// inside a plan that could never have run.
//
// An all-drop plan is not refused here, and that is deliberate. It is a real
// operation with a real meaning — the branch ends up at the base — and git
// carries it out without complaint.
func checkFirstKept(steps []RebaseStep) error {
	for _, step := range steps {
		if step.Instruction == InstructionDrop {
			continue
		}
		if step.Instruction.Combines() {
			return fmt.Errorf("%w: %s is the first commit it keeps, and %q needs one above it",
				ErrCombineWithoutTarget, ShortSHA(step.Commit), step.Instruction)
		}
		return nil
	}

	return nil
}

// TodoList renders a plan as the file git executes.
//
// The subjects come from the range the daemon read, never from the client, and
// that is what makes this safe to write unquoted: git's own `%s` is one line
// by construction, so nothing here can carry a newline and forge a second
// instruction. A client sends object names and verbs, and both are checked
// against the range before this is called.
//
// Trailing newline, because a todo list is a file of lines and git's own
// writer ends it with one.
func TodoList(steps []RebaseStep, inRange []Commit) string {
	subjects := make(map[string]string, len(inRange))
	for _, commit := range inRange {
		subjects[commit.SHA] = commit.Subject
	}

	var todo strings.Builder
	for _, step := range steps {
		fmt.Fprintf(&todo, "%s %s %s\n", step.Instruction, step.Commit, subjects[step.Commit])
	}
	return todo.String()
}

// InteractiveRebaseArgs is the command RebaseInteractive runs.
//
// Exported for the reason RebaseArgs is: the line the user is shown and the
// line git receives have one definition between them.
//
// The four pinned flags are RebaseArgs's four, pinning the same four settings
// for the same reasons — `rebase.autoSquash`, `rebase.autoStash`,
// `rebase.rebaseMerges` and `rebase.updateRefs` — and every one of them earns
// its place again here rather than being copied across. --no-autosquash
// matters MORE with a plan than without one: with the setting on, git rewrites
// the todo list it was given, promoting any commit whose subject begins
// `fixup!` into an instruction the user never wrote. --no-rebase-merges keeps
// git from adding `label` and `reset` lines to a list that has none.
//
// --no-ff is the one flag rebase.go passes that this must not. There it makes
// "every commit is written again" true; here it would make it true of commits
// the user marked `pick` and left in place, rewriting the whole branch for a
// plan that changed its last row. Without it git fast-forwards over the
// untouched bottom of the plan and those commits keep their hashes, which is
// what the interface promises.
//
// The base reaches git after `--`, for the reason every revision in this
// package does: a string from the network must never be read as an option.
// checkRevision refuses a leading dash before it gets here.
func InteractiveRebaseArgs(base string) []string {
	return []string{
		"rebase",
		"--interactive",
		"--no-autosquash",
		"--no-autostash",
		"--no-rebase-merges",
		"--no-update-refs",
		"--", base,
	}
}

// RebaseInteractive rewrites the commits after base, following the plan.
//
// The todo list is written to a file of its own and handed to git through
// GIT_SEQUENCE_EDITOR, which is the only way in: git obtains a todo list by
// running a program over a file it has already written, and there is no flag
// that takes one ready-made.
//
// The plan is NOT re-checked here. CheckPlan is the caller's, one line
// earlier, against the range the caller read — see the route — because
// checking here would need a second reading of the range, and two readings of
// a repository that can change between them is how a check comes to disagree
// with the thing it checked.
//
// A conflict is not hidden: git stops, writes the markers into the work tree
// and exits non-zero, and that refusal travels whole.
//
// A stop at an `edit` is NOT that, and the difference is the one thing a
// caller here must not get wrong: git exits ZERO, because stopping there is
// what it was told to do. Nothing in this function's answer tells a finished
// rebase from one waiting at the second of five commits — a nil error means
// both. What tells them apart is ReadState afterwards, and the route reads it
// for exactly that reason. Reporting "rebased" over a repository sitting in
// the middle of a plan would be the interface's worst possible lie: the state
// is real, the banner would be missing, and the next operation would be
// refused for a reason nothing on screen had mentioned.
//
// Not Run, for the reason Rebase is not: this checks out a tree per commit and
// can run the user's hooks on each one. Thirty seconds is the deadline for
// commands that return instantly, and killing this halfway leaves a
// half-replayed sequence while the user is told their command timed out.
func (r *Runner) RebaseInteractive(ctx context.Context, dir, base string, todo string) error {
	base = strings.TrimSpace(base)
	if err := checkRevision(base); err != nil {
		return err
	}
	if strings.TrimSpace(todo) == "" {
		return ErrEmptyPlan
	}

	editors, cleanup, err := editorsForPlan(todo)
	if err != nil {
		return err
	}
	defer cleanup()

	_, err = r.Exec(ctx, Command{
		Dir:            dir,
		Args:           InteractiveRebaseArgs(base),
		Timeout:        rewriteTimeout,
		SequenceEditor: editors.sequence,
		MessageEditor:  editors.message,
	})
	return err
}

// rebaseTodoFlag is the argument that makes this program write a todo list
// instead of starting a daemon.
//
// A flag rather than an environment variable, and a whole word rather than a
// letter, because the only thing that ever passes it is sequenceEditor below
// and the only thing that ever reads it is the first line of main. Neither
// needs it to be short, and a stray `-w` in somebody's shell history should
// not be a program that overwrites a file.
const rebaseTodoFlag = "--write-rebase-todo"

// refuseMessageFlag is the argument that makes this program refuse to be a
// commit-message editor, loudly, and stop.
//
// The other half of the same idea as rebaseTodoFlag: git will run whatever
// GIT_EDITOR names, so the surest way to be certain no message is ever
// committed unread is to name a program that cannot commit one. It costs a
// process that exits immediately, on a path nothing should ever reach.
const refuseMessageFlag = "--refuse-message-editor"

// rebaseEditors are the two programs git may run during an interactive rebase:
// one over the todo list, one over a commit message.
//
// Together because they are one decision — what this rebase will let git open
// — and because both are this executable, found once. Splitting them left the
// second easy to forget, which is exactly how GIT_EDITOR came to be unset.
type rebaseEditors struct {
	sequence string
	message  string
}

// sequenceEditor writes the todo list to a temporary file and returns the
// GIT_SEQUENCE_EDITOR that will copy it into place, plus the cleanup for it.
//
// The program is this process's own executable, and the reasons are all
// availability: it is the one program certain to exist wherever the daemon
// runs, on every platform yagit ships for, needing no PATH lookup, no shell
// builtin and nothing installed alongside. `cp` would be none of those on
// Windows, and a script written next to the todo would be a file to make
// executable on two platforms and impossible on the third.
//
// git runs the value as a SHELL COMMAND — that is what an editor setting is,
// documented and long-standing — and appends the file to edit. So the parts
// are quoted here rather than hoped to be free of spaces: git builds the
// script as `<value> "$@"` and does no quoting of its own, so an unquoted
// installation path with a space in it would have run its first word as a
// program. See shellQuote.
//
// The file is created with os.CreateTemp, which opens it 0600 and gives it a
// name nothing else has. It matters that the name is unpredictable: this is a
// file whose contents become a list of commands git carries out, and a
// predictable path in a shared temporary directory is one another user on the
// machine could have written first.
func editorsForPlan(todo string) (editors rebaseEditors, cleanup func(), err error) {
	program, err := os.Executable()
	if err != nil {
		return rebaseEditors{}, nil, fmt.Errorf("%w: %w", ErrNoSequenceEditor, err)
	}

	file, err := os.CreateTemp("", "yagit-rebase-todo-*")
	if err != nil {
		return rebaseEditors{}, nil, fmt.Errorf("cannot write the rebase plan: %w", err)
	}
	name := file.Name()

	// Removed whatever happens next, including the write below failing: a plan
	// left in the temporary directory is a list of somebody's commits, and this
	// is the only code that knows the name.
	//
	// The removal's own failure is silenced, and this is the deliberate act the
	// linter's check-blank is there to make deliberate. There is nothing to do
	// about it and nothing to say: by the time this runs the rebase has already
	// succeeded or failed on its own terms, that answer is what the user asked
	// for, and turning "a file in /tmp outlived its use" into the reply to
	// "rewrite my commits" would be reporting the wrong thing entirely.
	//nolint:errcheck // see above: the rebase's own answer is the one that matters
	cleanup = func() { os.Remove(name) }

	// Both, joined, rather than the write alone. A close that fails is how a
	// full disk reports a write that appeared to succeed, and the file this is
	// about becomes a list of commands git carries out — half of one is worse
	// than none.
	_, writeErr := file.WriteString(todo)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		cleanup()
		return rebaseEditors{}, nil, fmt.Errorf("cannot write the rebase plan: %w", err)
	}

	return rebaseEditors{
		sequence: shellQuote(program) + " " + rebaseTodoFlag + " " + shellQuote(name),
		message:  shellQuote(program) + " " + refuseMessageFlag,
	}, cleanup, nil
}

// RunAsEditor answers a command line asking this program to be one of the two
// editors an interactive rebase points git at, and reports whether it was one.
//
// One entry point for both, because there are three callers — the first line
// of cmd/yagit, and the TestMain of each package whose tests drive a rebase —
// and a second branch that one of them forgot is exactly how GIT_EDITOR came
// to be unset on a platform where that means "wait forever". Adding an editor
// now changes this function and nothing else.
//
// Recognised by the exact flag and the exact argument count, so a daemon
// starting normally falls through untouched. git appends the file to edit, so
// each shape is one argument longer than what editorsForPlan wrote.
//
// `handled` says this process was an editor and must now exit; the error says
// which way. The two are separate because being an editor and succeeding at it
// are different answers, and the caller's exit code depends on the second.
func RunAsEditor(args []string) (handled bool, err error) {
	switch {
	case len(args) == 4 && args[1] == rebaseTodoFlag:
		return true, WriteRebaseTodo(args[2], args[3])

	case len(args) == 3 && args[1] == refuseMessageFlag:
		// Never reached by a plan yagit wrote: CheckPlan refuses every
		// instruction that opens an editor over a message, and the five that
		// are offered commit under messages that already exist. Reaching this
		// means one of those two statements has stopped being true, so it says
		// so rather than failing as an editor that could not start.
		return true, errors.New(
			"a daemon has no terminal to show a commit message in, so yagit will not " +
				"commit one unread — and this rebase asked for one, which no plan " +
				"yagit writes ever does")

	default:
		return false, nil
	}
}

// WriteRebaseTodo copies a prepared todo list over the one git generated.
//
// The whole of what this program does as a sequence editor, kept beside the
// code that asks git to run it rather than in the daemon's main, so that the
// thing under test and the thing that ships are one function. RunAsEditor
// above is its only caller.
//
// The destination is truncated rather than appended to, and it is written
// whole: git reads the file back the moment this process exits, so a partial
// write is a partial list of commands it would carry out.
func WriteRebaseTodo(source, destination string) error {
	todo, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("cannot read the rebase plan at %s: %w", source, err)
	}
	if err := os.WriteFile(destination, todo, 0o600); err != nil {
		return fmt.Errorf("cannot write the rebase plan to %s: %w", destination, err)
	}
	return nil
}
