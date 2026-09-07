package repo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

func TestDiscoverFindsSiblingRepositories(t *testing.T) {
	registry, root, runner := newRegistry(t)

	pathA := filepath.Join(root, "projects", "alpha")
	pathB := filepath.Join(root, "projects", "beta")
	createRepo(t, runner, pathA)
	createRepo(t, runner, pathB)

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, pathA) || !slices.Contains(paths, pathB) {
		t.Fatalf("paths = %v, want %q and %q", paths, pathA, pathB)
	}
}

func TestDiscoverScansASubdirectoryOnly(t *testing.T) {
	registry, root, runner := newRegistry(t)

	inside := filepath.Join(root, "keep", "repo")
	outside := filepath.Join(root, "skip", "repo")
	createRepo(t, runner, inside)
	createRepo(t, runner, outside)

	scanFrom := filepath.Join(root, "keep")
	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{Dir: scanFrom})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, inside) {
		t.Fatalf("paths = %v, want %q", paths, inside)
	}
	if slices.Contains(paths, outside) {
		t.Fatalf("paths = %v, should not contain %q", paths, outside)
	}
}

func TestDiscoverRespectsMaxDepth(t *testing.T) {
	registry, root, runner := newRegistry(t)

	deep := filepath.Join(root, "a", "b", "c", "repo")
	createRepo(t, runner, deep)

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found.Repos) != 0 {
		t.Fatalf("found = %v, want nothing beyond depth 2", discoveredPaths(found.Repos))
	}

	found, err = registry.Discover(context.Background(), repo.DiscoverOptions{MaxDepth: 4})
	if err != nil {
		t.Fatalf("Discover deeper: %v", err)
	}
	if len(found.Repos) != 1 || found.Repos[0].Path != deep {
		t.Fatalf("found = %v, want [%q]", discoveredPaths(found.Repos), deep)
	}
}

