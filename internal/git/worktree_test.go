package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// worktreeRecords assembles `worktree list --porcelain -z` output: attributes
// NUL-terminated, an empty one ending each record.
func worktreeRecords(records ...[]string) []byte {
	var builder strings.Builder
	for _, attributes := range records {
		for _, attribute := range attributes {
			builder.WriteString(attribute)
			builder.WriteString("\x00")
		}
		builder.WriteString("\x00")
	}
	return []byte(builder.String())
}

func TestParseWorktrees(t *testing.T) {
	output := worktreeRecords(
		[]string{"worktree /repo", "HEAD " + strings.Repeat("a", 40), "branch refs/heads/main"},
		[]string{"worktree /repo-side", "HEAD " + strings.Repeat("b", 40), "detached"},
		[]string{"worktree /repo-locked", "HEAD " + strings.Repeat("c", 40),
			"branch refs/heads/release/2.0", "locked on a drive that is not always here"},
		[]string{"worktree /repo-gone", "HEAD " + strings.Repeat("d", 40),
			"branch refs/heads/old", "prunable gitdir file points to non-existent location"},
	)

	worktrees, err := git.ParseWorktrees(output, true)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 4 {
		t.Fatalf("len = %d: %+v", len(worktrees), worktrees)
	}

	if !worktrees[0].Main || worktrees[0].Branch != "main" {
		t.Errorf("first = %+v, want the main tree on main", worktrees[0])
	}
	for _, other := range worktrees[1:] {
		if other.Main {
			t.Errorf("%s claims to be the main tree", other.Path)
		}
	}
	if !worktrees[1].Detached || worktrees[1].Branch != "" {
		t.Errorf("second = %+v, want detached with no branch", worktrees[1])
	}
	// The short name, like every other list in the package.
	if worktrees[2].Branch != "release/2.0" {
		t.Errorf("third branch = %q", worktrees[2].Branch)
	}
	if !worktrees[2].Locked || worktrees[2].LockReason == "" {
		t.Errorf("third = %+v, want locked with a reason", worktrees[2])
	}
	if !worktrees[3].Prunable || worktrees[3].PrunableReason == "" {
		t.Errorf("fourth = %+v, want prunable with a reason", worktrees[3])
	}
}

// The reason `-z` is used at all: `--porcelain` alone prints paths raw, so a
// directory holding a newline splits one worktree into two.
func TestParseWorktreesKeepsANewlineInAPath(t *testing.T) {
	output := worktreeRecords(
		[]string{"worktree /repo", "HEAD " + strings.Repeat("a", 40), "branch refs/heads/main"},
		[]string{"worktree /oh\ndear", "HEAD " + strings.Repeat("b", 40), "detached"},
	)

	worktrees, err := git.ParseWorktrees(output, true)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("len = %d: %+v", len(worktrees), worktrees)
	}
	if worktrees[1].Path != "/oh\ndear" {
		t.Fatalf("path = %q", worktrees[1].Path)
	}
}

func TestParseWorktreesBare(t *testing.T) {
	output := worktreeRecords([]string{"worktree /repo.git", "bare"})

	worktrees, err := git.ParseWorktrees(output, true)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 1 || !worktrees[0].Bare || worktrees[0].HEAD != "" {
		t.Fatalf("worktrees = %+v", worktrees)
	}
}

func TestParseWorktreesEmpty(t *testing.T) {
	worktrees, err := git.ParseWorktrees(nil, true)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 0 {
		t.Fatalf("worktrees = %+v", worktrees)
	}
}

// An attribute before any worktree line means the format is not what this
// reads. Refused rather than attached to whatever came last.
func TestParseWorktreesRefusesAnOrphanAttribute(t *testing.T) {
	if _, err := git.ParseWorktrees([]byte("HEAD abc\x00\x00"), true); err == nil {
		t.Fatal("an attribute with no worktree line was accepted")
	}
}

// A keyword a later git adds is ignored rather than refused: the fields this
// reads are the ones it acts on.
func TestParseWorktreesIgnoresAnUnknownAttribute(t *testing.T) {
	output := worktreeRecords(
		[]string{"worktree /repo", "HEAD " + strings.Repeat("a", 40), "branch refs/heads/main",
			"something-git-adds later"},
	)

	worktrees, err := git.ParseWorktrees(output, true)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 1 || worktrees[0].Branch != "main" {
		t.Fatalf("worktrees = %+v", worktrees)
	}
}

func TestWorktreeArgs(t *testing.T) {
	for _, probe := range []struct {
		name string
		args []string
		want []string
	}{
		{"an existing branch", git.AddWorktreeArgs("/side", "side", "", false),
			[]string{"worktree", "add", "--", "/side", "side"}},
		{"a new branch", git.AddWorktreeArgs("/side", "main", "topic", false),
			[]string{"worktree", "add", "-b", "topic", "--", "/side", "main"}},
		{"detached", git.AddWorktreeArgs("/side", "v1.0", "", true),
			[]string{"worktree", "add", "--detach", "--", "/side", "v1.0"}},
		{"remove", git.RemoveWorktreeArgs("/side", false),
			[]string{"worktree", "remove", "--", "/side"}},
		{"remove by force", git.RemoveWorktreeArgs("/side", true),
			[]string{"worktree", "remove", "--force", "--", "/side"}},
	} {
		if strings.Join(probe.args, " ") != strings.Join(probe.want, " ") {
			t.Errorf("%s = %v, want %v", probe.name, probe.args, probe.want)
		}
	}
}

