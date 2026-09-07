// Package repo keeps the state of the repositories the user has explicitly
// opened.
//
// It carries the daemon's security boundary. The client only ever handles
// opaque identifiers, never paths: an unknown identifier grants access to
// nothing. The single place where a path coming from the network is accepted
// is Open, which checks it against the allowed root before letting it through.
//
// Depends on git, and on nothing else.
package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

// Errors the caller tells apart to pick an HTTP status code. They are
// compared with errors.Is, never by their text.
var (
	// ErrPathNotAbsolute: a relative path would be resolved against the
	// daemon's working directory, which makes no sense here.
	ErrPathNotAbsolute = errors.New("path is not absolute")

	// ErrOutsideRoot: the guard that keeps yagit from reading anywhere on
	// disk at the request of a web page.
	ErrOutsideRoot = errors.New("path outside the allowed root")

	// ErrUnknownRepo: an identifier that designates no open repository.
	ErrUnknownRepo = errors.New("unknown repository")

	// ErrRepoReplaced: the identifier still resolves, but not to the
	// directory it was opened on. Something swapped it out underneath.
	ErrRepoReplaced = errors.New("repository replaced since it was opened")

	// ErrDestinationExists: the place a new repository would go is already on
	// disk. Shared by clone and init, which differ in what fills the directory
	// and not at all in what has to be true of it first.
	//
	// git would refuse a non-empty directory itself; refusing any existing
	// path here keeps the message ours — and covers the empty-directory case
	// git would otherwise fill, which is not what "clone into this path" meant.
	ErrDestinationExists = errors.New("destination already exists")
)

// Repo is an open repository.
type Repo struct {
	ID string `json:"id"`

	// Path is where git commands run: the work tree, or the git directory
	// itself for a bare repository, which has no work tree.
	Path string `json:"path"`

	Name string `json:"name"`

	// Bare says the repository has no work tree. The interface has no working
	// directory to show for one, and no file to stage.
	Bare bool `json:"bare"`

	OpenedAt time.Time `json:"opened_at"`

	// gitDir is where the objects, the refs and the config actually live.
	//
	// It is not Path, and the difference is the whole point. A directory whose
	// `.git` is a FILE reading `gitdir: /somewhere/else` has its work tree
	// inside the root and its history outside it; a linked worktree borrows
	// the main repository's git directory. The boundary has to hold on this
	// path, not on the work tree.
	gitDir string

	// worktreeGitDir is where THIS work tree's own state lives, which for a
	// linked worktree is `<gitDir>/worktrees/<name>` and for everything else
	// is gitDir itself.
	//
	// The split is git's, not yagit's: refs, objects and config are shared,
	// while HEAD, the index, ORIG_HEAD, MERGE_HEAD and a rebase's state
	// belong to one work tree. Both halves are needed to know what a
	// repository is doing.
	worktreeGitDir string

	// identity is what gitDir was at open time, so a later request can tell
	// that directory apart from one that took its place.
	identity os.FileInfo

	// pinned holds gitDir open for as long as this entry lives, which is what
	// keeps identity above meaning the same thing over time. It is nil on
	// Windows, where holding it would be both unnecessary and harmful.
	//
	// Both halves of that are explained where they are decided, in pin_unix.go
	// and pin_windows.go, because the reason differs by platform and a single
	// comment here could only be right about one of them.
	pinned *os.File
}

// GitDirs is every directory git writes this repository's state into: the
// common one, and this work tree's own where they differ.
//
// Exposed for one caller: the filesystem watch, which has to follow the
// directories git writes into and not the work tree beside them. Two of them
// for a linked worktree, and both are load-bearing — a commit writes a ref in
// the common directory, while `git switch` and `git rebase` touch only the
// per-worktree one. Watching either alone is silent for half of what happens.
//
// Neither is Path, and callers that run git commands want Path. They are the
// same only for an ordinary repository, which is exactly what makes the
// mistake easy to make and hard to see.
func (r *Repo) GitDirs() []string {
	if r.worktreeGitDir == "" || r.worktreeGitDir == r.gitDir {
		return []string{r.gitDir}
	}
	return []string{r.gitDir, r.worktreeGitDir}
}

