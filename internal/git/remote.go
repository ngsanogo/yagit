package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The three commands that leave the machine, and the remotes they leave for.
//
// Everything else in this package reads or writes a repository on this disk
// and answers in milliseconds. These wait on somebody else's server, which is
// why they carry networkTimeout rather than the Runner's own deadline, and why
// they are the only commands here whose failure is routinely not the user's
// fault.
//
// yagit authenticates nothing. git has a credential subsystem, an ssh agent
// and the user's own configuration behind it, and every one of those is
// reached by passing the environment through rather than by asking for a
// password yagit would then have to hold. A daemon that collected credentials
// would be a daemon that stores them, and it has no better place to put them
// than the keyring git is already using. See docs/adr/0020.
//
// The rule from staging.go holds here too: a name reaches git where git reads
// a name. `git push` and `git fetch` both take their repository after `--`,
// and a remote may be called `-x` — git accepts that name at `git remote add`,
// so refusing to think about it later is how `git fetch -x` becomes an
// unknown-option error about a request nobody made.

// Remote is one configured remote, as `git remote --verbose` lists it.
type Remote struct {
	Name string `json:"name"`

	// FetchURL and PushURL are where git goes to read and to write. They are
	// usually the same string and are two fields because remote.<name>.pushurl
	// exists: a fork read over https and written over ssh is an ordinary
	// arrangement, and a single URL would show one half of it.
	//
	// Both are REDACTED — see RedactURL. Nothing in this package returns a URL
	// that can be handed back to git, which is deliberate: the only caller is
	// a list on a screen, and a token pasted into a remote URL is a token that
	// would otherwise be drawn on it.
	FetchURL string `json:"fetch_url"`
	PushURL  string `json:"push_url"`
}

// ErrNoRemote: an operation that needs a remote was not given one.
//
// Refused here rather than handed to git, whose answer to `git push` with no
// argument depends on the user's push.default and on which branch they happen
// to be standing on — a different operation from the one the button named.
var ErrNoRemote = errors.New("no remote given")

// ErrNoUpstream: the branch tracks nothing, so there is nowhere to pull from.
//
// A state, not a failure: a branch created locally and never pushed has no
// upstream, and the interface offers to publish it rather than reporting an
// error. It is named so that the interface can tell that case apart from a
// network that refused.
var ErrNoUpstream = errors.New("this branch tracks no upstream branch")

// ErrNoCommits: the repository has no commit, so no branch exists to send.
//
// Told apart from ErrDetachedHEAD below because they are two different states
// wearing one shape — ReadHEAD answers an empty repository with no name at all
// — and because only one of them is fixed by making a commit.
var ErrNoCommits = errors.New("this repository has no commit yet")

// ErrDetachedHEAD: there is no branch to pull into or push from.
//
// Neither operation has a meaning here. Pulling would merge into a commit
// nothing points at, and pushing would have to invent a name for a branch that
// does not exist; git says as much for the first and quietly does something
// surprising for the second, depending on configuration.
var ErrDetachedHEAD = errors.New("HEAD is detached, so no branch is being tracked")

// Remotes lists what the repository is configured to talk to.
func (r *Runner) Remotes(ctx context.Context, dir string) ([]Remote, error) {
	// `git remote --verbose` rather than reading remote.*.url out of the
	// configuration: git is the authority on which remotes it considers
	// configured, and the config keys alone cannot say — a `remote.x.fetch`
	// with no URL is a half-written remote git does not list, and a remote
	// name containing a dot makes the key ambiguous to read back.
	output, err := r.Run(ctx, dir, "remote", "--verbose")
	if err != nil {
		return nil, err
	}
	return ParseRemotes(output)
}

