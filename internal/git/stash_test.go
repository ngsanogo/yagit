package git_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

// Reading the stack, and the lines that act on it.
//
// Two subjects here and they are the two places a stash goes wrong quietly.
// The list is a record-and-field format over text a person typed, so a
// separator inside a message would produce a plausible entry that is not the
// one git described. The argument builders decide which position git is handed
// — and a position is the one argument in this package that means something
// different tomorrow.
//
// What git actually does with any of it is stash_integration_test.go's
// subject.

// stashRecord assembles one record of the format Stashes reads, so the tests
// below describe entries rather than escape sequences.
func stashRecord(sha, selector, subject, date string) string {
	return strings.Join([]string{sha, selector, subject, date}, "\x00") + "\x00\n"
}

// stashOutput assembles a full listing the way git emits one.
//
// The newline between records is not decoration: `--pretty=format:` SEPARATES
// records with one, so every record but the first arrives with a leading
// newline that splitRecords has to absorb. Concatenating the records instead
// would build an input git never produces, and the pure tests would agree with
// each other while disagreeing with the binary — which is exactly what
// happened before splitRecords existed.
func stashOutput(records ...string) []byte {
	return []byte(strings.Join(records, "\n"))
}

func TestParseStashListReadsEveryField(t *testing.T) {
	output := stashOutput(
		stashRecord(
			"a1b2c3d4e5f60718293a4b5c6d7e8f9012345678",
			"stash@{0}",
			"On main: fix the parser",
			"2026-09-04T10:00:00+02:00",
		),
		stashRecord(
			"1122334455667788990011223344556677889900",
			"stash@{1}",
			"WIP on release: 5956208 feat(cherry-pick): apply a commit",
			"2026-09-03T18:30:00+02:00",
		),
	)

	stashes, err := git.ParseStashList(output)
	if err != nil {
		t.Fatalf("ParseStashList: %v", err)
	}
	if len(stashes) != 2 {
		t.Fatalf("got %d stashes, want 2", len(stashes))
	}

	first := stashes[0]
	if first.Index != 0 {
		t.Errorf("Index = %d, want 0", first.Index)
	}
	if first.SHA != "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678" {
		t.Errorf("SHA = %q, want the full object name", first.SHA)
	}
	if first.Branch != "main" {
		t.Errorf("Branch = %q, want main", first.Branch)
	}
	if first.Message != "fix the parser" {
		t.Errorf("Message = %q, want git's prefix taken off", first.Message)
	}
	if !first.Date.Equal(time.Date(2026, 9, 4, 10, 0, 0, 0, time.FixedZone("", 2*60*60))) {
		t.Errorf("Date = %v, want the record's own", first.Date)
	}

	second := stashes[1]
	if second.Index != 1 {
		t.Errorf("Index = %d, want 1", second.Index)
	}
	if second.Branch != "release" {
		t.Errorf("Branch = %q, want release", second.Branch)
	}
	// The WIP form's message is the commit git was sitting on. The colon
	// inside it is the case the prefix cut has to survive.
	if second.Message != "5956208 feat(cherry-pick): apply a commit" {
		t.Errorf("Message = %q, want everything after the branch", second.Message)
	}
}

func TestParseStashListReadsRefFromEachEntry(t *testing.T) {
	output := stashOutput(
		stashRecord("aa", "stash@{0}", "On main: one", "2026-09-04T10:00:00Z"),
		stashRecord("bb", "stash@{1}", "On main: two", "2026-09-04T09:00:00Z"),
	)

	stashes, err := git.ParseStashList(output)
	if err != nil {
		t.Fatalf("ParseStashList: %v", err)
	}
	for index, want := range []string{"stash@{0}", "stash@{1}"} {
		if got := stashes[index].Ref(); got != want {
			t.Errorf("stashes[%d].Ref() = %q, want %q", index, got, want)
		}
	}
}

