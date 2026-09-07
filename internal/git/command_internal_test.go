package git

import (
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// These tests reach inside the package on purpose: the environment handed to
// git is not observable from the outside, and it is the one place where being
// wrong on a platform nobody develops on produces failures that blame the
// network, the credentials, or git itself.

// everyPlatformYagitShipsFor mirrors the build targets in ./do. A platform
// that gets a binary gets its environment checked here.
var everyPlatformYagitShipsFor = []string{"linux", "darwin", "windows"}

func TestInheritedNamesCarryWhatGitCannotWorkWithout(t *testing.T) {
	// PATH finds git's own subcommands and the user's helpers;
	// GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are the only way a test — or a
	// user — can hand git a known configuration.
	required := []string{"PATH", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"}

	for _, platform := range everyPlatformYagitShipsFor {
		names := inheritedNames(platform)
		for _, name := range required {
			if !slices.Contains(names, name) {
				t.Errorf("%s: %s is not passed through, and git needs it everywhere", platform, name)
			}
		}
	}
}

func TestWindowsInheritsWhatWindowsItselfNeeds(t *testing.T) {
	// Each of these breaks something that does not look like an environment
	// problem when it is missing: SystemRoot breaks name resolution inside
	// Winsock, PATHEXT hides every credential helper that ships as a .cmd,
	// and the home variables send git looking for a ~/.gitconfig that is not
	// where the user put it.
	required := []string{
		"SystemRoot", "SystemDrive", "PATHEXT", "COMSPEC",
		"HOME", "HOMEDRIVE", "HOMEPATH", "USERPROFILE",
		"APPDATA", "LOCALAPPDATA", "ProgramData",
		"TEMP", "TMP",
	}

	names := inheritedNames("windows")
	for _, name := range required {
		if !slices.Contains(names, name) {
			t.Errorf("windows: %s is not passed through", name)
		}
	}
}

func TestUnixDoesNotInheritWindowsVariables(t *testing.T) {
	// Not a style rule. An allowlist is only readable while every name on it
	// has a reason to be there, and a Windows variable on a Linux list is a
	// name nobody can justify — which is how allowlists rot into "everything
	// anyone ever added".
	for _, platform := range []string{"linux", "darwin"} {
		for _, name := range inheritedNames(platform) {
			if strings.HasPrefix(name, "Program") || name == "SystemRoot" || name == "PATHEXT" {
				t.Errorf("%s: inherits %s, which only means something on Windows", platform, name)
			}
		}
	}
}

func TestNoPlatformListsAVariableTwice(t *testing.T) {
	for _, platform := range everyPlatformYagitShipsFor {
		names := inheritedNames(platform)
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			if seen[name] {
				t.Errorf("%s: %s appears twice", platform, name)
			}
			seen[name] = true
		}
	}
}

func TestNoPlatformHandsGitAnAgent(t *testing.T) {
	// A command that cannot authenticate must fail saying so, rather than
	// succeed because an agent leaked in through an inherited environment.
	// Every name git could reach a key through is listed here, and each one
	// leaves this list only by being written into the one below with the
	// operation that made it necessary.
	agents := []string{"SSH_AUTH_SOCK", "SSH_AGENT_PID", "GPG_AGENT_INFO", "GNUPGHOME", "GIT_ASKPASS", "SSH_ASKPASS"}

	// Phase 6 brought commits, and a user with commit.gpgsign set has decided
	// that an unsigned commit is not acceptable. Signing is the operation;
	// these two are what it needs. Both are the user's own agents, on the
	// user's own machine, reached by a daemon already running as them.
	//
	// GIT_ASKPASS and SSH_ASKPASS stay out and are a different question: they
	// name a PROGRAM git runs to ask for a passphrase, and a daemon with no
	// terminal has nowhere to ask. They arrive, if ever, with an interface
	// that can do the asking.
	deliberate := []string{"GNUPGHOME", "SSH_AUTH_SOCK"}

	for _, platform := range everyPlatformYagitShipsFor {
		for _, name := range inheritedNames(platform) {
			if slices.Contains(agents, name) && !slices.Contains(deliberate, name) {
				t.Errorf("%s: inherits %s; if that is now intended, say so here too", platform, name)
			}
		}
	}
}

func TestSigningReachesTheUsersAgentsOnEveryPlatform(t *testing.T) {
	// The other half of the test above. Dropping a name from the lists would
	// otherwise be silent: commits would go on being made, unsigned, on a
	// machine whose owner asked for every one of them to be signed.
	for _, platform := range everyPlatformYagitShipsFor {
		names := inheritedNames(platform)
		for _, needed := range []string{"GNUPGHOME", "SSH_AUTH_SOCK"} {
			if !slices.Contains(names, needed) {
				t.Errorf("%s: does not inherit %s, so commit signing cannot reach the user's agent", platform, needed)
			}
		}
	}
}

func TestNetworkCommandsReachTheProxyAndTheUsersSSH(t *testing.T) {
	// The other half of the agent test above, for the commands that leave the
	// machine. Each of these breaks a fetch on a machine where it is set, and
	// none of them breaks it in a way that looks like an environment problem:
	// a dropped proxy hangs until the deadline and blames a network that is
	// working, and a dropped GIT_SSH_COMMAND fails to authenticate on a
	// machine whose key is right there.
	required := []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"GIT_SSH_COMMAND", "GIT_SSH", "XDG_CONFIG_HOME",
	}

	for _, platform := range everyPlatformYagitShipsFor {
		names := inheritedNames(platform)
		for _, name := range required {
			if !slices.Contains(names, name) {
				t.Errorf("%s: %s is not passed through, so a fetch fails where it is set", platform, name)
			}
		}
	}
}

