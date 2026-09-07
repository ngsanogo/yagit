package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// prepared is what the commit box would open on, for a repository in whatever
// state the test has put it in.
func prepared(t *testing.T, runner *git.Runner, dir string, amend bool) (string, git.MessageSource) {
	t.Helper()
	text, source, err := runner.PreparedMessage(
		context.Background(), dir, filepath.Join(dir, ".git"), amend)
	if err != nil {
		t.Fatalf("PreparedMessage: %v", err)
	}
	return text, source
}

func TestPreparedMessageIsNothingWhenNothingPreparedOne(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	text, source := prepared(t, runner, dir, false)
	if text != "" || source != git.SourceNone {
		t.Errorf("PreparedMessage = %q from %q, want nothing at all", text, source)
	}
}

func TestPreparedMessageReadsTheConfiguredTemplate(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	template := filepath.Join(dir, ".gitmessage")
	if err := os.WriteFile(template,
		[]byte("\n# Why, not what.\nRefs: \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, runner, dir, "config", "commit.template", template)

	text, source := prepared(t, runner, dir, false)
	if source != git.SourceTemplate {
		t.Errorf("source = %q, want %q", source, git.SourceTemplate)
	}
	// The comment is gone, because `git commit` would strip it too — the box
	// shows what would be committed, not what is in the file.
	if text != "Refs:" {
		t.Errorf("text = %q, want the template with its comment stripped", text)
	}
}

// A relative path is resolved against the work tree, which is how git reads
// it. A daemon that resolved it against its own working directory would fail
// on the ordinary case and say the file does not exist.
func TestPreparedMessageResolvesARelativeTemplateAgainstTheWorkTree(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	if err := os.WriteFile(filepath.Join(dir, ".gitmessage"), []byte("subject\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, runner, dir, "config", "commit.template", ".gitmessage")

	if text, source := prepared(t, runner, dir, false); text != "subject" || source != git.SourceTemplate {
		t.Errorf("PreparedMessage = %q from %q, want the template", text, source)
	}
}

// A template that is nothing but comments proposes nothing, and labelling an
// empty box as git's words would be a sentence about text that is not there.
func TestPreparedMessageIgnoresATemplateOfNothingButComments(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	template := filepath.Join(dir, ".gitmessage")
	if err := os.WriteFile(template, []byte("# a reminder\n# and another\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, runner, dir, "config", "commit.template", template)

	if text, source := prepared(t, runner, dir, false); text != "" || source != git.SourceNone {
		t.Errorf("PreparedMessage = %q from %q, want nothing", text, source)
	}
}

// A configured file that is not there is a failure and not silence — it is
// what `git commit` itself does, and a box that opened empty would leave
// somebody wondering where their template went.
func TestPreparedMessageFailsOnATemplateThatIsNotThere(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "config", "commit.template", filepath.Join(dir, "no-such-file"))

	_, _, err := runner.PreparedMessage(
		context.Background(), dir, filepath.Join(dir, ".git"), false)
	if err == nil {
		t.Fatal("a template that cannot be read was passed over in silence")
	}
}

// An amend is a different question: the message of the commit being replaced,
// and never a skeleton offered over the top of it.
func TestPreparedMessageOnAnAmendIsTheCommitBeingReplaced(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	template := filepath.Join(dir, ".gitmessage")
	if err := os.WriteFile(template, []byte("a skeleton\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, runner, dir, "config", "commit.template", template)

	if text, source := prepared(t, runner, dir, true); text != "A" || source != git.SourceHead {
		t.Errorf("PreparedMessage(amend) = %q from %q, want the tip's own message", text, source)
	}
}

func TestWillSignCommitsReadsTheConfiguration(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	// Unset is the ordinary case, and `git config --get` says so with exit
	// code 1 and an empty stderr. Reading that as a failure would put a
	// refusal under every commit box on every machine.
	signing, err := runner.WillSignCommits(ctx, dir)
	if err != nil {
		t.Fatalf("WillSignCommits with nothing configured: %v", err)
	}
	if signing {
		t.Error("a repository that configures nothing was reported as signing")
	}

	// `--type=bool` is what makes git normalise its own spellings rather than
	// this parsing a boolean git already knows how to read.
	for _, spelling := range []string{"true", "yes", "on", "1"} {
		runGit(t, runner, dir, "config", "commit.gpgsign", spelling)
		signing, err := runner.WillSignCommits(ctx, dir)
		if err != nil {
			t.Fatalf("WillSignCommits with commit.gpgsign=%s: %v", spelling, err)
		}
		if !signing {
			t.Errorf("commit.gpgsign=%s was read as off", spelling)
		}
	}

	runGit(t, runner, dir, "config", "commit.gpgsign", "false")
	if signing, err := runner.WillSignCommits(ctx, dir); err != nil || signing {
		t.Errorf("commit.gpgsign=false: signing=%v, err=%v", signing, err)
	}
}