// ParseRemotes turns `git remote --verbose` into remotes. Pure function,
// testable without git.
//
// The output is two lines per remote — one for fetch, one for push — each
// being a name, a tab, a URL, a space and the direction in brackets. Both
// lines are read: the pair differs whenever remote.<name>.pushurl is set,
// which is exactly the arrangement worth showing.
func ParseRemotes(output []byte) ([]Remote, error) {
	var remotes []Remote
	// Where each name landed, so the second line of a pair finds the first
	// rather than appending a duplicate. An index rather than a map of structs
	// because the order git printed them in is the order to show them in:
	// git sorts by name, and a list that re-sorted itself would be a second
	// opinion about something already decided.
	position := make(map[string]int)

	for index, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}

		name, url, direction, err := parseRemoteLine(line)
		if err != nil {
			return nil, fmt.Errorf("git remote --verbose line %d: %w", index+1, err)
		}

		at, known := position[name]
		if !known {
			at = len(remotes)
			position[name] = at
			remotes = append(remotes, Remote{Name: name})
		}

		if direction == "push" {
			remotes[at].PushURL = url
		} else {
			remotes[at].FetchURL = url
		}
	}

	return remotes, nil
}

func parseRemoteLine(line string) (name, url, direction string, err error) {
	name, rest, found := strings.Cut(line, "\t")
	if !found {
		return "", "", "", fmt.Errorf("no tab between the name and the URL in %q", line)
	}

	// The direction is the last field, in brackets, and the URL is everything
	// before it — cut from the right, because a URL may contain a space and a
	// remote's name may not.
	end := strings.LastIndex(rest, " (")
	if end < 0 || !strings.HasSuffix(rest, ")") {
		return "", "", "", fmt.Errorf("no (fetch) or (push) after the URL in %q", line)
	}

	direction = rest[end+2 : len(rest)-1]
	if direction != "fetch" && direction != "push" {
		// Refused rather than guessed. git prints one of two words here, and a
		// third one means this output is not what this function thinks it is —
		// which is worth an error naming the line, not a remote silently
		// filed under the wrong direction.
		return "", "", "", fmt.Errorf("direction %q is neither fetch nor push, in %q", direction, line)
	}

	return name, RedactURL(rest[:end]), direction, nil
}

// Editing the remote list: add, rename, remove.
//
// Configuration only — none of these leave the machine. They sit here rather
// than beside the network commands below because they are the other half of
// the same subject: what the repository is configured to talk to, and how
// that list is changed. Fetch, pull and push then use the names this writes.

// ErrNoRemoteURL: an add was asked for with nowhere to point.
//
// Refused here rather than handed to git with an empty URL, which either
// fails with a sentence about the usage or records a remote that cannot be
// fetched — a success that looks like the dialog was skipped.
var ErrNoRemoteURL = errors.New("no remote URL given")

// AddRemoteArgs is the command AddRemote runs.
//
// Exported for the reason DeleteBranchArgs is: a confirmation that showed a
// different line from the one git received would be a lie. The name and URL
// both go after `--`, because a remote may be called `-f` and git accepts
// that name at `git remote add`.
func AddRemoteArgs(name, url string) []string {
	return []string{"remote", "add", "--", name, url}
}

// RenameRemoteArgs is the command RenameRemote runs.
//
// git moves the remote-tracking branches under refs/remotes/ with the name,
// and rewrites branch.*.remote that pointed at the old one. That is the whole
// of the operation, and it is why a rename is worth offering rather than
// remove-then-add.
func RenameRemoteArgs(from, to string) []string {
	return []string{"remote", "rename", "--", from, to}
}

// RemoveRemoteArgs is the command RemoveRemote runs.
//
// `remove` rather than `rm`: both work, and the longer form is the one a
// confirmation can show without looking like an abbreviation the user did not
// ask for. The remote-tracking branches under refs/remotes/<name>/ go with it.
func RemoveRemoteArgs(name string) []string {
	return []string{"remote", "remove", "--", name}
}

// SetRemoteURLArgs is the command SetRemoteURL runs.
//
// One URL, the fetch one. A separately configured pushurl is left alone —
// `git remote set-url` without `--push` is what changes where the remote is
// read from, and a form that silently rewrote both would erase an https/ssh
// split somebody set on purpose. The confirmation shows this line alone.
func SetRemoteURLArgs(name, url string) []string {
	return []string{"remote", "set-url", "--", name, url}
}

// AddRemote records a remote by name and URL.
//
// The URL is whatever the user typed — a path on this disk, an https URL, an
// ssh one. Nothing here redacts it on the way in: redaction is for what
// leaves this package toward a screen, and a URL that had its password
// stripped before git saw it would be a remote that cannot authenticate.
func (r *Runner) AddRemote(ctx context.Context, dir, name, url string) error {
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" {
		return ErrNoRemote
	}
	if url == "" {
		return ErrNoRemoteURL
	}
	_, err := r.Run(ctx, dir, AddRemoteArgs(name, url)...)
	return err
}

