package git_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

// logRecord assembles a record the way the format string does: NUL-separated
// fields, terminated by NUL and a newline.
func logRecord(fields ...string) string {
	return strings.Join(fields, "\x00") + "\x00\n"
}

// logOutput assembles a full output. git inserts a newline between records,
// and that detail is exactly what the parsing has to absorb without
// mistaking it for a separator.
func logOutput(records ...string) []byte {
	return []byte(strings.Join(records, "\n"))
}

func TestParseLogEmptyOutput(t *testing.T) {
	commits, err := git.ParseLog(nil)
	if err != nil {
		t.Fatalf("empty output must not be an error: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected 0 commits, got %d", len(commits))
	}
}

func TestParseLogRootCommit(t *testing.T) {
	output := logOutput(logRecord(
		"a1b2c3", "", "Ada Lovelace", "2024-03-01T10:00:00+01:00", "first commit", ""))

	commits, err := git.ParseLog(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d", len(commits))
	}

	commit := commits[0]
	if commit.SHA != "a1b2c3" {
		t.Errorf("SHA = %q, expected a1b2c3", commit.SHA)
	}
	if len(commit.Parents) != 0 {
		t.Errorf("a root commit has no parent, got %v", commit.Parents)
	}
	if commit.Author != "Ada Lovelace" {
		t.Errorf("author = %q", commit.Author)
	}
	if len(commit.Refs) != 0 {
		t.Errorf("expected no decoration, got %v", commit.Refs)
	}

	expected := time.Date(2024, 3, 1, 10, 0, 0, 0, time.FixedZone("", 3600))
	if !commit.Date.Equal(expected) {
		t.Errorf("date = %v, expected %v", commit.Date, expected)
	}
}

func TestParseLogMergeAndRefDecoration(t *testing.T) {
	output := logOutput(
		logRecord("ffff", "aaaa bbbb", "Grace Hopper", "2024-03-02T09:00:00Z",
			"Merge branch 'feature'", "HEAD -> main, origin/main, tag: v1.0"),
		logRecord("aaaa", "cccc", "Alan Turing", "2024-03-01T09:00:00Z",
			"work on the branch", ""),
	)

	commits, err := git.ParseLog(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}

	merge := commits[0]
	if len(merge.Parents) != 2 || merge.Parents[0] != "aaaa" || merge.Parents[1] != "bbbb" {
		t.Errorf("parents = %v, expected [aaaa bbbb]", merge.Parents)
	}

	expectedRefs := []string{"HEAD -> main", "origin/main", "tag: v1.0"}
	if len(merge.Refs) != len(expectedRefs) {
		t.Fatalf("decoration = %v, expected %v", merge.Refs, expectedRefs)
	}
	for index, ref := range expectedRefs {
		if merge.Refs[index] != ref {
			t.Errorf("decoration[%d] = %q, expected %q", index, merge.Refs[index], ref)
		}
	}

	// The second record leads with the newline git inserted; if it were not
	// stripped, the SHA would come out prefixed with "\n".
	if commits[1].SHA != "aaaa" {
		t.Errorf("SHA of the second commit = %q, expected aaaa", commits[1].SHA)
	}
}

// TestParseLogSubjectWithNewline locks down the property that makes this
// format extensible: the parser leans on the record terminator, not on
// newlines. git produces none in the current fields, but it will as soon as
// the message body is added — and this test will then fail if someone has
// "simplified" the splitting in the meantime.
func TestParseLogSubjectWithNewline(t *testing.T) {
	subject := "fix the parser\nrest of the message"
	output := logOutput(logRecord(
		"deadbeef", "cafe", "Barbara Liskov", "2024-03-03T12:00:00Z", subject, ""))

	commits, err := git.ParseLog(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected 1 commit, got %d — the record terminator did not do its job", len(commits))
	}
	if commits[0].Subject != subject {
		t.Errorf("subject = %q, expected %q", commits[0].Subject, subject)
	}
}

func TestParseLogSubjectWithTrickyCharacters(t *testing.T) {
	subject := `feat(api): handles “quotes”, $variables and 🌳`
	output := logOutput(logRecord(
		"1234", "", "Ada", "2024-03-04T12:00:00Z", subject, ""))

	commits, err := git.ParseLog(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if commits[0].Subject != subject {
		t.Errorf("subject = %q, expected %q", commits[0].Subject, subject)
	}
}

func TestParseLogWrongFieldCount(t *testing.T) {
	output := logOutput("far\x00too\x00few\x00fields\x00\n")

	_, err := git.ParseLog(output)
	if err == nil {
		t.Fatal("a truncated record must produce an error, not an empty commit")
	}
	if !strings.Contains(err.Error(), "fields") {
		t.Errorf("the error must name the problem, got: %v", err)
	}
}

func TestParseLogUnreadableDate(t *testing.T) {
	output := logOutput(logRecord("1234", "", "Ada", "yesterday morning", "subject", ""))

	_, err := git.ParseLog(output)
	if err == nil {
		t.Fatal("an unreadable date must produce an error")
	}
	if !strings.Contains(err.Error(), "yesterday morning") {
		t.Errorf("the error must quote the offending value, got: %v", err)
	}
}