func commitEmpty(t *testing.T, runner *git.Runner, path, message string) {
	t.Helper()
	if _, err := runner.Run(context.Background(), path,
		"-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "--allow-empty", "-m", message); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestDiscoverExcludesLinkedWorktreeByDefault(t *testing.T) {
	registry, root, runner := newRegistry(t)

	mainPath := filepath.Join(root, "main")
	createRepo(t, runner, mainPath)
	commitEmpty(t, runner, mainPath, "init")
	if _, err := runner.Run(context.Background(), mainPath, "branch", "feature"); err != nil {
		t.Fatalf("branch: %v", err)
	}

	linkedPath := filepath.Join(root, "linked")
	if _, err := runner.Run(context.Background(), mainPath,
		"worktree", "add", linkedPath, "feature"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, mainPath) {
		t.Fatalf("paths = %v, want main worktree %q", paths, mainPath)
	}
	if slices.Contains(paths, linkedPath) {
		t.Fatalf("paths = %v, linked worktree %q must be excluded by default", paths, linkedPath)
	}
	// Counted apart from submodules, because the interface offers a separate
	// switch for each and the sentence it writes names one of them.
	if found.Skipped.Worktrees != 1 {
		t.Errorf("Skipped.Worktrees = %d, want 1", found.Skipped.Worktrees)
	}
	if found.Skipped.Submodules != 0 {
		t.Errorf("Skipped.Submodules = %d, want 0: nothing here is a submodule", found.Skipped.Submodules)
	}
}

func TestDiscoverIncludesLinkedWorktreeWhenAsked(t *testing.T) {
	registry, root, runner := newRegistry(t)

	mainPath := filepath.Join(root, "main")
	createRepo(t, runner, mainPath)
	commitEmpty(t, runner, mainPath, "init")
	if _, err := runner.Run(context.Background(), mainPath, "branch", "feature"); err != nil {
		t.Fatalf("branch: %v", err)
	}

	linkedPath := filepath.Join(root, "linked")
	if _, err := runner.Run(context.Background(), mainPath,
		"worktree", "add", linkedPath, "feature"); err != nil {
		t.Fatalf("worktree add: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{IncludeWorktrees: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, mainPath) || !slices.Contains(paths, linkedPath) {
		t.Fatalf("paths = %v, want %q and %q", paths, mainPath, linkedPath)
	}
}

func TestDiscoverExcludesSubmoduleCheckoutByDefault(t *testing.T) {
	registry, root, runner := newRegistry(t)

	standalone := filepath.Join(root, "standalone-sub")
	createRepo(t, runner, standalone)
	commitEmpty(t, runner, standalone, "sub")

	superPath := filepath.Join(root, "super")
	createRepo(t, runner, superPath)
	commitEmpty(t, runner, superPath, "super")
	if _, err := runner.Run(context.Background(), superPath,
		"-c", "protocol.file.allow=always", "submodule", "add", standalone, "submod"); err != nil {
		t.Fatalf("submodule add: %v", err)
	}
	if _, err := runner.Run(context.Background(), superPath,
		"-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "-am", "track submodule"); err != nil {
		t.Fatalf("commit submodule: %v", err)
	}

	submodPath := filepath.Join(superPath, "submod")

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, superPath) {
		t.Fatalf("paths = %v, want superproject %q", paths, superPath)
	}
	if slices.Contains(paths, submodPath) {
		t.Fatalf("paths = %v, submodule checkout %q must be excluded by default", paths, submodPath)
	}

	foundDirect, err := registry.Discover(context.Background(), repo.DiscoverOptions{Dir: submodPath})
	if err != nil {
		t.Fatalf("Discover submodule dir: %v", err)
	}
	if len(foundDirect.Repos) != 0 {
		t.Fatalf("scanning submodule directly = %v, want nothing", discoveredPaths(foundDirect.Repos))
	}
	if foundDirect.Skipped.Submodules != 1 {
		t.Errorf("Skipped.Submodules = %d, want 1: the reason that scan came back empty",
			foundDirect.Skipped.Submodules)
	}
}

func TestDiscoverIncludesSubmoduleWhenAsked(t *testing.T) {
	registry, root, runner := newRegistry(t)

	standalone := filepath.Join(root, "standalone-sub")
	createRepo(t, runner, standalone)
	commitEmpty(t, runner, standalone, "sub")

	superPath := filepath.Join(root, "super")
	createRepo(t, runner, superPath)
	commitEmpty(t, runner, superPath, "super")
	if _, err := runner.Run(context.Background(), superPath,
		"-c", "protocol.file.allow=always", "submodule", "add", standalone, "submod"); err != nil {
		t.Fatalf("submodule add: %v", err)
	}
	if _, err := runner.Run(context.Background(), superPath,
		"-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "-am", "track submodule"); err != nil {
		t.Fatalf("commit submodule: %v", err)
	}

	submodPath := filepath.Join(superPath, "submod")

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{IncludeSubmodules: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, superPath) || !slices.Contains(paths, submodPath) {
		t.Fatalf("paths = %v, want %q and %q", paths, superPath, submodPath)
	}
	if kind := kindForPath(found.Repos, submodPath); kind != repo.DiscoveredSubmodule {
		t.Fatalf("submodule kind = %q, want %q", kind, repo.DiscoveredSubmodule)
	}
}

// A submodule of a submodule, which is where the paths git reports stop being
// interchangeable with the ones a Go program can open.
//
// `git submodule foreach --recursive` runs its command through a shell and
// reports a path relative to where it was invoked — "outer/inner" for this
// one. Reading it with `pwd` instead worked here and dropped every submodule
// on Windows, where the shell git bundles answers /c/Users/… and nothing in
// the package would open that. The recursive case is the one that pins the
// join, because a single level is the case where the relative path happens to
// be a bare name.
func TestDiscoverFindsANestedSubmodule(t *testing.T) {
	registry, root, runner := newRegistry(t)

	leaf := filepath.Join(root, "leaf")
	createRepo(t, runner, leaf)
	commitEmpty(t, runner, leaf, "leaf")

	middle := filepath.Join(root, "middle")
	createRepo(t, runner, middle)
	commitEmpty(t, runner, middle, "middle")
	addSubmodule(t, runner, middle, leaf, "inner")

	superPath := filepath.Join(root, "super")
	createRepo(t, runner, superPath)
	commitEmpty(t, runner, superPath, "super")
	addSubmodule(t, runner, superPath, middle, "outer")

	if _, err := runner.Run(context.Background(), superPath,
		"-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive"); err != nil {
		t.Fatalf("submodule update --recursive: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{IncludeSubmodules: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	nested := filepath.Join(superPath, "outer", "inner")
	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, nested) {
		t.Fatalf("paths = %v, want the nested submodule %q", paths, nested)
	}
	if kind := kindForPath(found.Repos, nested); kind != repo.DiscoveredSubmodule {
		t.Errorf("nested submodule kind = %q, want %q", kind, repo.DiscoveredSubmodule)
	}
}

// addSubmodule adds one repository inside another and commits the result.
//
// protocol.file.allow: git refuses a file:// submodule by default since
// CVE-2022-39253, and every submodule in these tests is one.
func addSubmodule(t *testing.T, runner *git.Runner, parent, child, at string) {
	t.Helper()

	if _, err := runner.Run(context.Background(), parent,
		"-c", "protocol.file.allow=always", "submodule", "add", child, at); err != nil {
		t.Fatalf("submodule add %s in %s: %v", at, parent, err)
	}
	if _, err := runner.Run(context.Background(), parent,
		"-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "-am", "track "+at); err != nil {
		t.Fatalf("commit submodule %s: %v", at, err)
	}
}

func TestDiscoverListsStandaloneCloneOfASubmodule(t *testing.T) {
	registry, root, runner := newRegistry(t)

	standalone := filepath.Join(root, "standalone-sub")
	createRepo(t, runner, standalone)
	commitEmpty(t, runner, standalone, "sub")

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(found.Repos) != 1 || found.Repos[0].Path != standalone {
		t.Fatalf("found = %v, want standalone clone %q", discoveredPaths(found.Repos), standalone)
	}
}

func TestDiscoverReportsCanonicalScannedFrom(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "via-link", "project")
	createRepo(t, runner, path)

	linkParent := filepath.Join(root, "scan-here")
	if err := os.MkdirAll(linkParent, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(linkParent, "projects")
	if err := os.Symlink(filepath.Join(root, "via-link"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{Dir: linkParent})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	resolvedLinkParent, err := filepath.EvalSymlinks(linkParent)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if found.ScannedFrom != resolvedLinkParent {
		t.Errorf("ScannedFrom = %q, want canonical %q", found.ScannedFrom, resolvedLinkParent)
	}
}

// A symlink planted inside the root, pointing at a repository outside it.
//
// The counterpart of TestOpenRejectsSymlinkEscape, for the path that does not
// go through Open: nobody names this directory, the scan finds it. Listing it
// would put a repository from outside the allowed root in front of the user
// with an inviting button beside it, and Open would then — correctly — refuse
// the thing the interface had just offered.
func TestDiscoverDoesNotFollowASymlinkOutOfTheRoot(t *testing.T) {
	registry, root, runner := newRegistry(t)

	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	createRepo(t, runner, filepath.Join(outside, "private"))

	bridge := filepath.Join(root, "bridge")
	if err := os.Symlink(outside, bridge); err != nil {
		t.Fatalf("planting the symlink: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	for _, path := range discoveredPaths(found.Repos) {
		if !strings.HasPrefix(path, root+string(filepath.Separator)) && path != root {
			t.Errorf("scan listed %q, which is outside the root %q", path, root)
		}
		if strings.HasPrefix(path, outside) {
			t.Errorf("scan followed the symlink out to %q", path)
		}
	}
}

func TestDiscoverRejectsPathOutsideRoot(t *testing.T) {
	registry, _, runner := newRegistry(t)

	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	createRepo(t, runner, outside)

	_, err = registry.Discover(context.Background(), repo.DiscoverOptions{Dir: outside})
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("Discover outside = %v, want ErrOutsideRoot", err)
	}
}

func discoveredPaths(found []repo.DiscoveredRepo) []string {
	paths := make([]string, len(found))
	for index, repo := range found {
		paths[index] = repo.Path
	}
	return paths
}

func kindForPath(found []repo.DiscoveredRepo, path string) repo.DiscoveredKind {
	for _, repo := range found {
		if repo.Path == path {
			return repo.Kind
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// What a scan skipped, and why
// ---------------------------------------------------------------------------

// A scan that finds nothing has to be able to say why, and these counts are
// the whole of what it knows.
func TestDiscoverCountsSkippedDirectories(t *testing.T) {
	registry, root, runner := newRegistry(t)

	createRepo(t, runner, filepath.Join(root, ".hidden", "repo"))
	createRepo(t, runner, filepath.Join(root, "node_modules", "repo"))

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(found.Repos) != 0 {
		t.Fatalf("found = %v, want nothing: both repositories sit behind a skipped directory",
			discoveredPaths(found.Repos))
	}
	if found.Skipped.Dotted != 1 {
		t.Errorf("Skipped.Dotted = %d, want 1", found.Skipped.Dotted)
	}
	if found.Skipped.IgnoredName != 1 {
		t.Errorf("Skipped.IgnoredName = %d, want 1", found.Skipped.IgnoredName)
	}
}

// One broken repository moves one counter.
//
// The `.git` inside it is reached by the walk, and counting it as a name the
// scan ignores put a second number on the same directory — the louder of the
// two, whose sentence names node_modules, vendor, target and .cache. None of
// those was skipped, pointing a scan at a `.git` helps nobody, and the one
// true reason sat underneath it.
func TestDiscoverCountsABrokenRepositoryOnce(t *testing.T) {
	registry, root, _ := newRegistry(t)

	// A `.git` nothing wrote into: git knows the name and then refuses to
	// call the directory a repository, which is the reason being counted.
	if err := os.MkdirAll(filepath.Join(root, "broken", ".git"), 0o755); err != nil {
		t.Fatalf("creating the broken repository: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(found.Repos) != 0 {
		t.Fatalf("found = %v, want nothing: git refuses this one", discoveredPaths(found.Repos))
	}
	if found.Skipped.NotARepository != 1 {
		t.Errorf("Skipped.NotARepository = %d, want 1", found.Skipped.NotARepository)
	}
	if found.Skipped.IgnoredName != 0 {
		t.Errorf("Skipped.IgnoredName = %d, want 0: nothing here is named node_modules, vendor, target or .cache",
			found.Skipped.IgnoredName)
	}
	if found.Skipped.Dotted != 0 {
		t.Errorf("Skipped.Dotted = %d, want 0: a .git is not a directory to point a scan into either",
			found.Skipped.Dotted)
	}
}

// The counter says directories, and it has to mean it.
//
// WalkDir reports files too, and SkipDir returned for a file skips every
// remaining entry of its parent. Counting there recorded 1 for a whole level
// of directories, and which one depended on the order the filesystem listed
// them in — a number nobody could act on, presented as a reason.
func TestDiscoverCountsDirectoriesBelowTheLimitAndNotFiles(t *testing.T) {
	registry, root, runner := newRegistry(t)

	createRepo(t, runner, filepath.Join(root, "a", "b", "c", "repo"))
	createRepo(t, runner, filepath.Join(root, "a", "b", "d", "repo"))
	// Sorts before both directories, so a walk that counted files would stop
	// at it and report one.
	if err := os.WriteFile(filepath.Join(root, "a", "b", "a-file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing: %v", err)
	}

	found, err := registry.Discover(context.Background(), repo.DiscoverOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(found.Repos) != 0 {
		t.Fatalf("found = %v, want nothing below depth 2", discoveredPaths(found.Repos))
	}
	if found.Skipped.TooDeep != 2 {
		t.Errorf("Skipped.TooDeep = %d, want 2: the two directories at depth 3, not the file beside them",
			found.Skipped.TooDeep)
	}
}

// The interface draws its depth control from these two numbers, so they have
// to be what the scan applied rather than what it was asked for.
func TestDiscoverReportsTheDepthItApplied(t *testing.T) {
	registry, _, _ := newRegistry(t)
	ctx := context.Background()

	asked, err := registry.Discover(ctx, repo.DiscoverOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if asked.Depth != 2 {
		t.Errorf("Depth = %d, want the 2 that was asked for", asked.Depth)
	}
	if asked.DepthLimit < asked.Depth {
		t.Fatalf("DepthLimit = %d, below the depth %d it caps", asked.DepthLimit, asked.Depth)
	}

	byDefault, err := registry.Discover(ctx, repo.DiscoverOptions{})
	if err != nil {
		t.Fatalf("Discover with no depth: %v", err)
	}
	if byDefault.Depth <= 0 {
		t.Errorf("Depth = %d with no depth asked for, want the default the daemon chose",
			byDefault.Depth)
	}

	capped, err := registry.Discover(ctx, repo.DiscoverOptions{MaxDepth: byDefault.DepthLimit + 1})
	if err != nil {
		t.Fatalf("Discover above the limit: %v", err)
	}
	if capped.Depth != capped.DepthLimit {
		t.Errorf("Depth = %d for a request above the limit, want it capped at %d",
			capped.Depth, capped.DepthLimit)
	}
}

// A superproject whose submodules cannot be listed used to come back as a
// superproject with no submodules — the same answer git gives when there are
// none. The failure now leaves with the result, and the rest of the scan
// still arrives: one broken .gitmodules must not hide every other repository
// on the disk.
func TestDiscoverReportsASubmoduleListingFailure(t *testing.T) {
	registry, root, runner := newRegistry(t)

	child := filepath.Join(root, "child")
	createRepo(t, runner, child)
	commitEmpty(t, runner, child, "child")

	superPath := filepath.Join(root, "super")
	createRepo(t, runner, superPath)
	commitEmpty(t, runner, superPath, "super")
	addSubmodule(t, runner, superPath, child, "sub")

	// git parses .gitmodules before it runs foreach over an initialised
	// submodule, and refuses a file it cannot parse.
	if err := os.WriteFile(filepath.Join(superPath, ".gitmodules"), []byte("nonsense\n"), 0o600); err != nil {
		t.Fatalf("writing .gitmodules: %v", err)
	}

	found, err := registry.Discover(context.Background(),
		repo.DiscoverOptions{IncludeSubmodules: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	paths := discoveredPaths(found.Repos)
	if !slices.Contains(paths, superPath) || !slices.Contains(paths, child) {
		t.Fatalf("paths = %v, want both %q and %q: one failure must not empty the list",
			paths, superPath, child)
	}

	if len(found.SubmoduleFailures) != 1 {
		t.Fatalf("SubmoduleFailures = %v, want exactly the superproject", found.SubmoduleFailures)
	}
	failure := found.SubmoduleFailures[0]
	if failure.Path != superPath {
		t.Errorf("failure path = %q, want %q", failure.Path, superPath)
	}

	var gitError *git.Error
	if !errors.As(failure.Err, &gitError) {
		t.Fatalf("failure = %v, want a *git.Error carrying the command, the exit code and stderr",
			failure.Err)
	}
	if strings.TrimSpace(gitError.Stderr) == "" {
		t.Error("the failure carries no stderr, which is the half of it a user can act on")
	}
	if !strings.Contains(gitError.CommandLine(), "submodule foreach") {
		t.Errorf("command = %q, want the submodule listing that failed", gitError.CommandLine())
	}
}

// The pass has nothing to report when nothing failed, and the field stays
// empty rather than growing an entry per repository it looked at.
func TestDiscoverReportsNoFailureWhenSubmodulesList(t *testing.T) {
	registry, root, runner := newRegistry(t)

	child := filepath.Join(root, "child")
	createRepo(t, runner, child)
	commitEmpty(t, runner, child, "child")

	superPath := filepath.Join(root, "super")
	createRepo(t, runner, superPath)
	commitEmpty(t, runner, superPath, "super")
	addSubmodule(t, runner, superPath, child, "sub")

	found, err := registry.Discover(context.Background(),
		repo.DiscoverOptions{IncludeSubmodules: true})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found.SubmoduleFailures) != 0 {
		t.Errorf("SubmoduleFailures = %v, want none", found.SubmoduleFailures)
	}
}