// RenameRemote changes a remote's name.
//
// The remote HEAD is following moves with it when branch.<name>.remote names
// the old one — git's behaviour, and what renaming origin to upstream is for.
func (r *Runner) RenameRemote(ctx context.Context, dir, from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return ErrNoRemote
	}
	_, err := r.Run(ctx, dir, RenameRemoteArgs(from, to)...)
	return err
}

// RemoveRemote forgets a remote and the remote-tracking branches under it.
func (r *Runner) RemoveRemote(ctx context.Context, dir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoRemote
	}
	_, err := r.Run(ctx, dir, RemoveRemoteArgs(name)...)
	return err
}

// SetRemoteURL changes where a remote is fetched from.
//
// The URL is whatever the user typed — same rule as AddRemote. It is never
// taken from a redacted list: those strings are for screens, and handing one
// back to git would write `***` into the configuration.
func (r *Runner) SetRemoteURL(ctx context.Context, dir, name, url string) error {
	name, url = strings.TrimSpace(name), strings.TrimSpace(url)
	if name == "" {
		return ErrNoRemote
	}
	if url == "" {
		return ErrNoRemoteURL
	}
	_, err := r.Run(ctx, dir, SetRemoteURLArgs(name, url)...)
	return err
}

// RedactURL removes the credentials some remote URLs carry.
//
// Exported for the routes that show a command holding one before it runs — a
// submodule add is the case — so the line on the confirmation and the line in
// the remote list hide the same thing in the same way.
//
// `https://ada:ghp_xxx@github.com/ada/yagit.git` is a working remote and a
// common one — it is what several tools write when they store a token — and
// the token in the middle of it is a password. This list is drawn on a screen
// and travels through the JSON of a route anything on the loopback could read,
// so the secret is removed at the boundary rather than at the point of
// display: one function, and no way for a later caller to forget it.
//
// What is kept is the shape, because the shape is what the list is for: the
// host, the path, and whether a name is attached at all.
//
// The two cases differ for a reason that is not stylistic. A userinfo with a
// colon is a name and a password, and the name is worth showing — it answers
// "which account is this pushing as". A userinfo without one is either a plain
// username or a token used as one, and nothing in the string says which; the
// whole of it goes, because guessing wrong in that direction prints a
// credential.
func RedactURL(raw string) string {
	scheme, rest, found := strings.Cut(raw, "://")
	if !found {
		// The scp-like form, `git@github.com:ada/yagit.git`, which has no
		// place to put a password: everything before the colon is a host and a
		// user name. Left exactly as git printed it.
		return raw
	}

	authority, path, hasPath := strings.Cut(rest, "/")

	// LastIndex, not Index: a host cannot contain an at sign, and a password
	// git accepts may — so the last one is the separator every time.
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return raw
	}

	hidden := "***"
	if user, _, split := strings.Cut(authority[:at], ":"); split {
		hidden = user + ":***"
	}

	redacted := scheme + "://" + hidden + authority[at:]
	if hasPath {
		redacted += "/" + path
	}
	return redacted
}

// FetchArgs is the command Fetch runs. Exported for the reason
// DeleteBranchArgs is: the line the user is shown and the line git receives
// have one definition between them.
//
// `--prune` is not optional, and that is the decision rather than an omission.
// A remote-tracking branch whose branch was deleted on the server is a row in
// the sidebar naming something that no longer exists, offered for checkout and
// counted in the graph; keeping it is not caution, it is a picture of the
// remote that is wrong. Nothing is lost that a fetch does not put back, which
// is what makes the choice a fair one to take on the user's behalf.
//
// `--progress` is unconditional for the same reason `clone` carries it: the
// counters ride the request that started the fetch (ADR 0030), and a fetch
// without them is a busy button over a silent network.
//
// An empty remote means every one of them. That is what a fetch button with no
// remote picker beside it has to mean — the alternative is choosing one for
// the user and calling it "fetch".
func FetchArgs(remote string) []string {
	if remote == "" {
		return []string{"fetch", "--all", "--prune", "--progress"}
	}
	return []string{"fetch", "--prune", "--progress", "--", remote}
}

