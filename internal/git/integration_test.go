package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// These tests run the real git binary against a repository built on the fly.
//
// They check what no parsing test can: that the format strings sent to git
// actually produce the output the parser expects. A perfect parser on top of
// a wrong format string is still wrong.

// identity is passed to every command that writes an object, so the objects
// these tests produce never depend on who runs them.
var identity = []string{"-c", "user.name=yagit Test", "-c", "user.email=test@yagit.local"}

// isolateGitConfiguration points git at empty configuration files.
//
// An explicit identity is not enough on its own. The Runner passes HOME
// through, deliberately, so git reads the user's ~/.gitconfig — and on a
// machine that sets commit.gpgsign, the commits made here come out signed.
// The suite then passes or fails on whether that machine happens to have an
// unlocked signing key, which is precisely the machine dependence these tests
// exist to avoid.
//
// GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are git's documented way to say
// "read no configuration at this level"; the Runner forwards them for exactly
// this purpose.
func isolateGitConfiguration(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func runGit(t *testing.T, runner *git.Runner, dir string, args ...string) {
	t.Helper()
	if _, err := runner.Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
}

func commitEmpty(t *testing.T, runner *git.Runner, dir, message string) {
	t.Helper()
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"commit", "--allow-empty", "-m", message)...)
}

// testRepository builds a repository with a real merge and an annotated tag:
// the smallest history that exercises every case the parsing has to handle.
//
//	main:   A ──────── C ─── M
//	                 ╲     ╱
//	feature:           B ──
func testRepository(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "A: first commit")

	runGit(t, runner, dir, "checkout", "-b", "feature")
	commitEmpty(t, runner, dir, "B: work on the branch")

	runGit(t, runner, dir, "checkout", "main")
	commitEmpty(t, runner, dir, "C: work on main")

	runGit(t, runner, dir, append(append([]string{}, identity...),
		"merge", "--no-ff", "feature", "-m", "M: merge feature")...)

	runGit(t, runner, dir, append(append([]string{}, identity...),
		"tag", "-a", "v1.0", "-m", "version 1")...)

	return dir, runner
}

func TestLogAgainstARealRepository(t *testing.T) {
	dir, runner := testRepository(t)

	commits, err := runner.Log(context.Background(), dir)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 4 {
		t.Fatalf("expected 4 commits (A, B, C, M), got %d", len(commits))
	}

	// --topo-order puts the merge first: it is the tip of main.
	merge := commits[0]
	if len(merge.Parents) != 2 {
		t.Fatalf("the merge must have 2 parents, got %d: %v", len(merge.Parents), merge.Parents)
	}
	if !strings.HasPrefix(merge.Subject, "M:") {
		t.Errorf("merge subject = %q", merge.Subject)
	}
	if merge.Author != "yagit Test" {
		t.Errorf("author = %q, expected yagit Test", merge.Author)
	}
	if merge.Date.IsZero() {
		t.Error("the author date was not read")
	}

	// The decoration must carry HEAD and the tag: that is what paints the
	// badges on the graph row.
	decoration := strings.Join(merge.Refs, " | ")
	if !strings.Contains(decoration, "HEAD -> main") {
		t.Errorf("decoration = %q, expected HEAD -> main", decoration)
	}
	if !strings.Contains(decoration, "tag: v1.0") {
		t.Errorf("decoration = %q, expected tag: v1.0", decoration)
	}

	// The root commit, at the tail, has no parent.
	root := commits[len(commits)-1]
	if len(root.Parents) != 0 {
		t.Errorf("the root commit must have no parent, got %v", root.Parents)
	}
	if !strings.HasPrefix(root.Subject, "A:") {
		t.Errorf("root commit subject = %q", root.Subject)
	}
}

func TestForEachRefAgainstARealRepository(t *testing.T) {
	dir, runner := testRepository(t)

	refs, err := runner.ForEachRef(context.Background(), dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}

	byName := make(map[string]git.Ref, len(refs))
	for _, ref := range refs {
		byName[ref.Name] = ref
	}

	for _, name := range []string{"refs/heads/main", "refs/heads/feature", "refs/tags/v1.0"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("ref %s missing; got %v", name, byName)
		}
	}

	if byName["refs/heads/main"].Kind != git.RefBranch {
		t.Errorf("main should be a branch, got %q", byName["refs/heads/main"].Kind)
	}

	// The tag is annotated: without the dereferenced field its SHA would be
	// the tag object's, absent from the history, and the badge would vanish.
	commits, err := runner.Log(context.Background(), dir)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if tag := byName["refs/tags/v1.0"]; tag.SHA != commits[0].SHA {
		t.Errorf("the annotated tag points at %s, expected the merge commit %s", tag.SHA, commits[0].SHA)
	}
}

