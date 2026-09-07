package git_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestParseBlameReadsLinePorcelain(t *testing.T) {
	// Two lines from one commit: --line-porcelain repeats the fields.
	output := []byte("" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1 1 2\n" +
		"author Ada Lovelace\n" +
		"author-mail <ada@example.com>\n" +
		"author-time 1000000000\n" +
		"author-tz +0000\n" +
		"committer Ada Lovelace\n" +
		"committer-mail <ada@example.com>\n" +
		"committer-time 1000000000\n" +
		"committer-tz +0000\n" +
		"summary first line\n" +
		"filename notes.md\n" +
		"\thello\n" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 2 2\n" +
		"author Ada Lovelace\n" +
		"author-mail <ada@example.com>\n" +
		"author-time 1000000000\n" +
		"author-tz +0000\n" +
		"committer Ada Lovelace\n" +
		"committer-mail <ada@example.com>\n" +
		"committer-time 1000000000\n" +
		"committer-tz +0000\n" +
		"summary first line\n" +
		"filename notes.md\n" +
		"\tworld\n")

	lines, err := git.ParseBlame(output)
	if err != nil {
		t.Fatalf("ParseBlame: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines", len(lines))
	}
	if lines[0].Number != 1 || lines[0].Text != "hello" || lines[0].Author != "Ada Lovelace" {
		t.Errorf("line 0 = %+v", lines[0])
	}
	if lines[0].Date.UTC() != time.Unix(1000000000, 0).UTC() {
		t.Errorf("date = %v", lines[0].Date)
	}
	if lines[1].Number != 2 || lines[1].Text != "world" || lines[1].Subject != "first line" {
		t.Errorf("line 1 = %+v", lines[1])
	}
}

func TestParseBlameRefusesABrokenHeader(t *testing.T) {
	_, err := git.ParseBlame([]byte("not-a-header\n\ttext\n"))
	if err == nil {
		t.Fatal("accepted a broken header")
	}
}

func TestBlameAttributesLinesToCommits(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "notes.md", "one\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "add one")
	first := shaOf(t, runner, dir, "HEAD")

	write(t, dir, "notes.md", "one\ntwo\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "add two")
	second := shaOf(t, runner, dir, "HEAD")

	result, err := runner.Blame(ctx, dir, "HEAD", "notes.md")
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	if len(result.Lines) != 2 {
		t.Fatalf("lines = %d", len(result.Lines))
	}
	if result.Lines[0].SHA != first || result.Lines[0].Text != "one" {
		t.Errorf("line 1 = %+v, want sha %s", result.Lines[0], first)
	}
	if result.Lines[1].SHA != second || result.Lines[1].Text != "two" {
		t.Errorf("line 2 = %+v, want sha %s", result.Lines[1], second)
	}
	if !strings.Contains(result.Lines[1].Subject, "add two") {
		t.Errorf("subject = %q", result.Lines[1].Subject)
	}
}

func TestBlameRefusesAnEmptyPath(t *testing.T) {
	dir := initRepo(t)
	_, err := git.NewRunner(nil).Blame(context.Background(), dir, "HEAD", "")
	if !errors.Is(err, git.ErrNoPaths) {
		t.Fatalf("err = %v, want ErrNoPaths", err)
	}
}
