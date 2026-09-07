package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// This file holds the commands that change the work tree and the index;
// branch.go holds the ones that move HEAD, and worktree.go manages the LINKED
// working trees `git worktree` makes. Everything else in the package reads.
//
// Named staging.go rather than worktree.go, which it used to be: git uses
// "worktree" for a checkout of its own, and two files a letter apart meaning
// two unrelated things is how a path meant for `git add` ends up in a
// `git worktree remove`.
//
// Three rules apply to all of them, and they are what the interface's promises
// rest on:
//
//  1. Paths are passed after `--`. Without it a file named `HEAD`, `-f` or
//     `--all` is read as a revision or an option, and the command does
//     something other than what its own log line says.
//
//  2. Paths are passed as literal pathspecs. `--` is not enough on its own —
//     see literalPathspecs below.
//
//  3. Each one is a command a user could have typed. That is not style: the
//     log panel shows what ran, and a command nobody can paste into their own
//     terminal teaches nothing.

// ErrNoPaths guards every path-taking operation. `git add --` with no path is
// a no-op and `git clean --force --` with none is not, so an empty list is
// refused here rather than handed to git to interpret.
var ErrNoPaths = errors.New("no path given")

// literalPathspecs turns file names into pathspecs that match those names and
// nothing else.
//
// This is the third rule, and it is the one with teeth. `--` ends option
// parsing; it does not make what follows a filename. Everything after it is a
// pathspec, and git matches pathspecs with wildmatch — so a file legitimately
// named `*`, or `app/[id].tsx`, or `report(final).md`, is a PATTERN by the
// time git reads it. `git clean --force -- '*'` deletes every untracked file
// in the work tree while the interface named one, and nothing it removed was
// ever committed. `:(literal)` is git's own way to say "this is a name",
// understood since 1.9, and it shows up in the log panel where a reader can
// see why it is there.
func literalPathspecs(paths []string) []string {
	specs := make([]string, 0, len(paths))
	for _, path := range paths {
		specs = append(specs, ":(literal)"+path)
	}
	return specs
}

// maxPathspecArgv is how many characters of pathspec may go on the command
// line before the paths are handed to git on standard input instead.
//
// Windows is the reason there is a limit at all: CreateProcess refuses a
// command line past 32767 characters, and `:(literal)` adds ten characters to
// every path, so "Stage all" reaches it at roughly eight hundred files of
// ordinary length — while the request body carrying those paths is still a
// quarter of its own 64 KiB cap. Linux allows about two megabytes and macOS a
// quarter of one, so this fails on exactly one of the three platforms yagit
// ships to, which is the worst place for a limit to live.
//
// One number for all three, deliberately. A platform-dependent threshold would
// mean the branch below never runs on the machine anybody develops or tests on,
// and a path nothing exercises is a path that is wrong.
const maxPathspecArgv = 30000

// pathspecs decides how a list of paths reaches git: on the command line, or
// on standard input.
//
// The command line while it fits, and that is not laziness. The log panel shows
// what ran and the project's claim is that a person can learn git by reading it
// — `git add -- :(literal)notes.md` teaches that, and
// `git add --pathspec-from-file=-` teaches nothing about which file. So the
// readable form is kept for every case a human is reading, and the other form
// appears only past the point where the command line was going to be
// unreadable anyway.
//
// --pathspec-file-nul because a file name may contain a newline. git accepts
// it, the ordinary line-separated form does not, and a separator a name can
// contain is a separator that eventually splits one path into two that do not
// exist — the same reasoning `-z` gets everywhere else in this package.
//
// The `:(literal)` prefix survives both forms: what the file holds is
// pathspecs, not names, so the rule literalPathspecs exists for still applies.
func pathspecs(paths []string) (args []string, stdin []byte) {
	specs := literalPathspecs(paths)

	length := 0
	for _, spec := range specs {
		length += len(spec) + 1
	}
	if length <= maxPathspecArgv {
		return append([]string{"--"}, specs...), nil
	}

	return []string{"--pathspec-from-file=-", "--pathspec-file-nul"},
		[]byte(strings.Join(specs, "\x00") + "\x00")
}

// Stage adds paths to the index.
//
// `git add` covers every case a single path can be in — new, modified,
// deleted, untracked — which is why staging a whole file is this and not a
// patch. A patch cannot carry a mode change or a binary file; `git add` can.
func (r *Runner) Stage(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}
	// rewriteTimeout, not the default thirty seconds: `git add` runs the clean
	// filter over every path it takes, and in a repository routing assets
	// through LFS that filter uploads them.
	tail, stdin := pathspecs(paths)
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    append([]string{"add"}, tail...),
		Stdin:   stdin,
		Timeout: rewriteTimeout,
	})
	return err
}

