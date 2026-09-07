package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Which deadline a command runs under is a decision that is easy to make once
// and then never again: a new command is written next to an old one, copies its
// `r.Run(...)`, and inherits the thirty seconds meant for commands that return
// instantly. Nothing fails, because nothing here is slow on the machine it was
// written on.
//
// It fails on the customer's machine instead, and it fails destructively — a
// checkout killed halfway leaves the work tree split across two branches, and a
// commit killed while a pre-commit hook runs leaves an index.lock behind.
//
// So the list is written down and checked. Adding a command that rewrites the
// work tree or runs a user hook means adding it here, which is the moment to
// think about its deadline.
var commandsThatMustNotUseTheDefaultDeadline = []struct {
	function string
	because  string
}{
	{"Switch", "a checkout rewrites the work tree and runs the post-checkout hook"},
	{"Detach", "a checkout rewrites the work tree and runs the post-checkout hook"},
	{"CreateAndSwitch", "a checkout rewrites the work tree and runs the post-checkout hook"},
	{"DiscardTracked", "restoring files runs the smudge filter over each one"},
	{"Commit", "a commit runs the pre-commit and commit-msg hooks"},
}

func TestWorkTreeCommandsCarryTheRewriteDeadline(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	bodies := map[string]string{}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		text := string(contents)

		// Split on the declaration keyword at the start of a line: a function
		// body runs to the next top-level declaration, and nothing in this
		// package indents one.
		for _, chunk := range strings.Split(text, "\nfunc ") {
			name, _, found := strings.Cut(chunk, "(")
			if !found {
				continue
			}
			// Methods read `(r *Runner) Name(`, so the name is after the
			// receiver rather than before the first parenthesis.
			if strings.HasPrefix(chunk, "(r *Runner) ") {
				name, _, found = strings.Cut(strings.TrimPrefix(chunk, "(r *Runner) "), "(")
				if !found {
					continue
				}
			}
			bodies[strings.TrimSpace(name)] = chunk
		}
	}

	for _, command := range commandsThatMustNotUseTheDefaultDeadline {
		body, present := bodies[command.function]
		if !present {
			t.Errorf("%s is listed here but no longer exists: remove it, or rename it here", command.function)
			continue
		}
		if !strings.Contains(body, "rewriteTimeout") {
			t.Errorf("%s runs under the default thirty seconds, and it must not: %s",
				command.function, command.because)
		}
	}
}
