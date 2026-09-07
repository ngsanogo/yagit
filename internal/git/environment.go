package git

import (
	"os"
	"runtime"
	"strconv"
)

// The environment git runs in, built from an allowlist rather than inherited.
//
// A stray GIT_DIR, GIT_WORK_TREE or GIT_SSH_COMMAND in the daemon's own
// environment would silently change what every command here means, so nothing
// arrives unless it is named below. Each name was added with the operation
// that made it necessary, and the comment beside it says which — that list is
// the whole content of this file, and it is the reason it is not one line of
// os.Environ().

// environmentFor is the environment one command runs in: what every git
// command gets, and the one thing a command can ask for on top.
//
// The editor is added here rather than pinned in commandEnvironment because
// the justification for accepting a prepared message belongs to the command
// that has one. A daemon with no terminal cannot show an editor, so a git
// command that opens one is a command about to record something nobody read —
// and git's own refusal, "Terminal is dumb, but EDITOR unset", is the right
// answer for every command that has not said otherwise.
//
// A colon, and not `true`. git compares the value against exactly this string
// and returns without spawning anything, so there is no shell, no subprocess,
// and nothing to be missing on a platform — `true` would be looked up through
// git's own `sh`, which is a different program on each of the three yagit
// ships for.
// The sequence editor is added the same way and kept apart from the same
// default: it names a program to run over a todo list, never over a message,
// and a command that sets it has said nothing about what it will accept
// unread. An interactive rebase sets this and not the line above, which is
// what makes a `squash` reaching git a loud failure rather than a silent
// commit — see interactive.go.
func environmentFor(command Command) []string {
	environment := commandEnvironment()

	// A switch rather than two ifs, so the two cannot both be applied: they
	// are opposite answers to one question, and a command that set both would
	// silently get whichever was appended last.
	switch {
	case command.AcceptsPreparedMessage:
		environment = append(environment, "GIT_EDITOR=:")
	case command.MessageEditor != "":
		environment = append(environment, "GIT_EDITOR="+command.MessageEditor)
	}

	if command.SequenceEditor != "" {
		environment = append(environment, "GIT_SEQUENCE_EDITOR="+command.SequenceEditor)
	}
	return environment
}

// commandEnvironment builds the environment of every git command. It is set
// explicitly rather than inherited: git's behavior depends on too many
// environment variables to leave it to chance.
//
// Two halves. The first pins what yagit needs to be true of every git it
// runs. The second passes through, by name, the variables git needs to work
// at all on the host operating system — see inheritedNames.
func commandEnvironment() []string {
	environment := []string{
		// git output in English, unlocalized. yagit shows the raw stderr to
		// the user, and stable output is more useful than a translated one —
		// it is also the wording a search engine has answers for.
		"LC_ALL=C",

		// No interactive prompt. Without this, a network command with no
		// credentials waits for input that never comes and blocks the
		// daemon until the timeout fires.
		"GIT_TERMINAL_PROMPT=0",

		// Read commands do not take .git/index.lock. That keeps yagit from
		// fighting the git the user runs in their own terminal at the same
		// moment.
		"GIT_OPTIONAL_LOCKS=0",

		// No pager: the output is captured, not displayed.
		"GIT_PAGER=cat",
	}

	environment = append(environment, pinnedConfiguration()...)

	for _, name := range inheritedNames(runtime.GOOS) {
		// An unset variable is passed through as unset, not as empty. The two
		// are different to git: no HOME at all makes it fall back to the
		// password database, whereas HOME="" makes it look for configuration
		// in the filesystem root.
		if value, found := os.LookupEnv(name); found {
			environment = append(environment, name+"="+value)
		}
	}

	return environment
}

// pinned is one configuration key forced on every git command yagit runs.
type pinned struct{ key, value string }