// Unstage takes paths back out of the index.
//
// unborn selects the command, and it has to be told rather than discovered:
// `git restore --staged` restores the index from HEAD, and a branch whose
// first commit does not exist yet has no HEAD to restore from — git exits 128
// with "could not resolve HEAD". Removing the path from the index is the same
// intent expressed in the way that works there, and it leaves the file
// untracked, which is exactly where it came from.
func (r *Runner) Unstage(ctx context.Context, dir string, paths []string, unborn bool) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}

	if unborn {
		// --cached leaves the file on disk. Without it this deletes the
		// user's work, which is not what unstaging means anywhere.
		tail, stdin := pathspecs(paths)
		_, err := r.Exec(ctx, Command{
			Dir:     dir,
			Args:    append([]string{"rm", "--cached", "--quiet"}, tail...),
			Stdin:   stdin,
			Timeout: rewriteTimeout,
		})
		return err
	}

	tail, stdin := pathspecs(paths)
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    append([]string{"restore", "--staged"}, tail...),
		Stdin:   stdin,
		Timeout: rewriteTimeout,
	})
	return err
}

// DiscardTrackedArgs is the command DiscardTracked runs.
//
// The three Args functions here are exported for one caller: the route that
// answers what a discard WOULD run, so the confirmation can show it. Nothing
// else in the project needs to know an argument list before it runs, and the
// three that do are the three the user is asked to approve.
//
// They exist because the alternative was composing the same line a second time
// somewhere else, and that second copy is what drifted: it showed `git restore
// --worktree -- report(final).md` for a command git received as a pathspec
// with `:(literal)` in front of it. One definition, rendered by CommandLine,
// is the only version of this that cannot be wrong about itself.
func DiscardTrackedArgs(paths []string) []string {
	tail, _ := pathspecs(paths)
	return append([]string{"restore", "--worktree"}, tail...)
}

// DiscardTracked throws away the work-tree changes to tracked paths.
//
// Destructive, and unrecoverable: what it removes was never committed and is
// in no reflog. The interface names the files before running it.
func (r *Runner) DiscardTracked(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}
	// rewriteTimeout: `git restore` rewrites the named files out of the index,
	// running the smudge filter over each one, and it is destructive — a
	// deadline that killed it halfway would leave some files restored and
	// others not, with nothing to say which.
	_, stdin := pathspecs(paths)
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    DiscardTrackedArgs(paths),
		Stdin:   stdin,
		Timeout: rewriteTimeout,
	})
	return err
}

// DiscardUntracked deletes files git does not track.
//
// `git clean --force` rather than removing them directly. Deleting a file is
// not something the daemon should do behind git's back — the log panel would
// show nothing at all for the one operation in this package that destroys a
// file outright.
//
// No -x: ignored files stay. They are not in the status listing the interface
// offers, so cleaning them would remove something nobody chose.
func (r *Runner) DiscardUntracked(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}
	for _, args := range DiscardUntrackedBatches(paths) {
		if _, err := r.Exec(ctx, Command{Dir: dir, Args: args, Timeout: rewriteTimeout}); err != nil {
			return err
		}
	}
	return nil
}

