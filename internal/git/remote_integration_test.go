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

// The network commands, against the real binary and a repository on the same
// disk.
//
// A local path is a remote as far as git is concerned: the same refspecs, the
// same fast-forward rules, the same refusals. What it is not is a network, so
// nothing here can go slow or flaky, and everything about the argument lists
// that this file exists to check is exercised exactly as it would be over ssh.
//
// The claims being checked are all of the form "git does this with the string
// we hand it", and none of them can be checked any other way: that a branch
// following a differently-named upstream pushes to that name and creates no
// second branch, that --prune deletes a remote-tracking branch whose branch is
// gone, that --force-with-lease refuses when the remote moved underneath.

// clonedPair builds a bare repository and a clone of it, both with one commit.
//
//	server.git   A          ← bare, the "other machine"
//	work         A          ← on main, following origin/main
func clonedPair(t *testing.T) (server, work string, runner *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	root := t.TempDir()
	server = filepath.Join(root, "server.git")
	work = filepath.Join(root, "work")
	runner = git.NewRunner(nil)

	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	runGit(t, runner, root, "init", "--bare", "-b", "main", server)
	runGit(t, runner, work, "init", "-b", "main")
	// In the repository's own configuration rather than on the command line:
	// a merge started by `git pull` writes a commit, and the Runner does not
	// carry the -c arguments the other helpers here pass.
	runGit(t, runner, work, "config", "user.name", "yagit Test")
	runGit(t, runner, work, "config", "user.email", "test@yagit.local")
	runGit(t, runner, work, "remote", "add", "origin", server)

	commitEmpty(t, runner, work, "A")
	runGit(t, runner, work, "push", "--set-upstream", "origin", "main:refs/heads/main")

	return server, work, runner
}

// secondClone is another checkout of the same server: somebody else's machine.
func secondClone(t *testing.T, runner *git.Runner, server string) string {
	t.Helper()

	other := filepath.Join(t.TempDir(), "other")
	runGit(t, runner, filepath.Dir(other), "clone", server, other)
	runGit(t, runner, other, "config", "user.name", "Somebody Else")
	runGit(t, runner, other, "config", "user.email", "else@yagit.local")
	return other
}

func shaOf(t *testing.T, runner *git.Runner, dir, revision string) string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "rev-parse", "--verify", revision)
	if err != nil {
		t.Fatalf("rev-parse %s in %s: %v", revision, dir, err)
	}
	return strings.TrimSpace(string(output))
}

func TestRemotesReadsBothURLs(t *testing.T) {
	_, work, runner := clonedPair(t)
	runGit(t, runner, work, "config", "remote.origin.pushurl", "https://ada:secret@example.test/x.git")

	remotes, err := runner.Remotes(context.Background(), work)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 1 || remotes[0].Name != "origin" {
		t.Fatalf("Remotes = %+v, want one called origin", remotes)
	}
	if remotes[0].PushURL != "https://ada:***@example.test/x.git" {
		t.Errorf("push URL is %q; the password must not leave this package", remotes[0].PushURL)
	}
	if remotes[0].FetchURL == remotes[0].PushURL {
		t.Error("the two URLs came back identical, so pushurl was not read")
	}
}

func TestUpstreamOfReadsWhatABranchFollows(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if upstream.Remote != "origin" || upstream.Ref != "refs/heads/main" {
		t.Errorf("UpstreamOf = %+v, want origin refs/heads/main", upstream)
	}

	// A branch that follows nothing is a state, not a failure: it is every
	// branch anybody has just created, and the interface offers to publish it.
	runGit(t, runner, work, "branch", "solo")
	solo, err := runner.UpstreamOf(ctx, work, "solo")
	if err != nil {
		t.Fatalf("UpstreamOf(solo): %v", err)
	}
	if solo.Configured() {
		t.Errorf("UpstreamOf(solo) = %+v, want nothing", solo)
	}

	if _, err := runner.UpstreamOf(ctx, work, ""); !errors.Is(err, git.ErrDetachedHEAD) {
		t.Errorf("UpstreamOf(\"\") = %v, want ErrDetachedHEAD", err)
	}
}