// Fetch brings remote-tracking branches up to date, and moves nothing else.
//
// The one network operation that cannot surprise anybody: it writes under
// refs/remotes, never touches the work tree, and leaves every local branch
// where it was. That is why it is the button offered first — it is how you
// find out what has happened elsewhere before deciding what to do about it.
//
// onProgress receives each stderr segment as git writes it; nil is fine when
// nobody is watching.
func (r *Runner) Fetch(ctx context.Context, dir, remote string, onProgress func(line string)) error {
	_, err := r.Exec(ctx, Command{
		Dir:         dir,
		Args:        FetchArgs(remote),
		IdleTimeout: networkIdle,
		OnProgress:  onProgress,
	})
	return err
}

// PullStrategy is what to do with the commits a pull brings back.
//
// git's own answer to that question is a setting — pull.rebase, and a warning
// when it is unset — because the right one depends on the repository and on
// the team. A button cannot read a setting and still say what it does, so the
// three are three operations here, named on screen, and the daemon passes the
// matching flag every time rather than letting configuration decide what the
// word "pull" meant this afternoon.
type PullStrategy string

const (
	// PullFastForward moves the branch up to the upstream, or refuses.
	//
	// The default, and the only one of the three that cannot produce a commit
	// nobody asked for or a conflict in the middle of somebody's afternoon.
	// When the two have genuinely diverged git says so and stops, which is the
	// moment the other two become a question worth putting to the user.
	PullFastForward PullStrategy = "ff-only"

	// PullMerge brings the upstream in as a merge commit.
	PullMerge PullStrategy = "merge"

	// PullRebase replays the local commits on top of the upstream.
	PullRebase PullStrategy = "rebase"
)

// ParsePullStrategy reads a strategy sent by a client.
//
// An unknown one is refused rather than read as the default, for the reason a
// nonsense scope is (docs/adr/0016): answering with an operation that looks
// entirely correct to a client that asked for a different one hides the defect
// for good — and here the two differ by a merge commit.
func ParsePullStrategy(raw string) (PullStrategy, error) {
	switch PullStrategy(raw) {
	case PullFastForward, PullMerge, PullRebase:
		return PullStrategy(raw), nil
	case "":
		return "", fmt.Errorf("%w: ff-only, merge or rebase", errUnknownStrategy)
	default:
		return "", fmt.Errorf("%w: %q is none of ff-only, merge, rebase", errUnknownStrategy, raw)
	}
}

var errUnknownStrategy = errors.New("no pull strategy given")

// PullArgs is the command Pull runs.
//
// The remote and the branch are named rather than left out. `git pull` with no
// arguments reads the branch's configuration and usually does the same thing —
// usually, and the exceptions are the ones that matter: a branch whose
// upstream was set to something else, a repository where push.default and
// branch.<name>.merge disagree. Naming both makes the line in the log panel
// the whole of the operation, with nothing read from a file to complete it.
//
// The strategy is always passed, including the one that matches git's default.
// Leaving it out would let pull.rebase in the user's configuration decide, and
// the button on screen said which of the three this was.
func PullArgs(remote, branch string, strategy PullStrategy) []string {
	args := []string{"pull", "--progress"}
	switch strategy {
	case PullRebase:
		args = append(args, "--rebase")
	case PullMerge:
		// --no-rebase rather than nothing: nothing means "whatever pull.rebase
		// says", which is the one answer this function exists to rule out.
		args = append(args, "--no-rebase")
	case PullFastForward:
		args = append(args, "--ff-only")
	}
	return append(args, "--", remote, branch)
}

// Pull fetches the upstream and integrates it into the current branch.
//
// Conflicts are not this function's business and are not an error it hides:
// git stops, writes the markers into the work tree and exits non-zero, which
// arrives as an *Error carrying git's own account. What the repository is then
// in the middle of is read by ReadState, drawn as a banner, and resolved on
// the screen that already exists for it.
//
// onProgress receives each stderr segment as git writes it; nil is fine when
// nobody is watching.
func (r *Runner) Pull(
	ctx context.Context, dir string, from Upstream, strategy PullStrategy, onProgress func(line string),
) error {
	if from.Remote == "" {
		return ErrNoRemote
	}
	if from.Branch() == "" {
		return ErrNoUpstream
	}
	if _, err := ParsePullStrategy(string(strategy)); err != nil {
		return err
	}

	_, err := r.Exec(ctx, Command{
		Dir:         dir,
		Args:        PullArgs(from.Remote, from.Branch(), strategy),
		IdleTimeout: networkIdle,
		OnProgress:  onProgress,
	})
	return err
}