// DiscardUntrackedBatches is the command, or commands, DiscardUntracked runs.
// See DiscardTrackedArgs for why it is exported.
//
// Plural, and `git clean` is the reason. Every other path-taking command here
// can be handed its pathspecs on standard input past the length a command line
// holds; clean is the one git never gave that option, so the only way to stay
// under the Windows limit is to run it more than once.
//
// What that costs is atomicity, and it is worth saying plainly: a failure
// partway through leaves the earlier batches deleted. It is the same shape as
// a clean interrupted, the confirmation names every file before any of it
// runs, and the alternative — refusing to discard eight hundred untracked
// files at all — is worse than doing it in three commands the log panel shows
// one after another.
func DiscardUntrackedBatches(paths []string) [][]string {
	specs := literalPathspecs(paths)

	batches := make([][]string, 0, 1)
	batch := []string{"clean", "--force", "--"}
	length := 0

	for _, spec := range specs {
		if length > 0 && length+len(spec)+1 > maxPathspecArgv {
			batches = append(batches, batch)
			batch = []string{"clean", "--force", "--"}
			length = 0
		}
		batch = append(batch, spec)
		length += len(spec) + 1
	}
	if length > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// ApplyTarget says what a patch is applied to.
type ApplyTarget string

const (
	// ApplyToIndex changes the index and leaves the work tree alone: staging,
	// or unstaging when reversed.
	ApplyToIndex ApplyTarget = "index"

	// ApplyToWorkTree changes the files on disk. Only ever used reversed, to
	// discard part of a change.
	ApplyToWorkTree ApplyTarget = "work-tree"
)

// StageLines puts part of a file's unstaged change into the index.
//
// UnstageLines and DiscardLines below are the same shape, and the three exist
// as named operations for one reason: each pairs a patch direction with a
// `git apply` direction, and those two have to agree. Pairing them once, here,
// removes the only way to get that wrong.
func (r *Runner) StageLines(ctx context.Context, dir string, file FileDiff, selected map[int]bool) error {
	patch, err := FormatPatch(file, selected, PatchForward)
	if err != nil {
		return err
	}
	return r.ApplyPatch(ctx, dir, patch, ApplyToIndex, false)
}

// UnstageLines takes part of a file's staged change back out of the index.
func (r *Runner) UnstageLines(ctx context.Context, dir string, file FileDiff, selected map[int]bool) error {
	patch, err := FormatPatch(file, selected, PatchReverse)
	if err != nil {
		return err
	}
	return r.ApplyPatch(ctx, dir, patch, ApplyToIndex, true)
}

// DiscardLines throws away part of a file's unstaged change.
//
// Destructive, and unrecoverable: what it removes was never committed and is
// in no reflog.
func (r *Runner) DiscardLines(ctx context.Context, dir string, file FileDiff, selected map[int]bool) error {
	patch, err := FormatPatch(file, selected, PatchReverse)
	if err != nil {
		return err
	}
	return r.ApplyPatch(ctx, dir, patch, ApplyToWorkTree, true)
}

// ApplyPatch feeds a patch to `git apply` on standard input.
//
// The three operations above are how the rest of the project reaches it. It
// stays exported because a patch and its application are worth testing
// together against real git, and because a caller with a patch from somewhere
// else has nowhere else to go.
//
// A patch that does not apply is a refusal, not a warning: git leaves the
// index and the work tree untouched, and the error carries git's own
// explanation of which hunk failed.
func (r *Runner) ApplyPatch(ctx context.Context, dir string, patch []byte, target ApplyTarget, reverse bool) error {
	_, err := r.Exec(ctx, Command{Dir: dir, Args: ApplyPatchArgs(target, reverse), Stdin: patch})
	return err
}

// ApplyPatchArgs is the command ApplyPatch runs. See DiscardTrackedArgs for
// why it is exported.
//
// It takes no paths: a patch names the files inside itself, which is why
// discarding lines is one command whatever was selected.
func ApplyPatchArgs(target ApplyTarget, reverse bool) []string {
	args := []string{"apply"}
	if target == ApplyToIndex {
		args = append(args, "--cached")
	}
	if reverse {
		args = append(args, "--reverse")
	}

	// `-` is where git reads the patch from. It is passed explicitly, though
	// git would read standard input anyway with no file named, because the
	// log panel shows this command and a reader should be able to see where
	// the patch came from.
	return append(args, "-")
}

// CommitOptions is what a commit can be asked to do beyond recording the
// index.
type CommitOptions struct {
	// Message is the whole commit message, subject and body. It reaches git
	// on standard input rather than through -m: a message holding anything
	// git's option parser would take an interest in — a leading dash, a NUL —
	// has no way to go wrong on the way there.
	Message string

	// Amend replaces the previous commit instead of adding one. It rewrites
	// history, so the interface confirms it and says what is being replaced.
	Amend bool
}

// ErrEmptyMessage: git refuses an empty message, and so does this, one step
// earlier and with a sentence about what to do.
var ErrEmptyMessage = errors.New("a commit message is required")

// Commit records the index and returns the SHA of what it wrote.
//
// The user's hooks run. They are the user's, and a client that silently
// passed --no-verify would be making a decision about someone else's
// repository; a hook that blocks on input hits the command timeout and its
// output reaches the interface whole.
//
// Signing follows the user's configuration too, since git reads their
// ~/.gitconfig. What it needs from the environment is passed by name in
// commandEnvironment — and a signature that needs a passphrase typed at a
// terminal cannot be produced by a daemon that has none, which git reports
// and the interface shows rather than committing unsigned.
func (r *Runner) Commit(ctx context.Context, dir string, options CommitOptions) (string, error) {
	if strings.TrimSpace(options.Message) == "" {
		return "", ErrEmptyMessage
	}

	// --file=- reads the message from standard input. --cleanup=whitespace is
	// git's own default for a message given this way, and it is named here so
	// that changing it is a decision rather than an accident: it trims
	// trailing space and blank edges, and — unlike the interactive default —
	// keeps lines starting with '#', which in a text box are text.
	args := []string{"commit", "--file=-", "--cleanup=whitespace"}
	if options.Amend {
		args = append(args, "--amend")
	}

	// rewriteTimeout, because a commit is not a command that returns
	// instantly: it runs the user's pre-commit and commit-msg hooks, and those
	// are where a repository puts its formatter, its linter and its test
	// selection. Forty-five seconds is an ordinary pre-commit hook and is over
	// the thirty-second deadline, which would kill git in the middle of writing
	// the commit and tell the user their commit timed out.
	if _, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    args,
		Stdin:   []byte(options.Message),
		Timeout: rewriteTimeout,
	}); err != nil {
		return "", err
	}

	return r.HeadSHA(ctx, dir)
}

