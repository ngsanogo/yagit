// Finding repositories the user has not named.
//
// Open is the daemon's security boundary and takes a path from the network.
// This file takes none: it walks inside the allowed root and reports what it
// found, and every path it produces came off the disk rather than off the
// wire. Opening one still goes through Open — nothing here shortcuts it.
//
// What it reports is therefore also a disclosure: the names of directories
// under the root, to anyone holding the session token. That is the same
// audience Open already serves, and it is the point of the feature.

package repo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// How deep a scan goes, unless the caller says otherwise, and how deep it may
// be asked to go.
//
// Four levels finds ~/code/org/project and stops before it has walked a home
// directory to the leaves. The ceiling is what makes the depth safe to accept
// from a request at all: without it, `?depth=` is a way to ask the daemon to
// stat every file the user owns, one HTTP request at a time.
const (
	defaultDiscoverDepth = 4
	maxDiscoverDepth     = 8
)

// DiscoverOptions controls a scan for git repositories under the allowed root.
type DiscoverOptions struct {
	// Dir is the directory to scan. Empty means the registry root.
	Dir string

	// MaxDepth limits how many directory levels below Dir are visited. Zero
	// selects the default; values above maxDiscoverDepth are capped.
	MaxDepth int

	// IncludeWorktrees lists linked worktrees as separate entries. By default
	// only the main worktree of each repository is returned.
	IncludeWorktrees bool

	// IncludeSubmodules runs a second pass with git submodule foreach on every
	// top-level repository found in the walk.
	IncludeSubmodules bool
}

// DiscoveredKind says how a path relates to the repository git reported.
type DiscoveredKind string

const (
	DiscoveredTopLevel  DiscoveredKind = "top-level"
	DiscoveredWorktree  DiscoveredKind = "worktree"
	DiscoveredSubmodule DiscoveredKind = "submodule"
)

// DiscoveredRepo is a git repository the scan found but has not opened yet.
type DiscoveredRepo struct {
	Path string         `json:"path"`
	Name string         `json:"name"`
	Bare bool           `json:"bare"`
	Kind DiscoveredKind `json:"kind"`
}

// DiscoverResult is what a scan produced. ScannedFrom is canonical: symlinks
// resolved, always inside the allowed root.
type DiscoverResult struct {
	Repos       []DiscoveredRepo `json:"repos"`
	ScannedFrom string           `json:"scanned_from"`

	// Depth is how many levels below ScannedFrom this scan went, once the
	// default was filled in and the ceiling applied; DepthLimit is that
	// ceiling. Both are reported rather than assumed, because both are
	// decided here: a control that lets somebody change the depth would
	// otherwise carry a second copy of two numbers it does not own.
	Depth      int `json:"depth"`
	DepthLimit int `json:"depth_limit"`

	// Skipped is why the scan found what it found.
	Skipped DiscoverSkipped `json:"skipped"`

	// SubmoduleFailures are the superprojects whose submodules could not be
	// listed. Only ever populated when IncludeSubmodules was set.
	//
	// Kept out of the JSON because each one carries an error: the API turns
	// it into the same {message, git} pair every error response uses, which
	// is the shape the interface already knows how to draw.
	SubmoduleFailures []DiscoverFailure `json:"-"`
}

// DiscoverSkipped counts the directories a scan refused to look inside, by
// the reason it refused.
//
// A scan that finds nothing is the case this exists for. "No repositories
// here" is not something a person can act on; "eleven directories were deeper
// than the limit" is, and the difference costs one counter per reason.
//
// Named fields rather than a map keyed by strings. The set of reasons is
// fixed, it lives in one function, and every reader of this has to know all
// of it anyway — whereas a misspelled key is a hint that silently never
// appears.
type DiscoverSkipped struct {
	// Unreadable: the directory could not be listed at all.
	Unreadable int `json:"unreadable"`

	// TooDeep: more levels below the scanned directory than MaxDepth allows.
	TooDeep int `json:"too_deep"`

	// IgnoredName: a name in skipDiscoverDirNames.
	IgnoredName int `json:"ignored_name"`

	// Dotted: a name beginning with a dot.
	Dotted int `json:"dotted"`

	// Worktrees and Submodules: a repository of a kind the caller did not ask
	// for. Two counters and not one, because the interface offers a separate
	// switch for each, and "turn on whichever one applies" is not a next move.
	Worktrees  int `json:"worktrees"`
	Submodules int `json:"submodules"`

	// NotARepository: something a repository has was there — a `.git`, a HEAD
	// — and git then refused to call the directory a repository.
	NotARepository int `json:"not_a_repository"`
}

