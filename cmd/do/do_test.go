package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The shell version of this program had no tests at all, which is most of why
// it was worth rewriting. These cover the two pieces where being wrong is
// silent: reading .env, and deciding what address the daemon listens on.

func TestParseEnvFileReadsAValue(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		key      string
		want     string
	}{
		{
			name:     "a plain assignment",
			contents: "YAGIT_ROOT=/home/ada/code\n",
			key:      "YAGIT_ROOT",
			want:     "/home/ada/code",
		},
		{
			name: "the last assignment wins",
			// Someone pasting a line at the bottom of the file expects it to
			// take effect, which is also what a shell sourcing the file would
			// have done.
			contents: "YAGIT_ROOT=/first\nYAGIT_ROOT=/second\n",
			key:      "YAGIT_ROOT",
			want:     "/second",
		},
		{
			name:     "a commented-out assignment is not one",
			contents: "#   YAGIT_PUBLIC_HOST=my-dev-box.local\n",
			key:      "YAGIT_PUBLIC_HOST",
			want:     "",
		},
		{
			name: "a longer key is not this key",
			// Prefix matching here would read YAGIT_ROOT out of a variable
			// that merely starts the same way, and point the daemon at the
			// wrong tree — with a security boundary attached to it.
			contents: "YAGIT_ROOT_ARCHIVE=/elsewhere\n",
			key:      "YAGIT_ROOT",
			want:     "",
		},
		{
			name:     "double quotes are stripped",
			contents: `YAGIT_ROOT="/home/ada/my code"` + "\n",
			key:      "YAGIT_ROOT",
			want:     "/home/ada/my code",
		},
		{
			name:     "single quotes are stripped",
			contents: "YAGIT_ROOT='/home/ada/my code'\n",
			key:      "YAGIT_ROOT",
			want:     "/home/ada/my code",
		},
		{
			name: "mismatched quotes are left alone",
			// Stripping here would invent a path the user did not write. The
			// daemon refuses a directory that does not exist, which is the
			// error they need to see.
			contents: `YAGIT_ROOT="/home/ada` + "\n",
			key:      "YAGIT_ROOT",
			want:     `"/home/ada`,
		},
		{
			name:     "an absent key is empty, not an error",
			contents: "YAGIT_ROOT=/home/ada\n",
			key:      "YAGIT_PUBLIC_HOST",
			want:     "",
		},
		{
			name:     "a file with no trailing newline still parses",
			contents: "YAGIT_ROOT=/home/ada",
			key:      "YAGIT_ROOT",
			want:     "/home/ada",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := parseEnvFile(testCase.contents).value(testCase.key); got != testCase.want {
				t.Errorf("value(%q) = %q, want %q", testCase.key, got, testCase.want)
			}
		})
	}
}

func TestParseEnvFileAgainstTheShippedExample(t *testing.T) {
	// .env.example is what every first run is parsed from. A parser that
	// disagrees with it is a parser that breaks the very first command anyone
	// types.
	contents, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}

	example := parseEnvFile(string(contents))
	if root := example.value("YAGIT_ROOT"); root != "__HOME__" {
		t.Errorf("YAGIT_ROOT reads as %q; .env.example is supposed to hold the __HOME__ placeholder", root)
	}

	// The example documents YAGIT_PUBLIC_HOST inside a comment. Reading it as
	// a value would widen the daemon's listen address to 0.0.0.0 on every
	// fresh checkout, which is the opposite of the default this project wants.
	if host := example.value("YAGIT_PUBLIC_HOST"); host != "" {
		t.Errorf("YAGIT_PUBLIC_HOST reads as %q out of a commented-out line", host)
	}
}

func TestTheShippedExampleSetsNothingThatIsIgnored(t *testing.T) {
	// The contract between .env.example and loadConfiguration, checked rather
	// than remembered: a key documented as an assignment must be one `./do`
	// reads. Ship one that is not and every first run of every fresh clone
	// opens with a warning about the file the project wrote itself.
	contents, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}

	env := parseEnvFile(string(contents))
	readConfiguration(env)

	if ignored := env.ignoredKeys(); len(ignored) != 0 {
		t.Errorf(".env.example assigns %v, which ./do does not read", ignored)
	}
}

func TestParseEnvFileCountsAssignmentsAndNotProse(t *testing.T) {
	env := parseEnvFile(strings.Join([]string{
		"# YAGIT_COMMENTED=1",     // documentation, not a setting
		"   #YAGIT_TIGHT=1",       // and still documentation without the space
		"",                        // blank lines are not assignments
		"YAGIT_ROOT=/srv",         //
		"  YAGIT_INDENTED=/srv  ", // leading space is not part of the name
		"YAGIT_NO_EQUALS",         // not an assignment either
		"YAGIT_SPACED = /srv",     // a shell would read this as a command
		"YAGIT_ROOT=/srv/again",   // the last assignment wins, and is not a second key
	}, "\n"))

	want := []string{"YAGIT_ROOT", "YAGIT_INDENTED"}
	if !slices.Equal(env.keys, want) {
		t.Errorf("keys = %v, want %v", env.keys, want)
	}
	if got := env.value("YAGIT_ROOT"); got != "/srv/again" {
		t.Errorf("YAGIT_ROOT = %q, want the last assignment", got)
	}
	if got := env.value("YAGIT_SPACED"); got != "" {
		t.Errorf("YAGIT_SPACED = %q; a name with a space before the = is not an assignment", got)
	}
}

