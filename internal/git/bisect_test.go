package git_test

import (
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestBisectStartArgsWithNoEnds(t *testing.T) {
	t.Parallel()

	args, err := git.BisectStartArgs("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if line := git.CommandLine(args); line != "git bisect start" {
		t.Fatalf("command = %q", line)
	}
}

func TestBisectStartArgsWithBothEnds(t *testing.T) {
	t.Parallel()

	args, err := git.BisectStartArgs("badtip", []string{"goodbase"})
	if err != nil {
		t.Fatal(err)
	}
	if line := git.CommandLine(args); line != "git bisect start badtip goodbase --" {
		t.Fatalf("command = %q", line)
	}
}

func TestBisectStartArgsRefusesADashRevision(t *testing.T) {
	t.Parallel()

	if _, err := git.BisectStartArgs("--output=/tmp/x", nil); !errors.Is(err, git.ErrBadRevision) {
		t.Fatalf("err = %v, want ErrBadRevision", err)
	}
}

func TestBisectStartArgsNeedsBadWhenGoodIsNamed(t *testing.T) {
	t.Parallel()

	if _, err := git.BisectStartArgs("", []string{"goodbase"}); !errors.Is(err, git.ErrBadRevision) {
		t.Fatalf("err = %v, want ErrBadRevision", err)
	}
}