// DiscoverFailure is a repository the scan could not finish inspecting.
//
// A scan is best effort, so one superproject whose submodules cannot be
// listed must not empty the list of everything else that was found. The
// failure is not the scan's to keep either: it leaves with the result, so
// that what git said — the command, the exit code, the raw stderr — reaches
// the person who asked.
type DiscoverFailure struct {
	// Path is the repository the failing command ran in.
	Path string

	// Err is what came back: a *git.Error wherever git itself refused.
	Err error
}

// gitDirName is what sits at the top of a repository: a directory in a normal
// clone, a file holding a gitdir pointer in a worktree or a submodule.
const gitDirName = ".git"

// Directories a scan does not enter.
//
// Not a blocklist of things that are dangerous — a repository inside any of
// these is a real repository. It is about what a person is looking for: the
// dependency tree of a project they already have open is noise, and walking
// node_modules is most of what a scan of a home directory would cost.
//
// These four are the whole list, and the sentence the interface writes about
// the counter names all four. `.git` is skipped in the walk instead, and
// counted as nothing: what is inside one is not a repository somebody is
// looking for, and it is not one of the names that sentence offers to go
// looking in.
//
// Names beginning with a dot are skipped for the same reason, in the walk
// itself rather than here, because that rule has an exception: the directory
// the scan was pointed AT is entered whatever it is called. Somebody who asks
// for ~/.local/src means it.
var skipDiscoverDirNames = map[string]struct{}{
	"node_modules": {},
	"vendor":       {},
	"target":       {},
	".cache":       {},
}

type discoverCandidate struct {
	repo   DiscoveredRepo
	gitDir string
}