func TestIgnoredKeysAreTheOnesNothingAskedFor(t *testing.T) {
	// The three names in this file are not invented: they are what a .env
	// grows when nothing ever says a key does nothing. YAGIT_ADDRESS is
	// almost YAGIT_ADDR, YAGIT_PORT is almost the port constant, and
	// YAGIT_TOKEN is a real variable the daemon reads and `./do` overrides.
	env := parseEnvFile(strings.Join([]string{
		"YAGIT_ROOT=/srv",
		"YAGIT_TOKEN=secret",
		"YAGIT_PORT=7420",
		"YAGIT_ADDRESS=127.0.0.1:7420",
		"EDITOR=vi",
	}, "\n"))
	readConfiguration(env)

	want := []string{"YAGIT_TOKEN", "YAGIT_PORT", "YAGIT_ADDRESS"}
	if ignored := env.ignoredKeys(); !slices.Equal(ignored, want) {
		t.Errorf("ignoredKeys = %v, want %v", ignored, want)
	}
}

func TestListenAddressFollowsThePublicHost(t *testing.T) {
	loopbackCases := map[string]string{
		"127.0.0.1": "127.0.0.1:7420",
		"localhost": "127.0.0.1:7420",
		"::1":       "127.0.0.1:7420",
	}

	for host, want := range loopbackCases {
		got, err := listenAddress(host, false)
		if err != nil {
			t.Fatalf("listenAddress(%q): %v", host, err)
		}
		if got != want {
			t.Errorf("listenAddress(%q) = %q, want %q", host, got, want)
		}
	}

	for _, host := range []string{"my-dev-box", "192.168.1.10", "box.example.com"} {
		if _, err := listenAddress(host, false); err == nil {
			t.Errorf("listenAddress(%q) without listen-all should refuse", host)
		}
		got, err := listenAddress(host, true)
		if err != nil {
			t.Fatalf("listenAddress(%q, listenAll): %v", host, err)
		}
		if got != "0.0.0.0:7420" {
			t.Errorf("listenAddress(%q, listenAll) = %q, want 0.0.0.0:7420", host, got)
		}
	}
}

func TestEveryCommandIsListedExactlyOnce(t *testing.T) {
	// The help is printed from commandOrder and dispatch reads commands. A
	// subcommand missing from either is a subcommand that exists and is
	// undiscoverable, or is documented and does not run.
	if len(commandOrder) != len(commands) {
		t.Errorf("commandOrder lists %d commands, the table holds %d", len(commandOrder), len(commands))
	}

	seen := make(map[string]bool, len(commandOrder))
	for _, name := range commandOrder {
		if _, known := commands[name]; !known {
			t.Errorf("commandOrder lists %q, which is not a command", name)
		}
		if seen[name] {
			t.Errorf("commandOrder lists %q twice", name)
		}
		seen[name] = true
	}

	for name := range commands {
		if !seen[name] {
			t.Errorf("%q is a command but the help never prints it", name)
		}
	}
}

// TestDocumentationSpellsEveryCommandAsTheProgramDoes holds the places that
// list the commands to the one table that runs them.
//
// The usage line in `commands` is what `./do help` prints and what dispatch
// enforces. Each document below restates it for an audience — DEVELOPMENT.md
// for somebody building from source, AGENTS.md for a tool, and the shim's own
// header for whoever opens the file — and a flag added to the program reached
// none of them until this test asked.
//
// The list is the documents that DO restate the table, not the documents that
// exist. README.md and CONTRIBUTING.md are deliberately absent: the README is
// for somebody installing a released binary, who never types `./do` at all,
// and CONTRIBUTING points at DEVELOPMENT rather than copying it. A document
// added here that does not carry the table fails every command at once, which
// is the honest signal — the wrong reading of a red run here is to delete the
// assertion rather than the entry.
func TestDocumentationSpellsEveryCommandAsTheProgramDoes(t *testing.T) {
	for _, document := range []string{filepath.Join("docs", "DEVELOPMENT.md"), filepath.Join("agent", "AGENTS.md"), "do"} {
		contents := readFileString(t, filepath.Join("..", "..", document))
		// A Markdown table cell has to escape the pipe in `test [go|web|…]`.
		text := strings.ReplaceAll(contents, `\|`, "|")

		for _, name := range commandOrder {
			usage := "./do " + commands[name].usage
			if !strings.Contains(text, usage) {
				t.Errorf("%s does not list `%s`", document, usage)
			}
		}
	}
}

func TestLocateProjectWalksUpToTheCheckout(t *testing.T) {
	// Tests run in the package directory, two levels below go.mod. Every path
	// this program touches is derived from what this returns, so a wrong
	// answer here writes .env and the token file into the wrong tree.
	located, err := locateProject()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(located.path("go.mod")); err != nil {
		t.Errorf("locateProject returned %s, which holds no go.mod", located.directory)
	}
	if _, err := os.Stat(located.path("cmd", "do", "main.go")); err != nil {
		t.Errorf("locateProject returned %s, which is not the yagit checkout", located.directory)
	}
}
