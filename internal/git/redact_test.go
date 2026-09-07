package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The token that must never come back out. Distinctive enough that a substring
// search over anything the daemon produces is conclusive.
const secret = "ghp_ThisMustNeverBeLogged"

func TestRedactTextHidesCredentialsInsideASentence(t *testing.T) {
	// git names the URL it could not reach, whole, in the middle of prose. The
	// sentence is shown, broadcast and journalled, so this is where a token
	// escapes even when the argument list is clean.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "git's own not-found line",
			in:   "fatal: repository 'https://ada:" + secret + "@github.com/ada/x.git/' not found",
			want: "fatal: repository 'https://ada:***@github.com/ada/x.git/' not found",
		},
		{
			name: "a token used as the whole userinfo goes whole",
			in:   "remote: https://" + secret + "@github.com/ada/x.git failed",
			want: "remote: https://***@github.com/ada/x.git failed",
		},
		{
			name: "two URLs in one line",
			in:   "https://a:" + secret + "@h/one and https://b:" + secret + "@h/two",
			want: "https://a:***@h/one and https://b:***@h/two",
		},
		{
			name: "a URL with no credentials is untouched",
			in:   "fatal: could not read from https://github.com/ada/x.git",
			want: "fatal: could not read from https://github.com/ada/x.git",
		},
		{
			name: "the scp-like form has nowhere to put a password",
			in:   "fatal: git@github.com:ada/x.git is unreachable",
			want: "fatal: git@github.com:ada/x.git is unreachable",
		},
		{
			name: "prose holding :// without a scheme is left alone",
			in:   "see ://nothing here",
			want: "see ://nothing here",
		},
		{
			name: "nothing to do",
			in:   "error: pathspec 'main' did not match",
			want: "error: pathspec 'main' did not match",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := git.RedactText(testCase.in); got != testCase.want {
				t.Errorf("RedactText(%q)\n got %q\nwant %q", testCase.in, got, testCase.want)
			}
		})
	}
}

// The regression this file exists for.
//
// A credential in a remote URL used to reach four places that outlive the
// request: the log panel of every open tab, the backlog at GET /api/log, the
// JSON of a failed command, and the daemon's journal on disk. All four read
// what the Runner hands its observer, so one check at that boundary covers
// them.
func TestNoCredentialReachesTheObserverOrTheError(t *testing.T) {
	observed := []git.Execution{}
	runner := git.NewRunner(func(execution git.Execution) {
		observed = append(observed, execution)
	})

	// A real repository, so the command runs and fails for the honest reason:
	// there is nothing at that address.
	directory := t.TempDir()
	if _, err := runner.Run(context.Background(), directory, "init", "--quiet", "."); err != nil {
		t.Fatalf("init: %v", err)
	}

	url := "https://ada:" + secret + "@127.0.0.1:1/ada/x.git"
	_, err := runner.Run(context.Background(), directory, "remote", "add", "origin", url)
	if err != nil {
		t.Fatalf("remote add: %v", err)
	}

	if len(observed) == 0 {
		t.Fatal("the observer saw nothing, so this test proves nothing")
	}

	for _, execution := range observed {
		if strings.Contains(execution.CommandLine(), secret) {
			t.Errorf("the command line carries the token: %s", execution.CommandLine())
		}
		if strings.Contains(strings.Join(execution.Args, " "), secret) {
			t.Errorf("the recorded arguments carry the token: %v", execution.Args)
		}
		if strings.Contains(execution.Stderr, secret) {
			t.Errorf("the recorded stderr carries the token: %s", execution.Stderr)
		}
	}

	// And the shape has to survive, or the log panel stops being useful: what
	// is hidden is the password, not the command.
	last := observed[len(observed)-1].CommandLine()
	for _, kept := range []string{"remote", "add", "origin", "127.0.0.1", "ada/x.git"} {
		if !strings.Contains(last, kept) {
			t.Errorf("redaction removed %q as well: %s", kept, last)
		}
	}
	if !strings.Contains(last, "ada:***") {
		t.Errorf("the account name is worth keeping, and the marker is what says a secret was there: %s", last)
	}
}

// A failed command reaches the user as an *Error, whose message is built from
// the same arguments and the same stderr.
func TestNoCredentialReachesAFailedCommandsError(t *testing.T) {
	runner := git.NewRunner(nil)
	directory := t.TempDir()

	// A path that is not a repository, so `git remote add` fails and the Error
	// carries the arguments it was given.
	url := "https://ada:" + secret + "@127.0.0.1:1/ada/x.git"
	_, err := runner.Run(context.Background(), directory, "remote", "add", "origin", url)
	if err == nil {
		t.Fatal("git succeeded outside a repository, so this test proves nothing")
	}

	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error message carries the token: %s", err)
	}

	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("not a *git.Error: %T", err)
	}
	if strings.Contains(strings.Join(failure.Args, " "), secret) {
		t.Errorf("the error's arguments carry the token: %v", failure.Args)
	}
	if strings.Contains(failure.Stderr, secret) {
		t.Errorf("the error's stderr carries the token: %s", failure.Stderr)
	}
	if strings.Contains(failure.CommandLine(), secret) {
		t.Errorf("the error's command line carries the token: %s", failure.CommandLine())
	}
}

// A clone is the route where a credential URL is most likely to be typed, and
// the one whose command line is broadcast for the whole of a long transfer.
func TestNoCredentialReachesTheObserverDuringAClone(t *testing.T) {
	observed := []git.Execution{}
	runner := git.NewRunner(func(execution git.Execution) {
		observed = append(observed, execution)
	})

	directory := t.TempDir()
	destination := filepath.Join(directory, "clone")

	// Port 1 refuses at once, so this fails immediately rather than waiting
	// out the network timeout.
	url := "https://ada:" + secret + "@127.0.0.1:1/ada/x.git"
	err := runner.Clone(context.Background(), url, destination, func(string) {})
	if err == nil {
		t.Fatal("the clone succeeded, so this test proves nothing")
	}

	if strings.Contains(err.Error(), secret) {
		t.Errorf("the clone error carries the token: %s", err)
	}
	for _, execution := range observed {
		if strings.Contains(execution.CommandLine(), secret) {
			t.Errorf("the clone command line carries the token: %s", execution.CommandLine())
		}
		if strings.Contains(execution.Stderr, secret) {
			t.Errorf("the clone stderr carries the token: %s", execution.Stderr)
		}
	}

	// Nothing the daemon would write anywhere may hold it either.
	if entries, err := os.ReadDir(directory); err == nil {
		for _, entry := range entries {
			if strings.Contains(entry.Name(), secret) {
				t.Errorf("a path on disk carries the token: %s", entry.Name())
			}
		}
	}
}