// Discover walks Dir looking for git repositories, validated the same way Open
// validates one it is handed.
//
// Submodules and linked worktrees are omitted unless the caller opts in, and
// that default is the one judgement this function makes. A linked worktree is
// the same repository seen twice: listing both puts two entries with the same
// history in front of somebody who has one project. A submodule checkout is a
// repository, but it is one you reach through its superproject, and a scan
// that lists them all turns a project with a dozen into thirteen rows. Both
// are still there for whoever wants them.
//
// The result is sorted by path, which is what makes it stable: the walk order
// is the filesystem's, and a list that reordered itself between two scans of
// an unchanged disk would be unusable.
func (r *Registry) Discover(
	ctx context.Context,
	opts DiscoverOptions,
) (result DiscoverResult, err error) {
	// The scan runs inside an os.Root: a handle on the allowed root whose
	// every operation is refused by the operating system if the name it is
	// given would leave that directory, symlinks included.
	//
	// resolveWithinRoot below already proves the same thing — it resolves the
	// links and compares the result against the root. That check stays. But
	// it is a string comparison this package performs on a path, once, and
	// this walk then makes tens of thousands of filesystem calls on paths
	// derived from it. Holding the root open makes the guarantee structural:
	// nothing reachable through `root` can be outside it, whatever a later
	// edit to the walk does. It is the one place in yagit that reads a
	// directory the user did not name, and the cost of being wrong here is
	// somebody's ~/.ssh.
	root, err := os.OpenRoot(r.root)
	if err != nil {
		return DiscoverResult{}, fmt.Errorf("opening the allowed root %q: %w", r.root, err)
	}
	defer func() {
		// The scan's own failure comes first. Nothing was written through
		// this handle, so a failure to close it is not what went wrong, and
		// reporting it instead would replace a sentence the user can act on
		// with one nobody can. It is still reported when there is nothing
		// else to say, because a descriptor that will not close is the daemon
		// running out of them later, for a reason nothing recorded.
		if closeErr := root.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("closing the allowed root %q: %w", r.root, closeErr)
		}
	}()

	scanRoot := r.root
	if opts.Dir != "" {
		resolved, resolveErr := r.resolveWithinRoot(opts.Dir)
		if resolveErr != nil {
			return DiscoverResult{}, resolveErr
		}
		scanRoot = resolved
	}

	// Inside the root, names are relative and slash-separated — the io/fs
	// convention — and the root itself is ".". filesystem holds the walk to
	// the same boundary the handle does.
	from, err := rootRelative(r.root, scanRoot)
	if err != nil {
		return DiscoverResult{}, err
	}
	filesystem := root.FS()

	info, err := fs.Stat(filesystem, from)
	if err != nil {
		return DiscoverResult{}, err
	}
	if !info.IsDir() {
		return DiscoverResult{}, &os.PathError{Op: "discover", Path: scanRoot, Err: os.ErrNotExist}
	}

	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = defaultDiscoverDepth
	}
	if maxDepth > maxDiscoverDepth {
		maxDepth = maxDiscoverDepth
	}

	var collected []discoverCandidate
	byGitDir := map[string]int{}
	var skipped DiscoverSkipped

	err = fs.WalkDir(filesystem, from, func(name string, entry fs.DirEntry, walkErr error) error {
		// A directory the daemon cannot read is not a failure of the scan: a
		// home directory holds plenty a process is not entitled to open, and
		// refusing to list any repository because one of them was unreadable
		// would be the wrong answer to the question that was asked. It is
		// counted rather than dropped, so a scan that came back empty can
		// still say what it never looked at.
		if walkErr != nil {
			skipped.Unreadable++
			return fs.SkipDir
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !entry.IsDir() {
			return nil
		}

		// The depth test comes after the directory test rather than before
		// it, and the counter is the reason. SkipDir returned for a *file*
		// skips every remaining entry of its parent, so testing files here
		// recorded one directory too deep for a whole level of them — and
		// which one depended on the order the filesystem listed them in.
		if name != from && depthBelow(from, name) > maxDepth {
			skipped.TooDeep++
			return fs.SkipDir
		}

		if name != from {
			// Uncounted, and before both counters below. A directory git
			// refused to call a repository is counted where that is decided;
			// letting the walk then descend into its `.git` and count that
			// too moved two counters for one broken repository, and the
			// louder of the two named four directory names that had nothing
			// to do with it.
			if entry.Name() == gitDirName {
				return fs.SkipDir
			}
			if _, skip := skipDiscoverDirNames[entry.Name()]; skip {
				skipped.IgnoredName++
				return fs.SkipDir
			}
			if strings.HasPrefix(entry.Name(), ".") {
				skipped.Dotted++
				return fs.SkipDir
			}
		}

		if !mightBeRepository(root, name) {
			return nil
		}

		// git needs a real directory to run in, and that is the only thing
		// the absolute path is used for from here on.
		candidate, isRepository := r.candidateAt(ctx, absoluteOf(r.root, name))
		if !isRepository {
			skipped.NotARepository++
			return nil
		}

		if !discoverIncludeKind(candidate.repo.Kind, opts) {
			switch candidate.repo.Kind {
			case DiscoveredWorktree:
				skipped.Worktrees++
			case DiscoveredSubmodule:
				skipped.Submodules++
			}
			return fs.SkipDir
		}

		if opts.IncludeWorktrees {
			collected = append(collected, candidate)
		} else {
			index, seen := byGitDir[candidate.gitDir]
			if !seen {
				byGitDir[candidate.gitDir] = len(collected)
				collected = append(collected, candidate)
			} else if collected[index].repo.Kind != DiscoveredTopLevel &&
				candidate.repo.Kind == DiscoveredTopLevel {
				collected[index] = candidate
			}
		}

		return fs.SkipDir
	})
	if err != nil {
		return DiscoverResult{}, err
	}

	var submoduleFailures []DiscoverFailure
	if opts.IncludeSubmodules {
		submodules, failures, submoduleErr := r.discoverSubmodules(ctx, collected)
		if submoduleErr != nil {
			return DiscoverResult{}, submoduleErr
		}
		submoduleFailures = failures
		collected = append(collected, submodules...)
	}

	repos := make([]DiscoveredRepo, len(collected))
	for index, candidate := range collected {
		repos[index] = candidate.repo
	}

	slices.SortFunc(repos, func(first, second DiscoveredRepo) int {
		return strings.Compare(first.Path, second.Path)
	})

	return DiscoverResult{
		Repos:             repos,
		ScannedFrom:       scanRoot,
		Depth:             maxDepth,
		DepthLimit:        maxDiscoverDepth,
		Skipped:           skipped,
		SubmoduleFailures: submoduleFailures,
	}, nil
}