// StateDir is where git records what THIS work tree is in the middle of:
// MERGE_HEAD, MERGE_MSG, CHERRY_PICK_HEAD, a rebase's directory.
//
// Deliberately not GitDirs()[0]. That one is the COMMON directory, which holds
// the refs and the objects and none of this — so a linked worktree asking it
// what it was doing would be told about a merge somebody else started, in
// another checkout, and offered that merge's message to commit.
func (r *Repo) StateDir() string {
	if r.worktreeGitDir == "" {
		return r.gitDir
	}
	return r.worktreeGitDir
}

// Registry is the list of open repositories. Safe under concurrent access:
// the daemon serves several requests at a time.
type Registry struct {
	mutex  sync.RWMutex
	byID   map[string]*Repo
	order  []string
	root   string
	runner *git.Runner
}

// NewRegistry builds the registry for a given root. The root is canonicalized
// once here, so that every later comparison happens between resolved paths.
func NewRegistry(root string, runner *git.Runner) (*Registry, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("repository root %q: %w", root, ErrPathNotAbsolute)
	}

	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("repository root %q not found: %w", root, err)
	}

	return &Registry{
		byID:   make(map[string]*Repo),
		root:   resolvedRoot,
		runner: runner,
	}, nil
}

// Root returns the canonical allowed root.
func (r *Registry) Root() string { return r.root }

// PrepareNewRepositoryPath checks that a place for a new repository is an
// absolute path under the root whose parent exists, and that the place itself
// does not.
//
// Both ways of making one ask this: `git clone` and `git init` each need a
// directory that is inside the boundary and not yet there. Named after what it
// checks rather than after either command, because a second caller under a
// clone-shaped name is how the check quietly becomes clone's private business
// again.
//
// The destination will not exist yet — that is the point — so the usual
// resolveWithinRoot path (EvalSymlinks on the candidate) cannot be used. The
// parent is resolved and checked; the leaf is then joined onto the resolved
// parent, which is how a symlink under the root cannot smuggle the destination
// outside it.
// DiscardIncompleteClone removes what a failed clone left behind.
//
// It is safe to remove because of what PrepareNewRepositoryPath guaranteed
// before the clone started: the path did not exist. Anything there now was
// written by that clone, so taking it away restores exactly the state the user
// was in before they pressed the button — rather than leaving a directory that
// makes the next attempt fail with "destination already exists" and that
// nothing in the interface can delete.
//
// The root check is made again rather than trusted from the earlier call. This
// is the one place in the project that removes a directory tree the user did
// not name file by file, and a path that has stopped being inside the root
// between the two calls is a path this must refuse rather than delete.
func (r *Registry) DiscardIncompleteClone(destination string) error {
	cleaned := filepath.Clean(destination)
	if !filepath.IsAbs(cleaned) {
		return fmt.Errorf("%q: %w", destination, ErrPathNotAbsolute)
	}

	// The parent is resolved before the comparison, exactly as
	// PrepareNewRepositoryPath resolves it, because isWithin compares two
	// resolved paths and the root already is one. Handing it a path as written
	// refuses a directory that is genuinely inside the root the moment any
	// component above it is a symbolic link — /var is /private/var on macOS,
	// so every temporary directory there took that branch.
	//
	// The leaf is joined back unresolved on purpose: resolving it would follow
	// a symbolic link planted at the destination, and it is the Lstat below
	// that has to see it as a link in order to refuse it.
	resolvedParent, err := r.resolveWithinRoot(filepath.Dir(cleaned))
	if err != nil {
		return err
	}
	cleaned = filepath.Join(resolvedParent, filepath.Base(cleaned))
	if !isWithin(r.root, cleaned) {
		return fmt.Errorf("%q: %w (%q)", cleaned, ErrOutsideRoot, r.root)
	}

	// A symbolic link at the destination would make RemoveAll follow nothing —
	// it removes the link — but a link is not what a clone leaves, and finding
	// one here means something else made it. Refusing is the answer that
	// cannot destroy what somebody else was doing.
	info, err := os.Lstat(cleaned)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot inspect %q: %w", cleaned, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory a clone would have made (%s)", cleaned, fileKind(info))
	}

	return os.RemoveAll(cleaned)
}