// HeadSHA resolves HEAD to a full commit name.
func (r *Runner) HeadSHA(ctx context.Context, dir string) (string, error) {
	output, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}

	sha := strings.TrimSpace(string(output))
	if sha == "" {
		return "", fmt.Errorf("git rev-parse HEAD in %s answered nothing", dir)
	}
	return sha, nil
}

// ConflictSide names which version of an unmerged path to keep.
//
// git's own words, and its own switches: `--ours` is what the branch you are
// on had, `--theirs` is what the branch you are merging in had. During a
// REBASE they are the other way round from what most people expect — "ours" is
// the branch being replayed onto — and that is git's meaning, not a mistake to
// correct here. The notes beside the buttons follow the operation.
type ConflictSide string

const (
	SideOurs   ConflictSide = "ours"
	SideTheirs ConflictSide = "theirs"
)

// HasSide says the named side of an unmerged path has content at all.
//
// Not every conflict is two versions of a file. "Deleted by us" is one side
// with content and one side without, and `git checkout --ours` on it fails
// with "path 'f.txt' does not have our version" — so keeping OUR side there
// means removing the file, not checking anything out. Which of the two
// commands a path needs is what this answers.
//
// Read off Conflict rather than off the raw codes, and that is the whole of
// why it is correct. The pair for "added by them" is `UA`: neither letter is
// a `D`, and a rule written as "not deleted" therefore reports our side as
// having content it does not have — `git checkout --ours` then fails on
// exactly the conflict this function exists for. Conflict already maps all
// seven pairs; a second, partial reading of them beside it is how the two
// disagree.
func (f FileStatus) HasSide(side ConflictSide) bool {
	switch f.Conflict() {
	case "":
		// Not unmerged: there are no sides to have.
		return false
	case ConflictBothDeleted:
		return false
	case ConflictAddedByUs, ConflictDeletedByThem:
		// Our side has the file; theirs is where it is missing.
		return side == SideOurs
	case ConflictAddedByThem, ConflictDeletedByUs:
		return side == SideTheirs
	default:
		// Both modified, both added, and any pair a later git invents. Both
		// sides have content, so it is a checkout — and for the pair nobody
		// has seen yet that is the answer that fails loudly rather than
		// running `git rm` on a file this code did not understand.
		return true
	}
}

// KeepSideArgs is the pair of commands that resolves a conflict in favour of
// one side, in the order they run. See DiscardTrackedArgs for why the argument
// lists of shown commands are built here.
//
// Two commands, and both are needed. `git checkout --ours` writes the file;
// only `git add` marks it resolved, by collapsing the three index stages into
// one. Stopping after the first leaves a repository that still refuses to
// commit, with a work tree that looks finished — the worst of both.
func KeepSideArgs(paths []string, side ConflictSide) [][]string {
	specs := literalPathspecs(paths)
	return [][]string{
		append([]string{"checkout", "--" + string(side), "--"}, specs...),
		append([]string{"add", "--"}, specs...),
	}
}

// RemoveConflictedArgs is the command that resolves a conflict by removing the
// file, for the side that has no content to keep.
func RemoveConflictedArgs(paths []string) []string {
	return append([]string{"rm", "--"}, literalPathspecs(paths)...)
}

// KeepSide resolves conflicts by taking one side whole.
//
// What it overwrites is the merged file in the work tree, conflict markers and
// any hand edits included. Those edits are in no index stage and no reflog, so
// the interface asks before calling this on a file somebody has been editing —
// and does not ask otherwise, because a dialog in front of the ordinary path
// out of a conflict is a dialog nobody reads.
func (r *Runner) KeepSide(ctx context.Context, dir string, paths []string, side ConflictSide) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}
	if side != SideOurs && side != SideTheirs {
		return fmt.Errorf("%q is neither ours nor theirs", side)
	}

	for _, args := range KeepSideArgs(paths, side) {
		if _, err := r.Run(ctx, dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// RemoveConflicted resolves a conflict by recording the deletion.
//
// The resolution for the side of a conflict that has no file: "deleted by us",
// kept ours. `git rm` both removes it from the work tree and collapses the
// index stages, which is the whole resolution in one command.
func (r *Runner) RemoveConflicted(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}
	_, err := r.Run(ctx, dir, RemoveConflictedArgs(paths)...)
	return err
}