func TestCommandEnvironmentPinsYagitSettings(t *testing.T) {
	environment := commandEnvironment()

	fixed := []string{
		"LC_ALL=C",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_PAGER=cat",
	}
	for _, entry := range fixed {
		if !slices.Contains(environment, entry) {
			t.Errorf("%q is missing from the environment handed to git", entry)
		}
	}

	// The count is asserted against the list rather than against a number
	// written here. A literal would have to be edited every time a setting is
	// pinned, and the edit that gets forgotten is the one that matters: git
	// reads the first COUNT pairs and ignores the rest, so a setting added
	// past a stale count applies to nothing and no test says so.
	if want := "GIT_CONFIG_COUNT=" + strconv.Itoa(len(pinnedSettings)); !slices.Contains(environment, want) {
		t.Errorf("%q is missing: the count git reads must match the list it is derived from", want)
	}

	for index, setting := range pinnedSettings {
		position := strconv.Itoa(index)
		for _, entry := range []string{
			"GIT_CONFIG_KEY_" + position + "=" + setting.key,
			"GIT_CONFIG_VALUE_" + position + "=" + setting.value,
		} {
			if !slices.Contains(environment, entry) {
				t.Errorf("%q is missing from the environment handed to git", entry)
			}
		}
	}
}

// The two settings pinned for a reason, named so that removing one is a
// deliberate act rather than a tidy-up.
//
// TestCommandEnvironmentPinsYagitSettings above checks that whatever is in
// the list reaches git. This checks that these two are in the list at all —
// which is a different question, and the one that matters when somebody is
// shortening a slice they do not have the history for.
func TestTheDangerousTransportStaysRefused(t *testing.T) {
	settings := map[string]string{}
	for _, setting := range pinnedSettings {
		settings[setting.key] = setting.value
	}

	// An `ext::` URL is a command line git runs. Nothing a graphical client
	// does needs it, and a URL arrives here from a text field.
	if settings["protocol.ext.allow"] != "never" {
		t.Error("protocol.ext.allow is not pinned to never: a clone URL can run a command")
	}

	// A user who sets i18n.logOutputEncoding for their terminal would
	// otherwise get JSON of mojibake from the daemon.
	if settings["i18n.logOutputEncoding"] != "UTF-8" {
		t.Error("i18n.logOutputEncoding is not pinned to UTF-8")
	}

	// `file` is deliberately absent: pinning it refuses a submodule added from
	// a local path, which is a supported action. See pinnedSettings.
	if _, pinnedFile := settings["protocol.file.allow"]; pinnedFile {
		t.Error("protocol.file.allow is pinned again: it refuses `git submodule add` from a local path")
	}
}

func TestAnUnsetVariableStaysUnset(t *testing.T) {
	// Passing an absent variable through as an empty string is not the same
	// thing as leaving it absent. git with HOME="" looks for configuration in
	// the filesystem root and finds none, silently, where git with no HOME at
	// all falls back to the password database and finds the user's.
	//
	// GIT_CONFIG_GLOBAL is on every platform's list and is unset on a normal
	// machine, which makes it the case to check.
	// t.Setenv is what registers the restore; os.Unsetenv on its own would
	// leak the removal into every test that runs after this one.
	t.Setenv("GIT_CONFIG_GLOBAL", "placeholder")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}

	for _, entry := range commandEnvironment() {
		if name, _, found := strings.Cut(entry, "="); found && name == "GIT_CONFIG_GLOBAL" {
			t.Fatalf("GIT_CONFIG_GLOBAL is unset in the process, yet git is handed %q", entry)
		}
	}
}

