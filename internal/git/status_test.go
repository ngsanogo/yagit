package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// statusOutput assembles what `git status --porcelain=v2 -z` writes: every
// record terminated by a NUL, including the last one.
func statusOutput(records ...string) []byte {
	var builder strings.Builder
	for _, record := range records {
		builder.WriteString(record)
		builder.WriteByte(0)
	}
	return []byte(builder.String())
}

func TestParseStatusEmptyOutput(t *testing.T) {
	status, err := git.ParseStatus(nil)
	if err != nil {
		t.Fatalf("empty output must not be an error: %v", err)
	}
	if !status.Clean() {
		t.Errorf("expected a clean status, got %d files", len(status.Files))
	}
	// A nil slice would marshal to null, and an interface handed null where it
	// expected a list is an interface that crashes on a clean repository.
	if status.Files == nil {
		t.Error("Files must be an empty slice, not nil")
	}
}

func TestParseStatusHeaders(t *testing.T) {
	status, err := git.ParseStatus(statusOutput(
		"# branch.oid 876c34abaf1974ba03918bd22071592cf4a7ac3e",
		"# branch.head main",
		"# branch.upstream origin/main",
		"# branch.ab +3 -2",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if status.HeadSHA != "876c34abaf1974ba03918bd22071592cf4a7ac3e" {
		t.Errorf("HeadSHA = %q", status.HeadSHA)
	}
	if status.Branch != "main" {
		t.Errorf("Branch = %q", status.Branch)
	}
	if status.Upstream != "origin/main" {
		t.Errorf("Upstream = %q", status.Upstream)
	}
	if status.Ahead != 3 || status.Behind != 2 {
		t.Errorf("ahead/behind = %d/%d, expected 3/2", status.Ahead, status.Behind)
	}
	if status.Detached || status.Unborn {
		t.Error("a branch with an upstream is neither detached nor unborn")
	}
}

func TestParseStatusUnbornBranch(t *testing.T) {
	// The state right after `git init`: a branch name, no commit behind it.
	status, err := git.ParseStatus(statusOutput(
		"# branch.oid (initial)",
		"# branch.head main",
		"? f.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !status.Unborn {
		t.Error("(initial) means the branch has no commit yet")
	}
	if status.HeadSHA != "" {
		t.Errorf("an unborn branch has no HEAD, got %q", status.HeadSHA)
	}
	if status.Branch != "main" {
		t.Errorf("Branch = %q, the name exists even with nothing behind it", status.Branch)
	}
}

func TestParseStatusDetachedHead(t *testing.T) {
	status, err := git.ParseStatus(statusOutput(
		"# branch.oid 876c34abaf1974ba03918bd22071592cf4a7ac3e",
		"# branch.head (detached)",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !status.Detached {
		t.Error("(detached) must set Detached rather than a branch named (detached)")
	}
	if status.Branch != "" {
		t.Errorf("Branch = %q, expected empty on a detached HEAD", status.Branch)
	}
}

func TestParseStatusOrdinaryEntries(t *testing.T) {
	const blob = "4cb29ea38f70d7c61b2a3a25b02e3bdf44905402"

	status, err := git.ParseStatus(statusOutput(
		"1 .M N... 100644 100644 100644 "+blob+" "+blob+" edited.txt",
		"1 M. N... 100644 100644 100644 "+blob+" "+blob+" staged.txt",
		"1 MM N... 100644 100644 100644 "+blob+" "+blob+" both.txt",
		"1 .D N... 100644 100644 000000 "+blob+" "+blob+" gone.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(status.Files) != 4 {
		t.Fatalf("expected 4 files, got %d", len(status.Files))
	}

	edited := status.Files[0]
	if edited.Path != "edited.txt" || edited.Index != git.CodeUnchanged || edited.WorkTree != git.CodeModified {
		t.Errorf("edited = %+v", edited)
	}
	if edited.Staged() {
		t.Error("a file modified only in the work tree has nothing staged")
	}
	if !edited.Unstaged() {
		t.Error("a file modified only in the work tree is unstaged")
	}

	staged := status.Files[1]
	if !staged.Staged() || staged.Unstaged() {
		t.Errorf("M. is staged and not unstaged, got %+v", staged)
	}

	// The case a single status verb cannot express, and the reason the two
	// codes are kept apart: staged, then edited again.
	both := status.Files[2]
	if !both.Staged() || !both.Unstaged() {
		t.Errorf("MM is on both sides, got %+v", both)
	}
}

func TestParseStatusRenameCarriesItsOriginPath(t *testing.T) {
	const blob = "3367afdbbf91e638efe983616377c60477cc6612"

	// The rename spends two records: the entry, then the path it came from.
	status, err := git.ParseStatus(statusOutput(
		"2 R. N... 100644 100644 100644 "+blob+" "+blob+" R100 renamed.txt",
		"ren.txt",
		"1 .M N... 100644 100644 100644 "+blob+" "+blob+" after.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(status.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(status.Files))
	}

	renamed := status.Files[0]
	if renamed.Kind != git.EntryRenamed {
		t.Errorf("Kind = %q", renamed.Kind)
	}
	if renamed.Path != "renamed.txt" || renamed.OldPath != "ren.txt" {
		t.Errorf("%q came from %q, expected renamed.txt from ren.txt", renamed.Path, renamed.OldPath)
	}
	if renamed.Score != 100 {
		t.Errorf("Score = %d, expected 100", renamed.Score)
	}

	// The entry after it must not have been shifted by the extra record. This
	// is the failure the two-record shape invites, and it is silent: every
	// following file would carry the wrong path.
	if status.Files[1].Path != "after.txt" {
		t.Errorf("the entry after a rename is %q, expected after.txt", status.Files[1].Path)
	}
}

func TestParseStatusCopyIsNotARename(t *testing.T) {
	const blob = "3367afdbbf91e638efe983616377c60477cc6612"

	status, err := git.ParseStatus(statusOutput(
		"2 C. N... 100644 100644 100644 "+blob+" "+blob+" C75 copy.txt",
		"source.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status.Files[0].Kind != git.EntryCopied {
		t.Errorf("Kind = %q, expected copied", status.Files[0].Kind)
	}
	if status.Files[0].Score != 75 {
		t.Errorf("Score = %d, expected 75", status.Files[0].Score)
	}
}

func TestParseStatusRenameWithNoOriginPathIsRefused(t *testing.T) {
	const blob = "3367afdbbf91e638efe983616377c60477cc6612"

	// Reading the entry without its second record would shift every field
	// after it, so a truncated pair is an error rather than a rename with an
	// empty origin.
	_, err := git.ParseStatus(statusOutput(
		"2 R. N... 100644 100644 100644 " + blob + " " + blob + " R100 renamed.txt",
	))
	if err == nil {
		t.Fatal("expected an error for a rename entry with no origin path")
	}
}

func TestParseStatusUnmergedEntriesReadAsUsAndThem(t *testing.T) {
	const blob = "3367afdbbf91e638efe983616377c60477cc6612"
	const stages = blob + " " + blob + " " + blob

	status, err := git.ParseStatus(statusOutput(
		"u UU N... 100644 100644 100644 100644 "+stages+" both.txt",
		"u DU N... 100644 100644 100644 100644 "+stages+" ours.txt",
		"u UA N... 100644 100644 100644 100644 "+stages+" theirs.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(status.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(status.Files))
	}
	if !status.Conflicted() {
		t.Error("an unmerged entry means the repository is conflicted")
	}

	expected := []git.Conflict{
		git.ConflictBothModified,
		git.ConflictDeletedByUs,
		git.ConflictAddedByThem,
	}
	for index, want := range expected {
		if got := status.Files[index].Conflict(); got != want {
			t.Errorf("%s: conflict = %q, expected %q", status.Files[index].Path, got, want)
		}
	}

	// An unmerged path is not staged: there is nothing a commit could record
	// while the merge is unresolved.
	if status.Files[0].Staged() {
		t.Error("an unmerged path has nothing staged")
	}
}

// The seven pairs, and which side of each has a file to check out.
//
// The one this exists for is `UA`. Neither of its letters is a `D`, so a rule
// written as "not deleted" reports our side as having content — and `git
// checkout --ours` then fails with "does not have our version" on the exact
// conflict the function is consulted about.
func TestHasSideFollowsTheConflictAndNotTheLetterD(t *testing.T) {
	for _, testCase := range []struct {
		pair   string
		ours   bool
		theirs bool
	}{
		{"UU", true, true},   // both modified
		{"AA", true, true},   // both added
		{"DD", false, false}, // both deleted
		{"AU", true, false},  // added by us
		{"UA", false, true},  // added by them
		{"DU", false, true},  // deleted by us
		{"UD", true, false},  // deleted by them
	} {
		t.Run(testCase.pair, func(t *testing.T) {
			record := git.FileStatus{
				Kind:     git.EntryUnmerged,
				Index:    git.Code(testCase.pair[:1]),
				WorkTree: git.Code(testCase.pair[1:]),
			}
			if got := record.HasSide(git.SideOurs); got != testCase.ours {
				t.Errorf("HasSide(ours) = %v for %s (%s), want %v",
					got, testCase.pair, record.Conflict(), testCase.ours)
			}
			if got := record.HasSide(git.SideTheirs); got != testCase.theirs {
				t.Errorf("HasSide(theirs) = %v for %s (%s), want %v",
					got, testCase.pair, record.Conflict(), testCase.theirs)
			}
		})
	}
}

// A path that is not unmerged has no sides at all, so neither answer is yes:
// the caller would otherwise run `git checkout --ours` on an ordinary file.
func TestHasSideIsFalseForAPathThatIsNotUnmerged(t *testing.T) {
	ordinary := git.FileStatus{Kind: git.EntryOrdinary, Index: git.CodeModified, WorkTree: git.CodeUnchanged}
	if ordinary.HasSide(git.SideOurs) || ordinary.HasSide(git.SideTheirs) {
		t.Error("an ordinary entry reports a side to keep")
	}
}

func TestConflictIsEmptyForEverythingElse(t *testing.T) {
	ordinary := git.FileStatus{Kind: git.EntryOrdinary, Index: git.CodeModified, WorkTree: git.CodeUnchanged}
	if got := ordinary.Conflict(); got != "" {
		t.Errorf("Conflict() = %q on an ordinary entry, expected empty", got)
	}
}

func TestParseStatusUntrackedPathsKeepEverythingAfterTheMarker(t *testing.T) {
	// The path is taken as the remainder, so a space or a leading dash in it
	// survives. -z is what makes that safe: without it git would have quoted
	// and escaped this name, and the interface would show something else.
	status, err := git.ParseStatus(statusOutput(
		"? a file with spaces.txt",
		"? -leading-dash.txt",
		"? sub/deep/nested.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	expected := []string{"a file with spaces.txt", "-leading-dash.txt", "sub/deep/nested.txt"}
	for index, want := range expected {
		if status.Files[index].Path != want {
			t.Errorf("path %d = %q, expected %q", index, status.Files[index].Path, want)
		}
		if status.Files[index].Kind != git.EntryUntracked {
			t.Errorf("path %d has kind %q, expected untracked", index, status.Files[index].Kind)
		}
		if !status.Files[index].Unstaged() {
			t.Errorf("path %d: an untracked file is a change the index does not have", index)
		}
	}
}

func TestParseStatusPathWithSpacesInAnOrdinaryEntry(t *testing.T) {
	const blob = "4cb29ea38f70d7c61b2a3a25b02e3bdf44905402"

	status, err := git.ParseStatus(statusOutput(
		"1 .M N... 100644 100644 100644 " + blob + " " + blob + " my documents/notes and things.md",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := status.Files[0].Path; got != "my documents/notes and things.md" {
		t.Errorf("path = %q; the path is the remainder, not one more field", got)
	}
}

func TestParseStatusSubmoduleField(t *testing.T) {
	const blob = "4cb29ea38f70d7c61b2a3a25b02e3bdf44905402"

	status, err := git.ParseStatus(statusOutput(
		"1 .M SCMU 160000 160000 160000 "+blob+" "+blob+" vendor/lib",
		"1 .M N... 100644 100644 100644 "+blob+" "+blob+" plain.txt",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	submodule := status.Files[0].Submodule
	if submodule == nil {
		t.Fatal("SCMU describes a submodule")
	}
	if !submodule.CommitChanged || !submodule.Modified || !submodule.Untracked {
		t.Errorf("SCMU = %+v, expected all three set", *submodule)
	}

	if status.Files[1].Submodule != nil {
		t.Error("N... is not a submodule")
	}
}

func TestParseStatusIgnoresUnknownHeaders(t *testing.T) {
	// git adds headers over time — `# stash` arrived in 2.35. One unread line
	// is a smaller failure than a working directory that will not load on a
	// newer git than this was written against.
	status, err := git.ParseStatus(statusOutput(
		"# branch.head main",
		"# stash 2",
		"# something.new whatever",
	))
	if err != nil {
		t.Fatalf("an unknown header must not fail the read: %v", err)
	}
	if status.Branch != "main" {
		t.Errorf("Branch = %q", status.Branch)
	}
}

func TestParseStatusRefusesRecordsItCannotRead(t *testing.T) {
	cases := map[string][]byte{
		"unknown record kind": statusOutput("x something"),
		"short ordinary entry": statusOutput(
			"1 .M N... 100644 100644 100644 deadbeef"),
		"status code of one character": statusOutput(
			"1 M N... 100644 100644 100644 deadbeef deadbeef path.txt"),
		"branch.ab with one field":       statusOutput("# branch.ab +3"),
		"branch.ab that is not a number": statusOutput("# branch.ab +x -2"),
	}

	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := git.ParseStatus(output); err == nil {
				t.Error("expected an error rather than a plausible answer")
			}
		})
	}
}

func TestParseStatusAheadBehindIsNeverNegative(t *testing.T) {
	// git writes the behind count with a minus sign. The interface counts
	// commits, and there is no such thing as minus two of them.
	status, err := git.ParseStatus(statusOutput("# branch.ab +0 -7"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status.Behind != 7 {
		t.Errorf("Behind = %d, expected 7", status.Behind)
	}
}