// The crown of the file. A branch called main that follows origin/trunk pushes
// to trunk — and `git push origin main`, which looks like the same request,
// would create a second branch on the server and push to that one forever.
func TestPushFollowsTheUpstreamsNameRatherThanTheBranchs(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	// main now follows origin/trunk: the arrangement anybody has who renamed a
	// branch on the server, or who works on a fork.
	runGit(t, runner, work, "push", "--set-upstream", "origin", "main:refs/heads/trunk")
	commitEmpty(t, runner, work, "B")

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	destination, err := git.DestinationFor("main", upstream, "")
	if err != nil {
		t.Fatalf("DestinationFor: %v", err)
	}
	if err := runner.Push(ctx, work, destination, false, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if got, want := shaOf(t, runner, server, "refs/heads/trunk"), shaOf(t, runner, work, "main"); got != want {
		t.Errorf("trunk on the server is %s, want %s", got, want)
	}

	// And nothing was created beside it. The server had refs/heads/main from
	// the fixture; what must not have happened is a push to it.
	if got, want := shaOf(t, runner, server, "refs/heads/main"), shaOf(t, runner, work, "HEAD~1"); got != want {
		t.Errorf("main on the server moved to %s; the push should have gone to trunk alone", got)
	}
}

func TestPushPublishesABranchAndRecordsWhereItWent(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	runGit(t, runner, work, "switch", "-c", "feature")
	commitEmpty(t, runner, work, "F")

	destination, err := git.DestinationFor("feature", git.Upstream{}, "origin")
	if err != nil {
		t.Fatalf("DestinationFor: %v", err)
	}
	if err := runner.Push(ctx, work, destination, false, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if got, want := shaOf(t, runner, server, "refs/heads/feature"), shaOf(t, runner, work, "feature"); got != want {
		t.Errorf("feature on the server is %s, want %s", got, want)
	}

	// --set-upstream is the half that makes the next push and the next pull
	// need no decision at all.
	upstream, err := runner.UpstreamOf(ctx, work, "feature")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if upstream.Remote != "origin" || upstream.Ref != "refs/heads/feature" {
		t.Errorf("feature follows %+v, want origin refs/heads/feature", upstream)
	}
}

func TestFetchPrunesABranchThatIsGoneFromTheServer(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	runGit(t, runner, work, "push", "origin", "main:refs/heads/doomed")
	if err := runner.Fetch(ctx, work, "origin", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/remotes/origin/doomed"); err != nil {
		t.Fatalf("the fetch did not bring back origin/doomed: %v", err)
	}

	runGit(t, runner, server, "update-ref", "-d", "refs/heads/doomed")

	if err := runner.Fetch(ctx, work, "origin", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// Without --prune the row stays in the sidebar forever, offered for
	// checkout, naming a branch nobody can reach.
	if _, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/remotes/origin/doomed"); err == nil {
		t.Error("origin/doomed survived the fetch, so --prune did not reach git")
	}
}

func TestFetchWithNoRemoteNamedReachesAllOfThem(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	// A second remote, which the fetch must reach without being told to.
	mirror := filepath.Join(t.TempDir(), "mirror.git")
	runGit(t, runner, filepath.Dir(mirror), "clone", "--bare", server, mirror)
	runGit(t, runner, work, "remote", "add", "mirror", mirror)

	if err := runner.Fetch(ctx, work, "", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/remotes/mirror/main"); err != nil {
		t.Errorf("mirror/main is not here, so --all did not reach git: %v", err)
	}
}

func TestPullFastForwardMovesTheBranchUp(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	other := secondClone(t, runner, server)
	commitEmpty(t, runner, other, "B from elsewhere")
	runGit(t, runner, other, "push", "origin", "main:refs/heads/main")

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if err := runner.Pull(ctx, work, upstream, git.PullFastForward, nil); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	if got, want := shaOf(t, runner, work, "main"), shaOf(t, runner, server, "refs/heads/main"); got != want {
		t.Errorf("main is at %s after the pull, want %s", got, want)
	}
}

func TestPullFastForwardRefusesDivergedBranchesAndSaysSo(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	other := secondClone(t, runner, server)
	commitEmpty(t, runner, other, "theirs")
	runGit(t, runner, other, "push", "origin", "main:refs/heads/main")
	commitEmpty(t, runner, work, "ours")

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}

	mine := shaOf(t, runner, work, "main")
	err = runner.Pull(ctx, work, upstream, git.PullFastForward, nil)
	if err == nil {
		t.Fatal("the pull succeeded; --ff-only must refuse two branches that have diverged")
	}

	// git's own refusal, whole. It is what the interface shows, and it is what
	// turns the merge and rebase items in the menu into a question worth
	// putting rather than a pair of buttons nobody read.
	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("Pull failed with %T, want a *git.Error carrying git's account", err)
	}
	if !strings.Contains(failure.Stderr, "Not possible to fast-forward") &&
		!strings.Contains(failure.Stderr, "diverging") {
		t.Errorf("stderr does not explain the refusal: %q", failure.Stderr)
	}
	if got := shaOf(t, runner, work, "main"); got != mine {
		t.Errorf("main moved to %s during a refused pull, want %s", got, mine)
	}
}

