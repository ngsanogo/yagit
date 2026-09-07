package git_test

import (
	"context"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestParseVersion(t *testing.T) {
	for output, want := range map[string]git.Version{
		"git version 2.39.5\n": {Major: 2, Minor: 39},
		// The suffix a platform's own build adds. Ignoring it is the point:
		// tripping over one would disable a feature on that platform alone.
		"git version 2.39.5 (Apple Git-154)\n": {Major: 2, Minor: 39},
		"git version 2.30.2.windows.1\n":       {Major: 2, Minor: 30},
		"git version 2.45.0-rc1\n":             {Major: 2, Minor: 45},
		// Two numbers is all this reads, so two numbers is enough to give it.
		"git version 3.0": {Major: 3, Minor: 0},
	} {
		got, err := git.ParseVersion(output)
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", output, err)
			continue
		}
		if got != want {
			t.Errorf("ParseVersion(%q) = %v, want %v", output, got, want)
		}
	}
}

func TestParseVersionRefusesWhatIsNotOne(t *testing.T) {
	for _, output := range []string{
		"", "git", "git version", "git version x.y", "git version 2",
		"hg version 2.39.5", "git version two.point.nine",
	} {
		if _, err := git.ParseVersion(output); err == nil {
			t.Errorf("ParseVersion(%q) = nil error, want a refusal", output)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	version := git.Version{Major: 2, Minor: 36}

	for _, allowed := range [][2]int{{2, 36}, {2, 35}, {2, 0}, {1, 99}} {
		if !version.AtLeast(allowed[0], allowed[1]) {
			t.Errorf("%v.AtLeast(%d, %d) = false", version, allowed[0], allowed[1])
		}
	}
	// The comparison that matters, and the one a lexical compare gets wrong:
	// 2.9 is older than 2.36, not newer.
	for _, refused := range [][2]int{{2, 37}, {2, 100}, {3, 0}} {
		if version.AtLeast(refused[0], refused[1]) {
			t.Errorf("%v.AtLeast(%d, %d) = true", version, refused[0], refused[1])
		}
	}
	if !(git.Version{Major: 2, Minor: 36}).AtLeast(2, 9) {
		t.Error("2.36 should be at least 2.9")
	}
}

// The version the tests run against is the one this machine has, so what is
// pinned here is that it can be read at all — the failure that would leave the
// worktree panel guessing.
func TestGitVersionReadsThisMachinesGit(t *testing.T) {
	runner := git.NewRunner(nil)
	ctx := context.Background()

	version, err := runner.GitVersion(ctx)
	if err != nil {
		t.Fatalf("GitVersion: %v", err)
	}
	if version.Major < 2 {
		t.Errorf("GitVersion = %v, want a git of 2 or later", version)
	}

	// Cached: the second answer is the first, not a second subprocess.
	again, err := runner.GitVersion(ctx)
	if err != nil {
		t.Fatalf("GitVersion again: %v", err)
	}
	if again != version {
		t.Errorf("GitVersion = %v then %v", version, again)
	}
}