// Lexicographic refname order puts v0.10.0 before v0.9.0. The sidebar reads
// tags in the order ForEachRef returns them, so that order has to be version
// order — newest first.
//
// v1.09 and v1.010 are here because they are the pair a rewrite gets wrong:
// git reads a digit run with a leading zero as the digits after a decimal
// point, so v1.010 sorts before v1.09 and both sort before v1.0. Reading the
// runs as plain integers reverses all three.
func TestForEachRefSortsTagsByVersionNewestFirst(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	for _, name := range []string{"v0.9.0", "v0.10.0", "v0.25.0", "v1.09", "v1.010"} {
		runGit(t, runner, dir, "tag", name)
	}

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}

	tags := shortNamesOfKind(refs, git.RefTag)
	want := []string{"v1.0", "v1.09", "v1.010", "v0.25.0", "v0.10.0", "v0.9.0"}
	if !slices.Equal(tags, want) {
		t.Fatalf("tags = %v, want %v (version, newest first)", tags, want)
	}

	// Everything that is not a tag keeps the order git printed it in, which
	// --sort=version:refname makes ascending. Reversing the whole namespace
	// with `--sort=-version:refname` would have reversed these too; that is
	// why only the tags are turned around, and in process.
	branches := shortNamesOfKind(refs, git.RefBranch)
	wantBranches := []string{"feature", "main"}
	if !slices.Equal(branches, wantBranches) {
		t.Fatalf("branches = %v, want %v (ascending)", branches, wantBranches)
	}
}

func shortNamesOfKind(refs []git.Ref, kind git.RefKind) []string {
	var names []string
	for _, ref := range refs {
		if ref.Kind == kind {
			names = append(names, ref.ShortName)
		}
	}
	return names
}

// TestReadHEADAgainstARealRepository covers the two states HEAD can be in with
// a commit under it. The sidebar marks the branch you are on, and a marker is
// worth nothing unless it is right in both.
func TestReadHEADAgainstARealRepository(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	onBranch, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if onBranch.Name != "main" {
		t.Errorf("name = %q, expected main", onBranch.Name)
	}
	if onBranch.Detached {
		t.Error("HEAD sits on a branch and was reported detached")
	}
	if len(onBranch.SHA) != 40 {
		t.Errorf("sha = %q, expected a full object name", onBranch.SHA)
	}

	runGit(t, runner, dir, "checkout", "--detach", "HEAD")

	detached, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("ReadHEAD after detaching: %v", err)
	}
	if !detached.Detached || detached.Name != "HEAD" {
		t.Errorf("detached HEAD = %+v, expected the name HEAD and Detached", detached)
	}
	if detached.SHA != onBranch.SHA {
		t.Errorf("detaching moved the commit: %q, was %q", detached.SHA, onBranch.SHA)
	}
}

// TestReadHEADOfARepositoryWithNoCommit pins the state right after git init.
//
// `rev-parse HEAD` fails there with exit code 128, and reporting that as an
// error would put a failure on screen for a repository that is merely new.
func TestReadHEADOfARepositoryWithNoCommit(t *testing.T) {
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)
	runGit(t, runner, dir, "init", "-b", "main")

	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("a repository with no commit is a normal state, not an error: %v", err)
	}
	if head != (git.HEAD{}) {
		t.Errorf("head = %+v, expected the zero value", head)
	}
}