func TestPullRebaseReplaysTheLocalCommitOnTop(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	other := secondClone(t, runner, server)
	commitEmpty(t, runner, other, "theirs")
	runGit(t, runner, other, "push", "origin", "main:refs/heads/main")
	commitEmpty(t, runner, work, "ours")

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if err := runner.Pull(ctx, work, upstream, git.PullRebase, nil); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	// Ours on top of theirs, and no merge commit: HEAD's parent is what the
	// server had.
	if got, want := shaOf(t, runner, work, "HEAD~1"), shaOf(t, runner, server, "refs/heads/main"); got != want {
		t.Errorf("HEAD~1 is %s, want the server's tip %s", got, want)
	}
}

func TestPullMergeBringsTheUpstreamInAsAMerge(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	other := secondClone(t, runner, server)
	commitEmpty(t, runner, other, "theirs")
	runGit(t, runner, other, "push", "origin", "main:refs/heads/main")
	commitEmpty(t, runner, work, "ours")

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if err := runner.Pull(ctx, work, upstream, git.PullMerge, nil); err != nil {
		t.Fatalf("Pull: %v", err)
	}

	commits, err := runner.Run(ctx, work, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	// A merge commit is a sha and two parents.
	if fields := strings.Fields(string(commits)); len(fields) != 3 {
		t.Errorf("HEAD has %d parents, want the two of a merge: %q", len(fields)-1, commits)
	}
}

// The reason the force flags are two rather than one.
//
// --force-with-lease alone holds its lease against the remote-tracking ref,
// and a fetch moves that ref without anybody reading what arrived — after
// which the lease is held against somebody else's commit and the force
// succeeds over work nobody has seen. --force-if-includes is what requires the
// overwritten commit to actually be in this branch's history.
func TestForcePushRefusesWhenTheRemoteMovedUnderneath(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	commitEmpty(t, runner, work, "B")
	runGit(t, runner, work, "push", "origin", "main:refs/heads/main")

	other := secondClone(t, runner, server)
	commitEmpty(t, runner, other, "theirs, and nobody here has read it")
	runGit(t, runner, other, "push", "origin", "main:refs/heads/main")

	// The rewrite that makes this a force push, and the fetch that quietly
	// moves the lease onto the commit somebody else just pushed.
	runGit(t, runner, work, "reset", "--hard", "HEAD~1")
	commitEmpty(t, runner, work, "B, reworded")
	if err := runner.Fetch(ctx, work, "origin", nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	theirs := shaOf(t, runner, server, "refs/heads/main")
	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	destination, err := git.DestinationFor("main", upstream, "")
	if err != nil {
		t.Fatalf("DestinationFor: %v", err)
	}

	if err := runner.Push(ctx, work, destination, true, nil); err == nil {
		t.Fatal("the force push was accepted, and somebody else's commit is gone")
	}
	if got := shaOf(t, runner, server, "refs/heads/main"); got != theirs {
		t.Errorf("the server is at %s, want the commit it had, %s", got, theirs)
	}
}

func TestForcePushGoesThroughOnceTheRewriteIncludesWhatWasThere(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	commitEmpty(t, runner, work, "B")
	runGit(t, runner, work, "push", "origin", "main:refs/heads/main")

	// The ordinary reason to force: the same commits, said differently.
	runGit(t, runner, work, "reset", "--hard", "HEAD~1")
	commitEmpty(t, runner, work, "B, reworded")

	upstream, err := runner.UpstreamOf(ctx, work, "main")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	destination, err := git.DestinationFor("main", upstream, "")
	if err != nil {
		t.Fatalf("DestinationFor: %v", err)
	}
	if err := runner.Push(ctx, work, destination, true, nil); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if got, want := shaOf(t, runner, server, "refs/heads/main"), shaOf(t, runner, work, "main"); got != want {
		t.Errorf("the server is at %s, want %s", got, want)
	}
}

func TestPushRefusesWithoutSomewhereToSend(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	for name, destination := range map[string]git.PushDestination{
		"no remote": {Branch: "main", Ref: "refs/heads/main"},
		"no branch": {Remote: "origin", Ref: "refs/heads/main"},
		"no ref":    {Remote: "origin", Branch: "main"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runner.Push(ctx, work, destination, false, nil); err == nil {
				t.Error("push was attempted with an incomplete destination")
			}
		})
	}
}

func TestAddRenameRemoveRemote(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	mirror := filepath.Join(t.TempDir(), "mirror.git")
	runGit(t, runner, filepath.Dir(mirror), "init", "--bare", "-b", "main", mirror)

	if err := runner.AddRemote(ctx, work, "mirror", mirror); err != nil {
		t.Fatalf("AddRemote: %v", err)
	}
	remotes, err := runner.Remotes(ctx, work)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 2 {
		t.Fatalf("after add: %+v, want origin and mirror", remotes)
	}

	if err := runner.RenameRemote(ctx, work, "mirror", "backup"); err != nil {
		t.Fatalf("RenameRemote: %v", err)
	}
	if _, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/remotes/origin/main"); err != nil {
		t.Fatalf("origin/main should still be there: %v", err)
	}

	if err := runner.RemoveRemote(ctx, work, "backup"); err != nil {
		t.Fatalf("RemoveRemote: %v", err)
	}
	remotes, err = runner.Remotes(ctx, work)
	if err != nil {
		t.Fatalf("Remotes after remove: %v", err)
	}
	if len(remotes) != 1 || remotes[0].Name != "origin" {
		t.Errorf("after remove: %+v, want only origin", remotes)
	}
}

