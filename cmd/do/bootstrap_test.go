package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestShellHookNamesTheCheckout(t *testing.T) {
	p := newProject(t)

	if err := runShellHook(p, nil); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(p.path(shellHookFileName))
	if err != nil {
		t.Fatal(err)
	}

	// Quoted, because that is how the path goes into a file the shell will
	// source: a checkout under a path with a space in it has to survive being
	// read back, and on Windows the separators are escaped along with it.
	// Looking for the raw path passes here and fails there, for a reason
	// nothing in the failure would show.
	if !strings.Contains(string(contents), strconv.Quote(p.directory)) {
		t.Fatalf("shell hook does not reference the checkout:\n%s", contents)
	}
	if !strings.Contains(string(contents), "yagit-up") {
		t.Fatal("shell hook missing yagit-up alias")
	}
}

// TestShellHookForwardsItsArguments is why `yagit-up --new-token` rotates the
// token. A shell function drops the arguments it is not told to pass on, in
// silence, so the alias used to run a plain `./do up` — and print the leaked
// token it had been typed to replace.
func TestShellHookForwardsItsArguments(t *testing.T) {
	p := newProject(t)
	if err := runShellHook(p, nil); err != nil {
		t.Fatal(err)
	}

	contents := readFileString(t, p.path(shellHookFileName))
	for _, call := range []string{`./do up "$@"`, `./do down "$@"`, `./do logs "$@"`} {
		if !strings.Contains(contents, call) {
			t.Errorf("shell hook does not forward arguments to %s:\n%s", call, contents)
		}
	}
}

func TestShellHookRefusesAnUnknownFlag(t *testing.T) {
	if err := runShellHook(newProject(t), []string{"--wat"}); err == nil {
		t.Fatal("shell-hook should refuse an unknown flag")
	}
}

// TestShellProfilePathRefusesToGuess covers the half of --write that writes to
// somebody's home directory.
//
// fish, nushell and the rest read neither ~/.bashrc nor ~/.zshrc, and $SHELL
// is unset outright in plenty of non-interactive contexts. Picking one of the
// two anyway reports success about a line that is never sourced, and leaves an
// edit in a file the person never asked to have edited.
func TestShellProfilePathRefusesToGuess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // what os.UserHomeDir reads on Windows

	cases := []struct {
		shell string
		want  string
	}{
		{shell: "/bin/bash", want: ".bashrc"},
		{shell: "/usr/local/bin/zsh", want: ".zshrc"},
		{shell: "/usr/bin/fish", want: ""},
		{shell: "", want: ""},
	}

	for _, test := range cases {
		t.Setenv("SHELL", test.shell)

		path, err := shellProfilePath()
		if test.want == "" {
			if err == nil {
				t.Errorf("SHELL=%q: wrote to %s instead of refusing", test.shell, path)
			}
			continue
		}
		if err != nil {
			t.Errorf("SHELL=%q: %v", test.shell, err)
			continue
		}
		if path != filepath.Join(home, test.want) {
			t.Errorf("SHELL=%q gave %q, want %s", test.shell, path, test.want)
		}
	}
}

// TestAppendLineOnlyEverAdds covers what is at stake: this writes to a file
// full of somebody else's configuration.
func TestAppendLineOnlyEverAdds(t *testing.T) {
	profile := filepath.Join(t.TempDir(), ".bashrc")

	// No trailing newline, which is how a hand-edited profile often ends. The
	// line must not be glued onto the end of the last one.
	writeFile(t, profile, "export EDITOR=vi\nalias ll='ls -l'")

	line := `source "/checkout/.yagit/shell.sh"`
	if err := appendLine(profile, line); err != nil {
		t.Fatalf("appendLine: %v", err)
	}

	after := readFileString(t, profile)
	if !strings.HasPrefix(after, "export EDITOR=vi\nalias ll='ls -l'\n") {
		t.Errorf("the existing configuration did not survive:\n%s", after)
	}
	if !strings.HasSuffix(after, line+"\n") {
		t.Errorf("the line was not appended:\n%s", after)
	}

	// Twice must be the same as once: `./do shell-hook --write` is a command
	// people re-run, and a profile that grows a line each time is a bug people
	// find months later.
	if err := appendLine(profile, line); err != nil {
		t.Fatalf("appendLine again: %v", err)
	}
	if got := strings.Count(readFileString(t, profile), line); got != 1 {
		t.Errorf("the line appears %d times, want 1", got)
	}
}

func TestAppendLineCreatesAProfileThatIsNotThereYet(t *testing.T) {
	profile := filepath.Join(t.TempDir(), ".zshrc")

	if err := appendLine(profile, "source /x"); err != nil {
		t.Fatalf("appendLine: %v", err)
	}
	if got := readFileString(t, profile); got != "source /x\n" {
		t.Errorf("new profile holds %q", got)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(contents)
}