func (r *Registry) PrepareNewRepositoryPath(candidate string) (string, error) {
	if !filepath.IsAbs(candidate) {
		return "", fmt.Errorf("%q: %w", candidate, ErrPathNotAbsolute)
	}

	cleaned := filepath.Clean(candidate)
	parent := filepath.Dir(cleaned)
	resolvedParent, err := r.resolveWithinRoot(parent)
	if err != nil {
		return "", err
	}

	destination := filepath.Join(resolvedParent, filepath.Base(cleaned))
	if !isWithin(r.root, destination) {
		return "", fmt.Errorf("%q: %w (%q)", destination, ErrOutsideRoot, r.root)
	}

	info, err := os.Lstat(destination)
	if err == nil {
		return "", fmt.Errorf("%q: %w (%s)", destination, ErrDestinationExists, fileKind(info))
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("cannot inspect %q: %w", destination, err)
	}

	return destination, nil
}

// ClaimNewRepositoryPath is PrepareNewRepositoryPath, then creates the
// destination as a real directory before anything else can plant a symlink
// there.
//
// Prepare alone leaves a TOCTOU window: the leaf is confirmed absent, then
// `git clone` / `git init` run later, and a symlink planted in between makes
// git write through the link — outside the root — while the path argument
// still looks like it is under it. Creating the directory ourselves closes
// that window: Mkdir fails if a symlink (or anything else) appeared, and git
// is then told to fill an empty directory we already hold under the root.
//
// Used by the run half of clone and init. The plan half still uses Prepare,
// because a plan creates nothing.
func (r *Registry) ClaimNewRepositoryPath(candidate string) (string, error) {
	destination, err := r.PrepareNewRepositoryPath(candidate)
	if err != nil {
		return "", err
	}

	if err := os.Mkdir(destination, 0o755); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("%q: %w", destination, ErrDestinationExists)
		}
		return "", fmt.Errorf("cannot create %q: %w", destination, err)
	}

	if err := r.VerifyClaimedDirectory(destination); err != nil {
		// Best effort: leave no empty shell behind when the claim itself failed
		// its own check. The error that matters is the verification one.
		if removeErr := os.Remove(destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return "", fmt.Errorf("%w (also could not remove %q: %w)", err, destination, removeErr)
		}
		return "", err
	}
	return destination, nil
}

// VerifyClaimedDirectory confirms a claimed destination is still a real
// directory under the root, not a symlink swapped in after Claim.
//
// Called immediately before git is handed the path, so the residual window is
// the gap between this check and exec — not the whole time since Prepare.
func (r *Registry) VerifyClaimedDirectory(destination string) error {
	cleaned := filepath.Clean(destination)
	if !isWithin(r.root, cleaned) {
		return fmt.Errorf("%q: %w (%q)", cleaned, ErrOutsideRoot, r.root)
	}

	info, err := os.Lstat(cleaned)
	if err != nil {
		return fmt.Errorf("cannot inspect %q: %w", cleaned, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%q: %w (symlink)", cleaned, ErrDestinationExists)
	}
	if !info.IsDir() {
		return fmt.Errorf("%q: %w (%s)", cleaned, ErrDestinationExists, fileKind(info))
	}
	return nil
}

// PathWithinRoot resolves a path and refuses it when it leaves the root.
//
// The same check Open uses, exposed for callers that already hold a path from
// git (a linked worktree, a submodule) and need to know whether this daemon
// may act on it.
func (r *Registry) PathWithinRoot(candidate string) (string, error) {
	return r.resolveWithinRoot(candidate)
}

func fileKind(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "directory"
	case info.Mode()&os.ModeSymlink != 0:
		return "symlink"
	default:
		return "file"
	}
}