// Upstream is the branch on another machine that a local branch follows.
type Upstream struct {
	// Remote is the name it is reached through: "origin".
	Remote string `json:"remote"`

	// Ref is the branch's full name on the other side: "refs/heads/trunk".
	//
	// Full rather than short, because that is what git answers and because the
	// short form is ambiguous on the way back — `refs/heads/x` and
	// `refs/tags/x` shorten to the same string, and a push destination that
	// resolved to the second would be a push to a tag.
	Ref string `json:"ref"`
}

// Branch is the short name on the other side, or empty for no upstream.
func (u Upstream) Branch() string { return strings.TrimPrefix(u.Ref, headsPrefix) }

// Configured says the branch follows something.
func (u Upstream) Configured() bool { return u.Remote != "" && u.Ref != "" }

// upstreamFormat asks for the two halves separately and lets git split them.
//
// The alternative is to take %(upstream) — "refs/remotes/origin/trunk" — and
// cut it at a slash, which cannot be done correctly outside git: a remote may
// be called "origin/mirror" and a branch may be called "feat/lanes", so the
// string has several readings and only git's configuration says which is the
// right one.
const upstreamFormat = "%(upstream:remotename)%00%(upstream:remoteref)"

// UpstreamOf reads what a local branch follows.
//
// A branch with no upstream, and a name no branch has, both answer the zero
// Upstream with no error: neither is a failure, and Configured is what tells a
// caller it has an answer. The second case is real rather than defensive — an
// unborn branch is what `git init` leaves, and it is a name with no ref.
func (r *Runner) UpstreamOf(ctx context.Context, dir, branch string) (Upstream, error) {
	if branch == "" {
		return Upstream{}, ErrDetachedHEAD
	}

	// No `--` before the pattern, and none is needed: LocalBranchRef builds it
	// onto "refs/heads/", so whatever the branch is called the argument cannot
	// begin with a dash. That is the same protection the separator gives, made
	// by construction rather than by an option.
	output, err := r.Run(ctx, dir, "for-each-ref", "--format="+upstreamFormat, LocalBranchRef(branch))
	if err != nil {
		return Upstream{}, err
	}

	line := strings.TrimRight(string(output), "\n")
	if line == "" {
		return Upstream{}, nil
	}

	remote, ref, found := strings.Cut(line, fieldSeparator)
	if !found {
		return Upstream{}, fmt.Errorf(
			"git for-each-ref answered %q about %s, two NUL-separated fields expected", line, branch)
	}
	return Upstream{Remote: remote, Ref: ref}, nil
}

// PushDestination is where a branch is being sent.
type PushDestination struct {
	// Remote is the name git is given: "origin".
	Remote string

	// Branch is the local branch being pushed, short.
	Branch string

	// Ref is the full name it lands under on the other side. It is the
	// upstream's when the branch has one — a branch called `main` that follows
	// `origin/trunk` pushes to trunk, and `git push origin main` would instead
	// create a second branch on the server called main.
	Ref string

	// SetUpstream records the destination, so the branch follows it afterwards
	// and every later push and pull knows where to go. True exactly when the
	// branch had no upstream: this is publishing it.
	SetUpstream bool
}