// No git command opens an editor unless it says it accepts what would be in
// one.
//
// The daemon has no terminal: the environment built here carries neither TERM
// nor EDITOR, so git refuses with "Terminal is dumb, but EDITOR unset" rather
// than committing a message nobody read. That refusal is the right default —
// it is loud, and the alternative is silent — and it is why the editor is not
// pinned for every command the way GIT_TERMINAL_PROMPT is.
//
// `git rebase --continue` is the one that needs the other answer, and it asks
// for it: see Command.AcceptsPreparedMessage, and State.Blocked for the
// rebases where accepting would still be a guess.
func TestTheEnvironmentPinsNoEditor(t *testing.T) {
	for _, value := range commandEnvironment() {
		if strings.HasPrefix(value, "GIT_EDITOR=") {
			t.Errorf("GIT_EDITOR is %q for every command; an editor nobody can see must be asked for", value)
		}
	}
}

func TestACommandThatAcceptsAPreparedMessageGetsAnEditorThatOpensNothing(t *testing.T) {
	plain := environmentFor(Command{Args: []string{"status"}})
	if slices.Contains(plain, "GIT_EDITOR=:") {
		t.Error("an ordinary command was handed an editor it never asked for")
	}

	accepting := environmentFor(Command{Args: []string{"rebase", "--continue"}, AcceptsPreparedMessage: true})
	// A colon, and nothing else: git compares the value against this exact
	// string and returns without spawning anything. `true` would be looked up
	// through git's own shell, which is a different program on each platform
	// yagit ships for.
	if !slices.Contains(accepting, "GIT_EDITOR=:") {
		t.Errorf("a command that accepts the prepared message has no editor: %v", accepting)
	}
	for _, value := range accepting {
		if strings.HasPrefix(value, "GIT_EDITOR=") && value != "GIT_EDITOR=:" {
			t.Errorf("GIT_EDITOR is %q; only `:` is handled inside git itself", value)
		}
	}
}

// The sequence editor is never pinned, for any command.
//
// It is what `git rebase --interactive` opens over its todo list — a list of
// operations rather than a message. Accepting one unread would run a plan
// nobody chose, so the day yagit grows an interactive rebase it has to decide
// what that list says rather than inherit a silent yes from here.
func TestTheSequenceEditorIsLeftUnset(t *testing.T) {
	environments := [][]string{
		commandEnvironment(),
		environmentFor(Command{AcceptsPreparedMessage: true}),
	}
	for _, environment := range environments {
		for _, value := range environment {
			if strings.HasPrefix(value, "GIT_SEQUENCE_EDITOR=") {
				t.Errorf("GIT_SEQUENCE_EDITOR is set to %q; a todo list must not be accepted unread", value)
			}
		}
	}
}

// The two streams of a failed command are one explanation, and an empty one
// is left out rather than joined.
//
// A blank line above git's complaint reads as output that went missing, and
// on the ordinary failure — every command that writes nothing to stdout —
// that blank line would be on every error message in the application.
func TestBothStreamsJoinsOnlyWhatGitActuallyWrote(t *testing.T) {
	conflict := bothStreams(
		"Auto-merging notes.md\nCONFLICT (content): Merge conflict in notes.md",
		"error: could not apply e27e201… side change",
	)
	want := "Auto-merging notes.md\nCONFLICT (content): Merge conflict in notes.md\n" +
		"error: could not apply e27e201… side change"
	if conflict != want {
		t.Errorf("bothStreams(…) = %q, want %q", conflict, want)
	}

	if got := bothStreams("", "fatal: not a git repository"); got != "fatal: not a git repository" {
		t.Errorf("with no stdout, bothStreams = %q, want stderr alone", got)
	}
	if got := bothStreams("f.txt: needs merge\n", "  \n"); got != "f.txt: needs merge" {
		t.Errorf("with no stderr, bothStreams = %q, want stdout alone", got)
	}
	if got := bothStreams("", ""); got != "" {
		t.Errorf("with neither stream, bothStreams = %q, want empty", got)
	}
}

// What a failed command takes from its own standard output is bounded.
//
// git explains some refusals on stdout, and Exec joins that onto stderr so the
// user is not shown "(no error output)" — or an account of the failure with
// the filename missing from it. The copy is capped because every byte of it is
// broadcast to every open tab on the event stream and kept in the log panel's
// buffer — a `git rev-list` that died halfway would otherwise put its whole
// answer there.
func TestBorrowedOutputIsCutOnARuneBoundaryAndSaysSo(t *testing.T) {
	if got := beginningOf([]byte("  needs merge\n"), 64); got != "needs merge" {
		t.Errorf("short output came back as %q, want it whole and trimmed", got)
	}

	// Three bytes per rune, and a limit that lands inside one: a cut taken
	// where it was asked for would leave a dangling fragment, which reaches
	// the log panel as a replacement character in the middle of a word.
	long := strings.Repeat("é", 100)
	got := beginningOf([]byte(long), 15)

	if !utf8.ValidString(got) {
		t.Errorf("the cut left invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "(truncated)") {
		t.Errorf("output was cut without saying so: %q", got)
	}
	if len(got) > 15+len("\n… (truncated)") {
		t.Errorf("the cut kept %d bytes for a limit of 15: %q", len(got), got)
	}
}