// Open opens a repository designated by its path on disk.
//
// This is the only entry point that accepts a path coming from the client.
// Opening the same repository twice returns the same entry: the operation is
// idempotent.
func (r *Registry) Open(ctx context.Context, requestedPath string) (*Repo, error) {
	resolved, err := r.resolveWithinRoot(requestedPath)
	if err != nil {
		return nil, err
	}

	located, err := r.locate(ctx, resolved)
	if err != nil {
		return nil, err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// The identity is the git directory, not the work tree: two linked
	// worktrees of one repository share a history, and opening both should
	// not open the same repository twice under two names.
	identifier := repoIdentifier(located.gitDir)
	if existing, present := r.byID[identifier]; present {
		if err := r.stillTheSame(existing); err == nil {
			return existing, nil
		}

		// The path resolves to a DIFFERENT directory than the one this entry
		// was opened on: deleted and re-cloned, or rebuilt by a test.
		//
		// Every request addressed by identifier refuses that, correctly and
		// permanently — the identifier promised one repository and would now
		// serve another. But this call is not addressed by an identifier. It
		// arrived with a path, that path has just been checked against the
		// root and handed to git, and this is the one entry point allowed to
		// accept one. Refusing to open a repository that is there, because
		// something else used to be, is not a boundary — it is a stale note.
		//
		// Without this the only way back was closing the tab, so a daemon
		// outliving a `git clone` over the top of a checkout refused it for
		// the rest of its life.
		// Forgotten first, released second — the order Close uses, and for
		// the reason Close uses it. A descriptor that refuses to close (a
		// stale handle, an EIO) would otherwise leave the entry in the map
		// with its file already shut: every later Open finds it, fails
		// stillTheSame, fails the close again, and refuses. That is the
		// permanent "no way back" this block exists to remove, rebuilt one
		// error deeper.
		delete(r.byID, identifier)
		r.order = slices.DeleteFunc(r.order, func(known string) bool { return known == identifier })
		if err := r.release(existing); err != nil {
			return nil, err
		}
	}

	// The identity is taken only once the repository is known to be new.
	//
	// Open is idempotent and the returning caller is the common case — a second
	// tab on a repository already open. Taking a descriptor before the check
	// above would mean closing it again on exactly that path, every time, and
	// the error from that close would have nowhere left to go.
	pinned, identity, err := pinGitDir(located.gitDir)
	if err != nil {
		return nil, err
	}

	opened := &Repo{
		ID:             identifier,
		Path:           located.path,
		Name:           filepath.Base(located.path),
		Bare:           located.bare,
		OpenedAt:       time.Now().UTC(),
		gitDir:         located.gitDir,
		worktreeGitDir: located.worktreeGitDir,
		identity:       identity,
		pinned:         pinned,
	}
	r.byID[identifier] = opened
	r.order = append(r.order, identifier)

	return opened, nil
}

// location is what git says a repository is, once both of its halves have been
// checked against the root.
type location struct {
	path           string // where git commands run
	gitDir         string // where the objects, refs and config live
	worktreeGitDir string // where this work tree's own HEAD and index live
	bare           bool
}

// locate asks git what repository a directory belongs to, and validates every
// path it answers with.
//
// git has the last word on what a repository is and where it starts. Asking it
// rather than hunting for a .git by hand makes subdirectories, worktrees,
// submodules and bare repositories work all at once.
//
// Two questions, not one, and that is the security-relevant part.
// --show-toplevel alone answers with the work tree, which a hostile repository
// controls: a `.git` file reading `gitdir: /elsewhere` keeps the work tree
// inside the root while the history it serves comes from outside. Clone such a
// repository into your root and yagit would hand out its target's commits.
// --git-common-dir is the half that cannot lie, because it is where git
// actually reads from.
func (r *Registry) locate(ctx context.Context, dir string) (location, error) {
	// One call for all three: --git-common-dir works everywhere, bare
	// included; --git-dir is the same place except in a linked worktree,
	// where it is that worktree's own state and nothing else reports it; and
	// --is-bare-repository decides whether asking for a work tree makes any
	// sense at all.
	output, err := r.runner.Run(ctx, dir,
		"rev-parse", "--path-format=absolute",
		"--git-common-dir", "--git-dir", "--is-bare-repository")
	if err != nil {
		return location{}, fmt.Errorf("%q is not a git repository: %w", dir, err)
	}

	lines := splitGitLines(output)
	if len(lines) != 3 {
		return location{}, fmt.Errorf(
			"git rev-parse answered %d lines, 3 expected, in %q", len(lines), dir)
	}

	gitDir, err := r.resolveWithinRoot(lines[0])
	if err != nil {
		return location{}, fmt.Errorf(
			"the git directory of %q lies outside the allowed root: %w", dir, err)
	}

	// Checked against the root like the common directory, and for the same
	// reason: it is a path git chose, but a repository is a directory anybody
	// can write into, and a `gitdir:` file pointing elsewhere is exactly the
	// escape the common-directory check exists to close.
	worktreeGitDir, err := r.resolveWithinRoot(lines[1])
	if err != nil {
		return location{}, fmt.Errorf(
			"the work tree's git directory of %q lies outside the allowed root: %w", dir, err)
	}
	bare := lines[2] == "true"

	if bare {
		// No work tree to check, and none to run commands in: git runs in the
		// git directory itself.
		return location{path: gitDir, gitDir: gitDir, worktreeGitDir: gitDir, bare: true}, nil
	}

	output, err = r.runner.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	if err != nil {
		return location{}, fmt.Errorf("cannot find the work tree of %q: %w", dir, err)
	}

	lines = splitGitLines(output)
	if len(lines) != 1 {
		return location{}, fmt.Errorf(
			"git rev-parse answered %d lines, 1 expected, in %q", len(lines), dir)
	}

	workTree, err := r.resolveWithinRoot(lines[0])
	if err != nil {
		return location{}, err
	}

	return location{
		path:           workTree,
		gitDir:         gitDir,
		worktreeGitDir: worktreeGitDir,
		bare:           false,
	}, nil
}

// splitGitLines cuts git's output into lines.
//
// It trims the trailing newline and nothing else. TrimSpace would look
// equivalent and is not: a directory named "release " is legal, git prints its
// name verbatim, and trimming the trailing space turns a valid path into one
// that does not exist — with an error message blaming the wrong thing.
func splitGitLines(output []byte) []string {
	trimmed := strings.TrimSuffix(string(output), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// Get returns the repository an identifier designates, after checking it is
// still the one that was opened.
//
// Validating a path once, when it is opened, is not enough. Everything under
// the root is writable by whoever runs the daemon and by whatever they run:
// between two requests a directory can be replaced by a symlink pointing at a
// repository outside the root, and every command afterwards would read from
// there. So the check is repeated on every access — it costs two stat calls,
// and it is the only thing standing between a stale identifier and a boundary
// that no longer holds.
func (r *Registry) Get(identifier string) (*Repo, error) {
	r.mutex.RLock()
	found, present := r.byID[identifier]
	r.mutex.RUnlock()

	if !present {
		return nil, fmt.Errorf("%w: %q", ErrUnknownRepo, identifier)
	}
	if err := r.stillTheSame(found); err != nil {
		return nil, err
	}
	return found, nil
}

// stillTheSame re-checks both halves of a repository against the root, and
// that the git directory is the very one that was opened.
func (r *Registry) stillTheSame(opened *Repo) error {
	if _, err := r.resolveWithinRoot(opened.Path); err != nil {
		return fmt.Errorf("%q is no longer reachable where it was opened: %w", opened.Name, err)
	}

	gitDir, err := r.resolveWithinRoot(opened.gitDir)
	if err != nil {
		return fmt.Errorf("the git directory of %q no longer lies inside the allowed root: %w",
			opened.Name, err)
	}

	// The work tree's own state directory is checked too, and it was not
	// until files began to be READ out of it — MERGE_MSG, MERGE_HEAD, a
	// rebase's progress. It was validated once, at open time; everything under
	// the root is writable by whoever runs the daemon, so once at open time is
	// the same guarantee the other two paths above deliberately do not settle
	// for.
	if _, err := r.resolveWithinRoot(opened.StateDir()); err != nil {
		return fmt.Errorf("the state directory of %q no longer lies inside the allowed root: %w",
			opened.Name, err)
	}

	current, err := os.Stat(gitDir)
	if err != nil {
		return fmt.Errorf("cannot inspect the git directory of %q: %w", opened.Name, err)
	}

	// Same path, different directory: something took its place inside the
	// root. The identifier promised one repository and would now serve
	// another.
	if !os.SameFile(current, opened.identity) {
		return fmt.Errorf("%w: %q", ErrRepoReplaced, opened.Name)
	}
	return nil
}

// Close forgets a repository, releasing whatever it held for it.
//
// Without this an entry is permanent for the life of the process, and one case
// makes that actively harmful: a repository replaced on disk — deleted and
// re-cloned, which people do — fails every request afterwards with
// ErrRepoReplaced, correctly and forever. Refusing is right; refusing with no
// way back is not.
//
// Closing an identifier that names nothing is not an error. Two tabs shut at
// once, or a reload racing a click, must not produce a failure about a
// repository nobody wanted any more.
func (r *Registry) Close(identifier string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	closing, present := r.byID[identifier]
	if !present {
		return nil
	}

	delete(r.byID, identifier)
	r.order = slices.DeleteFunc(r.order, func(known string) bool { return known == identifier })

	return r.release(closing)
}

// release gives back whatever an entry was holding.
//
// Shared by Close and by the re-open above, because both let go of an entry
// and a second copy of this is how one of them comes to leak the descriptor.
// The caller holds the lock.
func (r *Registry) release(closing *Repo) error {
	// The descriptor held to keep the identity durable, on the platforms that
	// hold one. Leaking it would defeat the point of taking it deliberately.
	if closing.pinned == nil {
		return nil
	}
	if err := closing.pinned.Close(); err != nil {
		return fmt.Errorf("releasing the git directory of %q: %w", closing.Name, err)
	}
	return nil
}

func (r *Registry) List() []*Repo {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	repos := make([]*Repo, 0, len(r.order))
	for _, identifier := range r.order {
		repos = append(repos, r.byID[identifier])
	}
	return repos
}

// resolveWithinRoot canonicalizes a path and checks that it stays under the
// allowed root.
//
// Symlinks are resolved BEFORE the comparison. Without that, a link planted
// inside the root and pointing at /etc would be enough to get out, and the
// guard would guard nothing.
func (r *Registry) resolveWithinRoot(candidate string) (string, error) {
	if !filepath.IsAbs(candidate) {
		return "", fmt.Errorf("%q: %w", candidate, ErrPathNotAbsolute)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Clean(candidate))
	if err != nil {
		return "", fmt.Errorf("%q not found: %w", candidate, err)
	}

	if !isWithin(r.root, resolved) {
		return "", fmt.Errorf("%q: %w (%q)", resolved, ErrOutsideRoot, r.root)
	}
	return resolved, nil
}

// isWithin reports whether path is the root or a descendant of the root. Both
// paths must already be cleaned and resolved.
//
// The separator appended to the root is not decoration: without it,
// "/srv/data" would count as containing "/srv/database".
func isWithin(root, path string) bool {
	if path == root {
		return true
	}

	// Added only when the root does not already end in one, because a root of
	// "/" would otherwise become "//" and match no absolute path at all —
	// every path refused, with a message saying it lies outside a root that
	// contains everything.
	separator := string(filepath.Separator)
	if !strings.HasSuffix(root, separator) {
		root += separator
	}
	return strings.HasPrefix(path, root)
}

// repoIdentifier derives a stable, opaque identifier from the canonical path.
//
// Stable: the same repository keeps its identifier from one run to the next,
// so tabs survive a restart. Opaque: it does not reveal the layout of the disk
// to a web page watching it.
func repoIdentifier(canonicalPath string) string {
	sum := sha256.Sum256([]byte(canonicalPath))
	return hex.EncodeToString(sum[:])[:12]
}