// TestGitErrorCarriesFullContext checks the project's central promise: never
// "Something went wrong". A failing command has to surface the exact command,
// its exit code and the raw stderr.
func TestGitErrorCarriesFullContext(t *testing.T) {
	dir, runner := testRepository(t)

	_, err := runner.Run(context.Background(), dir, "checkout", "no-such-branch")
	if err == nil {
		t.Fatal("checking out a branch that does not exist must fail")
	}

	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error of type %T, expected *git.Error", err)
	}
	if gitError.ExitCode <= 0 {
		t.Errorf("exit code = %d, expected strictly positive", gitError.ExitCode)
	}
	if !strings.Contains(gitError.Stderr, "no-such-branch") {
		t.Errorf("the raw stderr must be kept, got: %q", gitError.Stderr)
	}
	if gitError.CommandLine() != "git checkout no-such-branch" {
		t.Errorf("rendered command = %q", gitError.CommandLine())
	}
}

func TestObserverReceivesEveryExecution(t *testing.T) {
	isolateGitConfiguration(t)

	var executions []git.Execution
	runner := git.NewRunner(func(execution git.Execution) {
		executions = append(executions, execution)
	})

	dir := t.TempDir()
	runGit(t, runner, dir, "init", "-b", "main")

	// The observer feeds the UI's log panel: it has to see failures too, not
	// only successes.
	if _, err := runner.Run(context.Background(), dir, "checkout", "missing-branch"); err == nil {
		t.Fatal("the checkout should have failed")
	}

	if len(executions) != 2 {
		t.Fatalf("expected 2 observed executions, got %d", len(executions))
	}
	if executions[0].ExitCode != 0 {
		t.Errorf("git init: exit code %d", executions[0].ExitCode)
	}
	if executions[1].ExitCode == 0 {
		t.Error("the failing checkout must be observed with a non-zero exit code")
	}
	if executions[1].CommandLine() != "git checkout missing-branch" {
		t.Errorf("observed command = %q", executions[1].CommandLine())
	}
}

// TestErrorCarriesTheCauseWhenGitCannotStart covers a failure that produced a
// message saying nothing at all.
//
// When the binary never starts, os/exec reports the reason and the process
// writes no stderr. Reporting the captured stderr therefore reported
// emptiness: the user was told a command had failed with "(no error output)"
// while the only sentence that explained it — git missing from PATH — was held
// in an error nobody rendered.
func TestErrorCarriesTheCauseWhenGitCannotStart(t *testing.T) {
	isolateGitConfiguration(t)

	// A PATH with nothing in it: the git binary cannot be found, so the
	// process never starts.
	t.Setenv("PATH", t.TempDir())

	var observed []git.Execution
	runner := git.NewRunner(func(execution git.Execution) {
		observed = append(observed, execution)
	})

	_, err := runner.Run(context.Background(), t.TempDir(), "status")
	if err == nil {
		t.Fatal("running git with an empty PATH should fail")
	}

	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error of type %T, want *git.Error", err)
	}
	if !strings.Contains(gitError.Stderr, "executable file not found") {
		t.Errorf("Stderr = %q, want it to carry the reason the process never started", gitError.Stderr)
	}
	if !strings.Contains(gitError.Error(), "executable file not found") {
		t.Errorf("Error() = %q, want it to carry the reason", gitError.Error())
	}

	// The log panel is fed by the observer, so it has to see the reason too.
	if len(observed) != 1 {
		t.Fatalf("1 observed execution expected, got %d", len(observed))
	}
	if !strings.Contains(observed[0].Stderr, "executable file not found") {
		t.Errorf("observed stderr = %q, want the reason", observed[0].Stderr)
	}
}

// TestLogSurvivesAControlByteInASubject pins the reason the record terminator
// is what it is.
//
// It used to be 0x01, on the belief that git refuses that byte everywhere. git
// accepts it: `git commit -F` stores a message holding 0x01 verbatim, and a
// subject like the one below split one record into two, leaving the whole
// history unparseable — "expected 6 NUL-separated fields, got 5".
func TestLogSurvivesAControlByteInASubject(t *testing.T) {
	dir, runner := testRepository(t)

	// Written through a file: -m would go through the shell's own quoting.
	subject := "Revert \"add \x01 handling\""
	messageFile := filepath.Join(t.TempDir(), "message")
	if err := os.WriteFile(messageFile, []byte(subject+"\n"), 0o600); err != nil {
		t.Fatalf("writing the commit message: %v", err)
	}
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"commit", "--allow-empty", "-F", messageFile)...)

	commits, err := runner.Log(context.Background(), dir)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if commits[0].Subject != subject {
		t.Errorf("subject = %q, want %q", commits[0].Subject, subject)
	}
}