func TestParseStashListEmptyStack(t *testing.T) {
	// `git stash list` on a repository that never stashed prints nothing and
	// exits 0. An empty slice rather than nil, because the payload goes to a
	// browser and null is not a list.
	stashes, err := git.ParseStashList(nil)
	if err != nil {
		t.Fatalf("ParseStashList(nil): %v", err)
	}
	if stashes == nil {
		t.Fatal("stashes = nil, want an empty slice")
	}
	if len(stashes) != 0 {
		t.Errorf("got %d stashes, want none", len(stashes))
	}
}

func TestParseStashListKeepsAMessageThatLooksLikeARecord(t *testing.T) {
	// The reason the format is NUL-separated. A person can call a stash
	// anything, and "On main:" inside their own text must not be read as the
	// start of another entry.
	subject := "On main: fixed On main: twice"
	stashes, err := git.ParseStashList(
		stashOutput(stashRecord("aa", "stash@{0}", subject, "2026-09-04T10:00:00Z")))
	if err != nil {
		t.Fatalf("ParseStashList: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("got %d stashes, want 1", len(stashes))
	}
	if stashes[0].Message != "fixed On main: twice" {
		t.Errorf("Message = %q, want only the first prefix taken off", stashes[0].Message)
	}
}

func TestParseStashListDetachedHEADHasNoBranch(t *testing.T) {
	// git records "(no branch)", which is not a name anything can be done
	// with. Empty is what lets the interface draw nothing rather than offer a
	// branch nobody can check out.
	stashes, err := git.ParseStashList(
		stashOutput(stashRecord("aa", "stash@{0}", "On (no branch): mid-bisect", "2026-09-04T10:00:00Z")))
	if err != nil {
		t.Fatalf("ParseStashList: %v", err)
	}
	if stashes[0].Branch != "" {
		t.Errorf("Branch = %q, want empty for a detached HEAD", stashes[0].Branch)
	}
	if stashes[0].Message != "mid-bisect" {
		t.Errorf("Message = %q, want the message alone", stashes[0].Message)
	}
}

func TestParseStashListKeepsAnUnrecognisedSubjectWhole(t *testing.T) {
	// A reflog can be written by anything. Showing exactly what git said beats
	// refusing to draw the row, and it beats guessing a branch out of it.
	stashes, err := git.ParseStashList(
		stashOutput(stashRecord("aa", "stash@{0}", "something else entirely", "2026-09-04T10:00:00Z")))
	if err != nil {
		t.Fatalf("ParseStashList: %v", err)
	}
	if stashes[0].Branch != "" {
		t.Errorf("Branch = %q, want none", stashes[0].Branch)
	}
	if stashes[0].Message != "something else entirely" {
		t.Errorf("Message = %q, want the subject whole", stashes[0].Message)
	}
}

func TestParseStashListRefusesAListOutOfOrder(t *testing.T) {
	// The position is what every command in this file runs against, so a list
	// whose selectors do not match their places is refused rather than read
	// off the loop counter. An index that drifts by one is a drop of the wrong
	// stash.
	output := stashOutput(
		stashRecord("aa", "stash@{0}", "On main: one", "2026-09-04T10:00:00Z"),
		stashRecord("bb", "stash@{4}", "On main: two", "2026-09-04T09:00:00Z"),
	)

	if _, err := git.ParseStashList(output); err == nil {
		t.Fatal("ParseStashList accepted a list whose selectors skip a position")
	}
}

func TestParseStashListRefusesMalformedRecords(t *testing.T) {
	for name, output := range map[string]string{
		"too few fields":  "aa\x00stash@{0}\x00On main: one\x00\n",
		"unreadable date": stashRecord("aa", "stash@{0}", "On main: one", "yesterday"),
		"selector shape":  stashRecord("aa", "refs/stash@{0}", "On main: one", "2026-09-04T10:00:00Z"),
		"selector not a number": stashRecord(
			"aa", "stash@{top}", "On main: one", "2026-09-04T10:00:00Z"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := git.ParseStashList(stashOutput(output)); err == nil {
				t.Fatalf("ParseStashList accepted %q", output)
			}
		})
	}
}

