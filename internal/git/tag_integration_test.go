package git_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestCreateTagArgsPutsTheNameAfterTheDashDash(t *testing.T) {
	args := git.CreateTagArgs("-d", "release", "HEAD", true)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-- -d") {
		t.Fatalf("CreateTagArgs = %v; name must sit after --", args)
	}
	if args[0] != "tag" || args[1] != "-a" || args[2] != "-m" {
		t.Fatalf("CreateTagArgs = %v; want tag -a -m …", args)
	}
}

func TestCreateTagArgsLightweightOmitsMessageFlags(t *testing.T) {
	args := git.CreateTagArgs("v1", "ignored", "", false)
	want := "tag -- v1"
	if strings.Join(args, " ") != want {
		t.Fatalf("CreateTagArgs = %v, want %s", args, want)
	}
}

func TestDeleteTagArgsPutsTheNameAfterTheDashDash(t *testing.T) {
	args := git.DeleteTagArgs("-f")
	if strings.Join(args, " ") != "tag -d -- -f" {
		t.Fatalf("DeleteTagArgs = %v", args)
	}
}

func TestCreateTagRecordsAnAnnotatedTag(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	if err := runner.CreateTag(context.Background(), dir, "v1", "first release", "", true); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	refs, err := runner.ForEachRef(context.Background(), dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	found := false
	for _, ref := range refs {
		if ref.Kind == git.RefTag && ref.ShortName == "v1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("refs = %+v, want tag v1", refs)
	}
}

func TestCreateTagRecordsALightweightTag(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	if err := runner.CreateTag(context.Background(), dir, "v1", "", "", false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// A lightweight tag's object is the commit itself — no tag object.
	output, err := runner.Run(context.Background(), dir, "cat-file", "-t", "refs/tags/v1")
	if err != nil {
		t.Fatalf("cat-file: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "commit" {
		t.Fatalf("tag object type = %q, want commit (lightweight)", got)
	}
}

func TestCreateTagRefusesAnEmptyMessage(t *testing.T) {
	dir := initRepo(t)
	err := git.NewRunner(nil).CreateTag(context.Background(), dir, "v1", "  ", "", true)
	if !errors.Is(err, git.ErrNoTagMessage) {
		t.Fatalf("error = %v, want ErrNoTagMessage", err)
	}
}

func TestCreateTagLightweightAllowsAnEmptyMessage(t *testing.T) {
	dir := initRepo(t)
	if err := git.NewRunner(nil).CreateTag(context.Background(), dir, "v1", "", "", false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
}

func TestDeleteTagRemovesIt(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	if err := runner.CreateTag(context.Background(), dir, "v1", "first", "", true); err != nil {
		t.Fatal(err)
	}
	if err := runner.DeleteTag(context.Background(), dir, "v1"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	refs, err := runner.ForEachRef(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if ref.Kind == git.RefTag && ref.ShortName == "v1" {
			t.Fatal("tag v1 was still listed after delete")
		}
	}
}

func TestPushTagArgsWritesTheFullRefspec(t *testing.T) {
	args := git.PushTagArgs("origin", "v1.0.0")
	want := "push -- origin refs/tags/v1.0.0:refs/tags/v1.0.0"
	if strings.Join(args, " ") != want {
		t.Fatalf("PushTagArgs = %v, want %s", args, want)
	}
}

func TestPushTagSendsTheTagToTheRemote(t *testing.T) {
	server, work, runner := clonedPair(t)
	ctx := context.Background()

	if err := runner.CreateTag(ctx, work, "v1.0.0", "first release", "", true); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if err := runner.PushTag(ctx, work, "origin", "v1.0.0"); err != nil {
		t.Fatalf("PushTag: %v", err)
	}

	output, err := runner.Run(ctx, server, "rev-parse", "--verify", "refs/tags/v1.0.0")
	if err != nil {
		t.Fatalf("tag missing on server: %v", err)
	}
	local, err := runner.Run(ctx, work, "rev-parse", "--verify", "refs/tags/v1.0.0")
	if err != nil {
		t.Fatalf("local tag: %v", err)
	}
	if strings.TrimSpace(string(output)) != strings.TrimSpace(string(local)) {
		t.Errorf("server has %s, work has %s", output, local)
	}
}

func TestPushTagRefusesEmptyFields(t *testing.T) {
	_, work, runner := clonedPair(t)
	ctx := context.Background()

	for name, err := range map[string]error{
		"no remote": runner.PushTag(ctx, work, "", "v1"),
		"no name":   runner.PushTag(ctx, work, "origin", "  "),
	} {
		t.Run(name, func(t *testing.T) {
			if err == nil {
				t.Fatal("accepted an empty field")
			}
			if !errors.Is(err, git.ErrNoRemote) && !errors.Is(err, git.ErrNoTagName) {
				t.Errorf("got %v", err)
			}
		})
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	isolateGitConfiguration(t)
	dir := filepath.Join(t.TempDir(), "repo")
	runner := git.NewRunner(nil)
	runGit(t, runner, filepath.Dir(dir), "init", "-b", "main", dir)
	runGit(t, runner, dir, "config", "user.name", "yagit Test")
	runGit(t, runner, dir, "config", "user.email", "test@yagit.local")
	commitEmpty(t, runner, dir, "A")
	return dir
}