// candidateAt asks git what a directory actually is.
//
// The two errors it swallows are the same answer: this is not a repository the
// scan may list. A `.git` left by something that is not git, a gitfile pointing
// at a directory that has been deleted, a repository whose git directory
// resolves outside the allowed root — none of them is a failure of the scan,
// and all of them are things a home directory holds. Returning false rather
// than an error is what says so at the call site.
//
// The walk carries on into the directory afterwards. What lies below it is not
// decided by what its own name suggested.
func (r *Registry) candidateAt(ctx context.Context, dir string) (discoverCandidate, bool) {
	located, err := r.locate(ctx, dir)
	if err != nil {
		return discoverCandidate{}, false
	}

	kind, err := r.classifyRepo(ctx, located)
	if err != nil {
		return discoverCandidate{}, false
	}

	return discoverCandidate{
		repo: DiscoveredRepo{
			Path: located.path,
			Name: filepath.Base(located.path),
			Bare: located.bare,
			Kind: kind,
		},
		gitDir: located.gitDir,
	}, true
}

// mightBeRepository is a cheap pre-filter before locate: two stats against a
// directory entry, instead of a git process for every directory on the disk.
// It catches normal work trees and bare repositories, and everything it lets
// through still goes to locate for the real answer.
//
// Both stats go through the root handle rather than through os, so a name the
// walk produced cannot be made to reach outside the allowed root even if the
// walk one day stops producing only names from inside it.
func mightBeRepository(root *os.Root, dir string) bool {
	if _, err := root.Lstat(within(dir, gitDirName)); err == nil {
		return true
	}
	info, err := root.Stat(within(dir, "HEAD"))
	return err == nil && !info.IsDir()
}

// within joins a child onto a name inside the root, in io/fs form: slashes,
// and no "./" in front of a name at the top.
func within(dir, child string) string {
	if dir == "." {
		return child
	}
	return dir + "/" + child
}

// rootRelative turns an absolute path inside the root into the name io/fs
// knows it by. The root itself is ".".
func rootRelative(root, path string) (string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(relative), nil
}

// absoluteOf is rootRelative backwards: the path on disk that a name inside
// the root stands for.
func absoluteOf(root, name string) string {
	if name == "." {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(name))
}

// depthBelow counts the levels between the directory a scan started at and one
// it has reached. Both are io/fs names, so the separator is always a slash and
// there is no platform to think about.
func depthBelow(from, name string) int {
	if from != "." {
		name = strings.TrimPrefix(name, from+"/")
	}
	return strings.Count(name, "/") + 1
}

func discoverIncludeKind(kind DiscoveredKind, opts DiscoverOptions) bool {
	switch kind {
	case DiscoveredSubmodule:
		return opts.IncludeSubmodules
	case DiscoveredWorktree:
		return opts.IncludeWorktrees
	default:
		return true
	}
}