// TestLogScopeDrawsOnlyWhatIsCheckedOut: `--all` is what makes a repository
// with a thousand tags too wide to draw, and it is no longer what the
// interface asks for first.
func TestLogScopeDrawsOnlyWhatIsCheckedOut(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	// A branch that was never merged: reachable from a ref, not from HEAD.
	runGit(t, runner, dir, "checkout", "-b", "spare")
	commitEmpty(t, runner, dir, "E: only on spare")
	runGit(t, runner, dir, "checkout", "main")

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if head.Name != "main" || head.Detached {
		t.Fatalf("HEAD = %+v, expected the branch main", head)
	}

	checkedOut, err := runner.LogScope(ctx, dir, git.ScopeHead, refs, head, nil)
	if err != nil {
		t.Fatalf("LogScope head: %v", err)
	}
	if len(checkedOut) != 4 {
		t.Errorf("%d commits on main, expected 4 (A, B, C, M)", len(checkedOut))
	}
	for _, commit := range checkedOut {
		if strings.HasPrefix(commit.Subject, "E:") {
			t.Errorf("the unmerged commit %q is not on the current branch", commit.Subject)
		}
	}

	everything, err := runner.LogScope(ctx, dir, git.ScopeAll, refs, head, nil)
	if err != nil {
		t.Fatalf("LogScope all: %v", err)
	}
	if len(everything) != 5 {
		t.Errorf("%d commits across every ref, expected 5", len(everything))
	}
}

// TestLogScopeOfARepositoryWithNothingToWalk covers the two shapes of "there
// is nothing here yet". Both make `git log` exit 128, which is also what a
// misspelled revision does, so neither is read from git's answer.
func TestLogScopeOfARepositoryWithNothingToWalk(t *testing.T) {
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()

	runGit(t, runner, dir, "init", "-b", "main")

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("an unborn branch is a normal state, not an error: %v", err)
	}
	if head.SHA != "" {
		t.Fatalf("HEAD = %+v, expected nothing on an unborn branch", head)
	}

	for _, scope := range []git.Scope{git.ScopeHead, git.ScopeAll} {
		commits, err := runner.LogScope(ctx, dir, scope, refs, head, nil)
		if err != nil {
			t.Fatalf("%s: a repository with no commit is a normal state, not an error: %v", scope, err)
		}
		if len(commits) != 0 {
			t.Errorf("%s: expected no commit, got %d", scope, len(commits))
		}
	}
}

// TestLogScopeWalksADetachedHEADWithNoRefLeft is the case testing the refs
// alone got wrong: --all covers HEAD as well as refs/, so a repository whose
// every branch has been deleted under a detached HEAD still has a history.
func TestLogScopeWalksADetachedHEADWithNoRefLeft(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	runGit(t, runner, dir, "checkout", "--detach", "main")
	for _, ref := range []string{"main", "feature"} {
		runGit(t, runner, dir, "branch", "-D", ref)
	}
	runGit(t, runner, dir, "tag", "-d", "v1.0")

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected no ref left, got %v", refs)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if !head.Detached {
		t.Fatalf("HEAD = %+v, expected it detached", head)
	}

	for _, scope := range []git.Scope{git.ScopeHead, git.ScopeAll} {
		commits, err := runner.LogScope(ctx, dir, scope, refs, head, nil)
		if err != nil {
			t.Fatalf("%s: %v", scope, err)
		}
		if len(commits) != 4 {
			t.Errorf("%s: %d commits, expected the 4 reachable from the detached HEAD",
				scope, len(commits))
		}
	}
}

// TestLogScopeRefusesAScopeItDoesNotKnow. Falling back to --all would draw a
// picture that looks right and answers a question nobody asked.
func TestLogScopeRefusesAScopeItDoesNotKnow(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	_, err := runner.LogScope(ctx, dir, git.Scope("everything"), nil, git.HEAD{SHA: "irrelevant"}, nil)
	if err == nil {
		t.Fatal("a scope this package does not define must be refused")
	}
	if !strings.Contains(err.Error(), "everything") {
		t.Errorf("the refusal must name the scope it was given, got %q", err)
	}
}