// pinnedSettings is the configuration every git command runs under.
//
// Forced through GIT_CONFIG_* rather than through `-c` on the argument list:
// the log panel shows the arguments verbatim, and a user reading it should see
// the command they could type, not eight repetitions of yagit's own hygiene.
//
// The list is the single definition. GIT_CONFIG_COUNT is derived from its
// length in pinnedConfiguration below rather than written beside it, because a
// count and a list that must agree are two definitions of one number — and the
// failure when they drift is silent: git reads the first COUNT pairs and
// ignores the rest, so a setting added at the end of the list would simply
// never apply.
var pinnedSettings = []pinned{
	// The output encoding, pinned rather than inherited.
	//
	// git re-encodes commit messages into i18n.logOutputEncoding on the way
	// out. A user who sets that to ISO-8859-1 — a reasonable thing to want in
	// a terminal — would have the daemon answer JSON full of mojibake, because
	// JSON is UTF-8 and git had already transcoded away from it.
	{"i18n.logOutputEncoding", "UTF-8"},

	// `ext::` is not a transport, it is a command line.
	//
	// A URL of the form `ext::sh -c "…"` tells git to run that shell command
	// and speak the pack protocol over its standard streams. git's own default
	// for it is `user`, which permits it for a clone a person typed — and from
	// git's point of view every clone yagit runs is exactly that, because git
	// cannot tell a daemon acting on a form field from a person at a terminal.
	//
	// yagit can tell. The URL reaches `git clone` from a text input, and a
	// text input that runs arbitrary commands is a text input nobody should
	// have to be careful with: a URL pasted out of an issue, a chat message or
	// a README is a URL nobody read character by character. Nothing a
	// graphical client legitimately does needs this transport, so the cost of
	// refusing it is nothing and the cost of allowing it is the machine.
	//
	// Set for every command rather than for clone alone: a remote's URL is
	// also written into .git/config, where it is read again by every later
	// fetch, and a repository cloned elsewhere carries whatever its config
	// says.
	{"protocol.ext.allow", "never"},

	// `protocol.file.allow` is deliberately NOT pinned here, and the reason is
	// worth writing down because pinning it looks like the same good idea.
	//
	// It is the transport a submodule uses when its URL is a path on this
	// disk, and yagit supports adding one — the integration suite adds a
	// submodule from a local path, which is how this was found rather than
	// shipped. Narrowing it to `user` refuses that with "transport 'file' not
	// allowed", turning a working feature into an error message about a
	// setting the user never chose.
	//
	// What that would have bought is narrower than it looks: git already
	// refuses `file` for the clones it makes on a repository's behalf rather
	// than a person's. What is left is a submodule path somebody typed, which
	// is the case being supported.
}

// pinnedConfiguration renders pinnedSettings as the GIT_CONFIG_* triples git
// reads, with the count derived from the list.
func pinnedConfiguration() []string {
	rendered := make([]string, 0, 1+2*len(pinnedSettings))
	rendered = append(rendered, "GIT_CONFIG_COUNT="+strconv.Itoa(len(pinnedSettings)))
	for index, setting := range pinnedSettings {
		position := strconv.Itoa(index)
		rendered = append(rendered,
			"GIT_CONFIG_KEY_"+position+"="+setting.key,
			"GIT_CONFIG_VALUE_"+position+"="+setting.value,
		)
	}
	return rendered
}