// classifyRepo asks git whether a located repository is a submodule checkout
// or a linked worktree.
func (r *Registry) classifyRepo(ctx context.Context, located location) (DiscoveredKind, error) {
	output, err := r.runner.Run(ctx, located.path, "rev-parse", "--path-format=absolute",
		"--show-superproject-working-tree", "--git-dir", "--git-common-dir")
	if err != nil {
		return "", err
	}

	lines := splitGitLines(output)
	var superproject, gitDirRaw, commonDirRaw string
	switch len(lines) {
	case 3:
		superproject, gitDirRaw, commonDirRaw = lines[0], lines[1], lines[2]
	case 2:
		gitDirRaw, commonDirRaw = lines[0], lines[1]
	default:
		return "", fmt.Errorf(
			"git rev-parse answered %d lines, 2 or 3 expected, in %q", len(lines), located.path)
	}

	if superproject != "" {
		if _, err := r.resolveWithinRoot(superproject); err != nil {
			return "", err
		}
		return DiscoveredSubmodule, nil
	}

	gitDir, err := r.resolveWithinRoot(gitDirRaw)
	if err != nil {
		return "", err
	}
	commonDir, err := r.resolveWithinRoot(commonDirRaw)
	if err != nil {
		return "", err
	}

	if gitDir != commonDir {
		return DiscoveredWorktree, nil
	}
	return DiscoveredTopLevel, nil
}

// discoverSubmodules lists the submodules of every repository the walk kept.
//
// It returns what it found and what it could not: a superproject git refuses
// to answer about is a DiscoverFailure, never a shorter list. The two are
// indistinguishable otherwise, and the wrong one of them is silent.
func (r *Registry) discoverSubmodules(
	ctx context.Context,
	parents []discoverCandidate,
) ([]discoverCandidate, []DiscoverFailure, error) {
	seen := map[string]struct{}{}
	for _, parent := range parents {
		seen[parent.repo.Path] = struct{}{}
	}

	var found []discoverCandidate
	var failures []DiscoverFailure
	for _, parent := range parents {
		// A cancelled request stops the pass instead of filling it with
		// failures: every remaining command would fail for the same reason,
		// and each would be reported against a repository that has nothing
		// wrong with it.
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		if parent.repo.Bare || parent.repo.Kind == DiscoveredSubmodule {
			continue
		}

		// $displaypath, not `pwd`.
		//
		// git runs a foreach command through a shell, and on Windows that
		// shell is the one git bundles: `pwd` there answers an MSYS path —
		// /c/Users/… — which is not a path any Go program can open, so every
		// submodule was silently dropped on the one platform where nothing
		// else in this package differs. $displaypath is git's own, relative
		// to the directory foreach was invoked from, with forward slashes on
		// every platform. FromSlash turns those into the separator this
		// machine uses; on Unix it is a no-op.
		output, err := r.runner.Run(ctx, parent.repo.Path,
			"submodule", "foreach", "--recursive", "--quiet", `echo "$displaypath"`)
		if err != nil {
			// Kept, not swallowed. A superproject whose submodules cannot be
			// listed used to come back as a superproject with no submodules,
			// which is the same answer git gives when there are none.
			// Failing the whole scan instead would let one broken .gitmodules
			// hide every other repository on the disk.
			failures = append(failures, DiscoverFailure{Path: parent.repo.Path, Err: err})
			continue
		}

		for _, line := range splitGitLines(output) {
			if line == "" {
				continue
			}

			path := filepath.Join(parent.repo.Path, filepath.FromSlash(line))
			if _, present := seen[path]; present {
				continue
			}

			// Neither of the next two is a failure to report, for the
			// reasons candidateAt gives: git named a path, and what is at the
			// end of it is not a repository this scan may list — a checkout
			// deleted from the work tree, one whose git directory resolves
			// outside the allowed root, or something git does not call a
			// submodule at all.
			located, locateErr := r.locate(ctx, path)
			if locateErr != nil {
				continue
			}

			kind, classifyErr := r.classifyRepo(ctx, located)
			if classifyErr != nil || kind != DiscoveredSubmodule {
				continue
			}

			// Keyed by what locate resolved, not by what git printed: two
			// superprojects sharing a submodule reach it by two different
			// paths, and only the canonical one tells them apart.
			seen[located.path] = struct{}{}
			found = append(found, discoverCandidate{
				repo: DiscoveredRepo{
					Path: located.path,
					Name: filepath.Base(located.path),
					Bare: located.bare,
					Kind: DiscoveredSubmodule,
				},
				gitDir: located.gitDir,
			})
		}
	}

	return found, failures, nil
}