// PushArgs is the command Push runs. Exported for the reason FetchArgs is: a
// force push is shown before it runs, and the line shown has to be the line
// that runs.
//
// The refspec is written out — `main:refs/heads/trunk` — rather than left to
// git. `git push origin main` looks like the same request and is not: it sends
// the branch to `refs/heads/main` on the other side, so a branch that follows
// `origin/trunk` would quietly create a second branch on the server and push
// to that one forever after.
//
// The force flags are two and never `--force`. `--force-with-lease` refuses
// unless the remote is where this repository last saw it, which turns "I have
// rewritten history" into "I have rewritten history, and nobody has pushed
// since". `--force-if-includes` closes the hole the first one still leaves:
// the lease is held against the remote-tracking ref, and a fetch — a button
// away, or a colleague's — moves that ref without anybody having looked at
// what arrived. It requires that whatever the remote had is actually in this
// branch's history, which is the thing the user believes when they force.
func PushArgs(to PushDestination, force bool) []string {
	args := []string{"push", "--progress"}
	if to.SetUpstream {
		args = append(args, "--set-upstream")
	}
	if force {
		args = append(args, "--force-with-lease", "--force-if-includes")
	}
	return append(args, "--", to.Remote, to.Branch+":"+to.Ref)
}

// Push sends a branch to a remote.
//
// onProgress receives each stderr segment as git writes it; nil is fine when
// nobody is watching.
func (r *Runner) Push(
	ctx context.Context, dir string, to PushDestination, force bool, onProgress func(line string),
) error {
	switch {
	case to.Remote == "":
		return ErrNoRemote
	case to.Branch == "":
		return ErrDetachedHEAD
	case to.Ref == "":
		return ErrNoUpstream
	}

	_, err := r.Exec(ctx, Command{
		Dir:         dir,
		Args:        PushArgs(to, force),
		IdleTimeout: networkIdle,
		OnProgress:  onProgress,
	})
	return err
}

// DestinationFor works out where a branch pushes.
//
// Two cases, and they are two operations wearing one word. A branch with an
// upstream is being pushed to it, whatever the remote the caller suggested —
// the follow is the answer, and second-guessing it is how a repository ends up
// with two branches for one line of work. A branch without one is being
// PUBLISHED: it needs a remote chosen by somebody, it lands under its own
// name, and the choice is recorded so that nobody has to make it twice.
func DestinationFor(branch string, upstream Upstream, remote string) (PushDestination, error) {
	if branch == "" {
		return PushDestination{}, ErrDetachedHEAD
	}

	if upstream.Configured() {
		return PushDestination{Remote: upstream.Remote, Branch: branch, Ref: upstream.Ref}, nil
	}

	if remote == "" {
		return PushDestination{}, fmt.Errorf(
			"%w: %s follows nothing, so publishing it needs a remote named", ErrNoRemote, branch)
	}

	return PushDestination{
		Remote:      remote,
		Branch:      branch,
		Ref:         LocalBranchRef(branch),
		SetUpstream: true,
	}, nil
}

// SetUpstreamArgs is the command SetUpstream runs.
//
// The upstream is named as remote/branch — git's own form for
// `--set-upstream-to`. The local branch goes after `--` so a name that looks
// like a flag cannot become one.
func SetUpstreamArgs(branch, remote, upstreamBranch string) []string {
	return []string{
		"branch",
		"--set-upstream-to=" + remote + "/" + upstreamBranch,
		"--",
		branch,
	}
}

// UnsetUpstreamArgs is the command UnsetUpstream runs.
func UnsetUpstreamArgs(branch string) []string {
	return []string{"branch", "--unset-upstream", "--", branch}
}

// SetUpstream records that a local branch follows a remote-tracking one.
//
// Distinct from publishing: a publish pushes and then records; this only
// records. Useful when the remote branch already exists and the local one
// should follow it without sending anything.
func (r *Runner) SetUpstream(ctx context.Context, dir, branch, remote, upstreamBranch string) error {
	branch = strings.TrimSpace(branch)
	remote = strings.TrimSpace(remote)
	upstreamBranch = strings.TrimSpace(upstreamBranch)
	if branch == "" {
		return ErrDetachedHEAD
	}
	if remote == "" {
		return ErrNoRemote
	}
	if upstreamBranch == "" {
		return ErrNoUpstream
	}
	_, err := r.Run(ctx, dir, SetUpstreamArgs(branch, remote, upstreamBranch)...)
	return err
}

// UnsetUpstream forgets what a local branch follows.
//
// Afterwards every push and pull needs a destination again — the same state a
// freshly created local branch is in.
func (r *Runner) UnsetUpstream(ctx context.Context, dir, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ErrDetachedHEAD
	}
	_, err := r.Run(ctx, dir, UnsetUpstreamArgs(branch)...)
	return err
}