// The older git's output, which is the same grammar with newlines: read
// because the panel is on every screen and a red one there would be the whole
// interface saying no, not one operation refusing.
func TestParseWorktreesReadsTheOutputWithoutNUL(t *testing.T) {
	output := []byte(strings.Join([]string{
		"worktree /repo",
		"HEAD " + strings.Repeat("a", 40),
		"branch refs/heads/main",
		"",
		"worktree /repo-side",
		"HEAD " + strings.Repeat("b", 40),
		"detached",
		"",
		"",
	}, "\n"))

	worktrees, err := git.ParseWorktrees(output, false)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("len = %d: %+v", len(worktrees), worktrees)
	}
	if !worktrees[0].Main || worktrees[0].Branch != "main" || worktrees[0].Path != "/repo" {
		t.Errorf("first = %+v, want the main tree on main at /repo", worktrees[0])
	}
	if !worktrees[1].Detached || worktrees[1].Path != "/repo-side" {
		t.Errorf("second = %+v, want a detached tree at /repo-side", worktrees[1])
	}
}

// And what that costs, pinned rather than left as a surprise: a newline in a
// path is a record separator to this format and nothing can make it otherwise,
// so the path comes back cut at it. That is the whole reason `-z` was added,
// and the reason the fallback is only a fallback.
func TestParseWorktreesWithoutNULCutsANewlineInAPath(t *testing.T) {
	output := []byte("worktree /oh\ndear\nHEAD " + strings.Repeat("a", 40) + "\n\n")

	worktrees, err := git.ParseWorktrees(output, false)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 1 || worktrees[0].Path != "/oh" {
		t.Errorf("worktrees = %+v, want the one cut path this format cannot avoid", worktrees)
	}

	// The same bytes with the terminator git 2.36 writes, which is the case
	// the fallback exists to be worse than.
	whole, err := git.ParseWorktrees(
		worktreeRecords([]string{"worktree /oh\ndear", "HEAD " + strings.Repeat("a", 40)}), true)
	if err != nil {
		t.Fatalf("ParseWorktrees with NUL: %v", err)
	}
	if len(whole) != 1 || whole[0].Path != "/oh\ndear" {
		t.Errorf("with NUL = %+v, want the path kept whole", whole)
	}
}

// A newline is the ONLY thing the fallback loses, and a carriage return is not
// it. git terminates every attribute with LF on every platform, so a \r in
// this output belongs to the path — `build\r` is a legal directory name — and
// treating \r\n as a line ending would take the last character off it and
// leave a row whose remove button names a directory that is not there.
func TestParseWorktreesWithoutNULKeepsACarriageReturnInAPath(t *testing.T) {
	output := []byte("worktree /repos/build\r\nHEAD " + strings.Repeat("a", 40) + "\n\n")

	worktrees, err := git.ParseWorktrees(output, false)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 1 || worktrees[0].Path != "/repos/build\r" {
		t.Errorf("worktrees = %+v, want the path with its carriage return", worktrees)
	}
}

// The one field the two shapes spell differently. Without `-z` git has no way
// to put a newline in a lock reason, so it C-quotes the whole thing; the panel
// must show the sentence and not git's escaping of it.
func TestParseWorktreesWithoutNULUnquotesALockReason(t *testing.T) {
	output := []byte("worktree /repos/side\n" +
		"HEAD " + strings.Repeat("a", 40) + "\n" +
		`locked "needs a\nrebuild"` + "\n\n")

	worktrees, err := git.ParseWorktrees(output, false)
	if err != nil {
		t.Fatalf("ParseWorktrees: %v", err)
	}
	if len(worktrees) != 1 {
		t.Fatalf("len = %d: %+v", len(worktrees), worktrees)
	}
	if !worktrees[0].Locked || worktrees[0].LockReason != "needs a\nrebuild" {
		t.Errorf("locked = %+v, want the reason with its newline and no quoting",
			worktrees[0])
	}

	// Under `-z` the same reason arrives raw, and unquoting it would be
	// undoing an escape git never wrote.
	whole, err := git.ParseWorktrees(worktreeRecords(
		[]string{"worktree /repos/side", `locked "quoted" on purpose`}), true)
	if err != nil {
		t.Fatalf("ParseWorktrees with NUL: %v", err)
	}
	if len(whole) != 1 || whole[0].LockReason != `"quoted" on purpose` {
		t.Errorf("with NUL = %+v, want the reason byte for byte", whole)
	}
}

func TestWorktreeListArgsAsksForNULOnlyWhereGitHasIt(t *testing.T) {
	if got := strings.Join(git.WorktreeListArgs(true), " "); got != "worktree list --porcelain -z" {
		t.Errorf("with NUL = %q", got)
	}
	if got := strings.Join(git.WorktreeListArgs(false), " "); got != "worktree list --porcelain" {
		t.Errorf("without NUL = %q", got)
	}
}