// inheritedNames lists the variables git is allowed to inherit, for one
// operating system. Taking GOOS as an argument rather than reading it makes
// every platform's list readable — and testable — from any machine.
//
// The list is an allowlist and stays one. Inheriting the whole environment
// would hand git several dozen GIT_* switches yagit never chose, and would
// hand a network command the user's SSH and GPG agents without anyone having
// decided that. Those get added one at a time, by name, each with the reason
// it is there.
func inheritedNames(goos string) []string {
	shared := []string{
		// PATH is how git finds its own subcommands, and how it finds the
		// credential and diff helpers the user configured.
		"PATH",

		// Where the user keeps their configuration when they keep it under
		// XDG. git reads ~/.config/git/config from here, and a machine that
		// sets this variable has usually put the whole of ~/.gitconfig there —
		// identity, signing key, credential helper, insteadOf rewrites. HOME
		// alone does not find it, and a daemon that dropped it would run git
		// with somebody else's configuration on the one machine where it is
		// most carefully arranged.
		"XDG_CONFIG_HOME",

		// The proxy, in the four spellings the world settled on.
		//
		// git reads http.proxy from its configuration first, and falls back to
		// these; behind a corporate proxy they are frequently the only thing
		// that is set, because they are what every other tool on the machine
		// reads. Without them a fetch does not fail quickly — it hangs until
		// the deadline, having tried to open a connection nothing will answer,
		// and the error blames a network that is working.
		//
		// NO_PROXY is on the list for the opposite reason and matters just as
		// much: it is what keeps an internal host from being sent through a
		// proxy that cannot reach it.
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
		"NO_PROXY",

		// The same four in lower case. curl reads both spellings and so does
		// most of the ecosystem; passing one and not the other would make the
		// daemon work on one machine and hang on the next for a reason nobody
		// could see. Windows environments are case-insensitive, so these are
		// the same names there and resolve to the same values.
		"http_proxy",
		"https_proxy",
		"all_proxy",
		"no_proxy",

		// How the user pins the ssh they want git to use — a different binary,
		// a key chosen with -i, a jump host. git's own switch, in the same
		// family as GIT_CONFIG_GLOBAL below: yagit never sets it, and it must
		// not swallow it either, because on a machine that needs it every
		// fetch over ssh fails without it.
		"GIT_SSH_COMMAND",
		"GIT_SSH",
	}

	if goos != "windows" {
		return append(shared,
			// HOME is what lets git find the user's ~/.gitconfig — their
			// identity, their signing key, their aliases. yagit runs as the
			// user and must behave like the git in their own terminal.
			"HOME",

			// git writes temporary files during a merge, a patch application
			// or a checkout. Where TMPDIR points at something other than
			// /tmp — a sandbox, a machine whose /tmp is not writable —
			// dropping it turns those operations into failures nothing else
			// explains.
			"TMPDIR",

			// git's own switches for pinning which configuration files it
			// reads.
			//
			// yagit never sets them: honouring the user's configuration is
			// the point of passing HOME above. But it must not swallow them
			// either. They are the only way to hand git a known
			// configuration, and without that a test run on a machine whose
			// owner signs every commit produces signed commits — passing or
			// failing depending on whose machine it is.
			"GIT_CONFIG_GLOBAL",
			"GIT_CONFIG_SYSTEM",

			// The two that commit signing needs, added the day commits
			// arrived and not before — the rule for this list is one name at
			// a time, each with the operation that made it necessary.
			//
			// A user with commit.gpgsign set has decided that an unsigned
			// commit is not acceptable. Without GNUPGHOME, gpg looks under
			// $HOME/.gnupg, which is right for most people and wrong for
			// anyone who moved it; the commit then fails with an error about
			// a missing secret key, and the cause is a variable yagit
			// dropped.
			"GNUPGHOME",

			// gpg.format=ssh signs through the SSH agent, which is reachable
			// only through this socket. It is also what the network commands
			// authenticate with, which is the second reason it is here and was
			// the reason it was written down before there were any.
			"SSH_AUTH_SOCK",

			// How the credential helpers that keep a password in the desktop
			// keyring are reached: git-credential-libsecret talks to the
			// keyring over the session bus, and finds the bus through this.
			// Without it the helper answers nothing, git falls back to asking
			// for a password, GIT_TERMINAL_PROMPT=0 refuses to ask — and a
			// push fails with an authentication error on a machine whose
			// credentials are stored and working.
			"DBUS_SESSION_BUS_ADDRESS",
		)
	}

	// Windows needs a longer list, and every entry on it is load-bearing.
	//
	// A Windows process started with a hand-built environment is not a
	// process with fewer conveniences: it is a process where the C runtime
	// and Winsock cannot find themselves. SystemRoot alone decides whether
	// `git fetch` can resolve a hostname. The rule of this project is that
	// nothing fails silently, and a truncated environment here fails in ways
	// whose error messages point nowhere near the cause.
	return append(shared,
		// Winsock loads its provider catalog from under SystemRoot. Without
		// it every network command fails at name resolution, with an error
		// that blames the network.
		"SystemRoot",
		"SystemDrive",
		"windir",

		// Go's own exec resolution and Windows' CreateProcess use PATHEXT to
		// decide what counts as executable. Credential helpers ship as .cmd
		// and .bat far more often than as .exe.
		"PATHEXT",

		// cmd.exe, which git for Windows spawns for several helpers.
		"COMSPEC",

		// How git for Windows finds the user's home, in the order it tries
		// them: HOME first, then HOMEDRIVE + HOMEPATH, then USERPROFILE.
		// All four, or the fallback chain has a hole in it.
		"HOME",
		"HOMEDRIVE",
		"HOMEPATH",
		"USERPROFILE",

		// Git Credential Manager keeps its store under LOCALAPPDATA, and
		// several tools read APPDATA.
		"APPDATA",
		"LOCALAPPDATA",

		// The system-wide git configuration lives at
		// %ProgramData%\Git\config; helpers are looked up under the Program
		// Files variables, which differ between the 32- and 64-bit views.
		"ProgramData",
		"ProgramFiles",
		"ProgramFiles(x86)",
		"ProgramW6432",

		// Temporary files, as TMPDIR above.
		"TEMP",
		"TMP",

		// As on the other platforms.
		"GIT_CONFIG_GLOBAL",
		"GIT_CONFIG_SYSTEM",

		// Commit signing, as on the other platforms. SSH_AUTH_SOCK is on the
		// list for Windows too: OpenSSH's agent there speaks over a named
		// pipe, and the variable is how a client is pointed at a different
		// one — Git for Windows and 1Password both use it.
		"GNUPGHOME",
		"SSH_AUTH_SOCK",
	)
}