func TestRemoteManagementRefusesEmptyFields(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	for name, err := range map[string]error{
		"add-name":    runner.AddRemote(ctx, work, "  ", "https://example.test/x.git"),
		"add-url":     runner.AddRemote(ctx, work, "fork", ""),
		"rename-from": runner.RenameRemote(ctx, work, "", "upstream"),
		"rename-to":   runner.RenameRemote(ctx, work, "origin", " "),
		"remove":      runner.RemoveRemote(ctx, work, ""),
	} {
		t.Run(name, func(t *testing.T) {
			if err == nil {
				t.Fatal("accepted an empty field")
			}
			if !errors.Is(err, git.ErrNoRemote) && !errors.Is(err, git.ErrNoRemoteURL) {
				t.Errorf("got %v, want ErrNoRemote or ErrNoRemoteURL", err)
			}
		})
	}
}

func TestSetRemoteURLChangesWhereFetchComesFrom(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	alternate := filepath.Join(t.TempDir(), "alternate.git")
	runGit(t, runner, filepath.Dir(alternate), "init", "--bare", "-b", "main", alternate)

	if err := runner.SetRemoteURL(ctx, work, "origin", alternate); err != nil {
		t.Fatalf("SetRemoteURL: %v", err)
	}
	remotes, err := runner.Remotes(ctx, work)
	if err != nil {
		t.Fatalf("Remotes: %v", err)
	}
	if len(remotes) != 1 || remotes[0].FetchURL != alternate {
		t.Errorf("after set-url: %+v, want origin fetching from %q", remotes, alternate)
	}
}

func TestSetUpstreamRecordsWhatABranchFollows(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	runGit(t, runner, work, "branch", "solo")
	if err := runner.SetUpstream(ctx, work, "solo", "origin", "main"); err != nil {
		t.Fatalf("SetUpstream: %v", err)
	}
	upstream, err := runner.UpstreamOf(ctx, work, "solo")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if upstream.Remote != "origin" || upstream.Ref != "refs/heads/main" {
		t.Errorf("solo follows %+v, want origin refs/heads/main", upstream)
	}
}

func TestUnsetUpstreamForgetsWhatABranchFollows(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	runGit(t, runner, work, "branch", "solo")
	if err := runner.SetUpstream(ctx, work, "solo", "origin", "main"); err != nil {
		t.Fatalf("SetUpstream: %v", err)
	}
	if err := runner.UnsetUpstream(ctx, work, "solo"); err != nil {
		t.Fatalf("UnsetUpstream: %v", err)
	}
	upstream, err := runner.UpstreamOf(ctx, work, "solo")
	if err != nil {
		t.Fatalf("UpstreamOf: %v", err)
	}
	if upstream.Configured() {
		t.Errorf("solo still follows %+v after unset", upstream)
	}
}

func TestRemoveRemoteDropsItsTrackingBranches(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	if _, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/remotes/origin/main"); err != nil {
		t.Fatalf("fixture needs origin/main: %v", err)
	}
	if err := runner.RemoveRemote(ctx, work, "origin"); err != nil {
		t.Fatalf("RemoveRemote: %v", err)
	}
	if _, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/remotes/origin/main"); err == nil {
		t.Error("origin/main survived removing origin")
	}
}