func TestStashPushArgs(t *testing.T) {
	for name, testCase := range map[string]struct {
		message   string
		untracked bool
		want      []string
	}{
		"nothing but the verb": {
			want: []string{"stash", "push"},
		},
		"a message is one argument": {
			message: "fix the parser",
			// Joined rather than "--message", "fix the parser": the joined
			// form is the one that cannot be read as anything else.
			want: []string{"stash", "push", "--message=fix the parser"},
		},
		"a message git would read as an option": {
			message: "-f --force",
			want:    []string{"stash", "push", "--message=-f --force"},
		},
		"untracked files come along when asked": {
			message:   "everything",
			untracked: true,
			want:      []string{"stash", "push", "--include-untracked", "--message=everything"},
		},
		"the flag stands alone": {
			untracked: true,
			want:      []string{"stash", "push", "--include-untracked"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := git.StashPushArgs(testCase.message, testCase.untracked)
			if strings.Join(got, "\x00") != strings.Join(testCase.want, "\x00") {
				t.Errorf("StashPushArgs = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestStashApplyArgsPinTheSubcommand(t *testing.T) {
	keep := git.StashApplyArgs(git.StashApplyKeep, 2)
	if strings.Join(keep, " ") != "stash apply stash@{2}" {
		t.Errorf("apply args = %q", keep)
	}

	pop := git.StashApplyArgs(git.StashApplyPop, 0)
	if strings.Join(pop, " ") != "stash pop stash@{0}" {
		t.Errorf("pop args = %q", pop)
	}
}

func TestStashDropArgsNameThePosition(t *testing.T) {
	// A position and not an object name: `git stash drop` refuses a raw SHA,
	// which is the whole reason StashTarget checks one against the other.
	got := git.StashDropArgs(3)
	if strings.Join(got, " ") != "stash drop stash@{3}" {
		t.Errorf("StashDropArgs = %q", got)
	}
}

func TestParseStashApplyMode(t *testing.T) {
	for _, mode := range []git.StashApplyMode{git.StashApplyKeep, git.StashApplyPop} {
		got, err := git.ParseStashApplyMode(string(mode))
		if err != nil {
			t.Fatalf("ParseStashApplyMode(%q): %v", mode, err)
		}
		if got != mode {
			t.Errorf("ParseStashApplyMode(%q) = %q", mode, got)
		}
	}

	for name, raw := range map[string]string{
		"empty":          "",
		"another word":   "drop",
		"a git flag":     "--index",
		"different case": "Pop",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := git.ParseStashApplyMode(raw); err == nil {
				t.Fatalf("ParseStashApplyMode(%q) was accepted", raw)
			}
		})
	}
}

func TestStashRefIsTheOnlyPositionFormat(t *testing.T) {
	// One definition of "stash@{N}" behind every command, so a position shown
	// on a confirmation is character for character the one that runs.
	stash := git.Stash{Index: 7}
	if stash.Ref() != "stash@{7}" {
		t.Errorf("Ref() = %q", stash.Ref())
	}
	if got := git.StashDropArgs(7); got[2] != stash.Ref() {
		t.Errorf("StashDropArgs names %q, Ref() says %q", got[2], stash.Ref())
	}
	if got := git.StashApplyArgs(git.StashApplyPop, 7); got[2] != stash.Ref() {
		t.Errorf("StashApplyArgs names %q, Ref() says %q", got[2], stash.Ref())
	}
}

func TestErrNoStashAndErrStashMovedAreDistinct(t *testing.T) {
	// Two refusals with two answers: one means the stack is shorter than the
	// request thought, the other means the stash is still there under a
	// different number. A route that collapsed them would tell somebody their
	// work was gone.
	if errors.Is(git.ErrNoStash, git.ErrStashMoved) || errors.Is(git.ErrStashMoved, git.ErrNoStash) {
		t.Fatal("ErrNoStash and ErrStashMoved match each other")
	}
}
