package git_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The parsers in this package read the output of a program nobody here
// controls, describing a repository anybody can craft. Both of them cut a byte
// stream into records and records into fields, which is the one shape where a
// bug is silent: a separator a field can contain does not crash, it produces a
// plausible commit that is not the one git described.
//
// The record terminator was once 0x01, on the belief that git refuses that
// byte; it does not, and a subject reading `Revert "add \x01 handling"` would
// split one commit into two. The tests beside this file pin the cases we
// thought of. These pin the ones we did not.
//
// `go test` runs the seed corpus of every target below on each run, so they are
// regression tests for free. `./do test fuzz` is what actually goes looking.

// fixedDate keeps the fuzzer on the framing.
//
// The date field is the one with a narrow grammar, and a fuzzer handed it as an
// input spends its whole budget failing to produce an RFC 3339 string instead of
// exploring what a subject line can hold. TestParseLogUnreadableDate covers the
// unparseable case on its own.
const fixedDate = "2024-03-01T10:00:00+01:00"

// gitCanEmit reports whether git could put this string in a field.
//
// Neither byte is allowed anywhere in git's own output for these formats: a NUL
// is the field separator and git refuses it in every field, and a newline
// terminates the record — %s flattens the subject onto one line. Feeding them in
// would test the parser against input that cannot occur, and the answer would
// say nothing about whether it is correct.
func gitCanEmit(fields ...string) bool {
	for _, field := range fields {
		if strings.ContainsAny(field, "\x00\n") {
			return false
		}
	}
	return true
}

// FuzzParseLogRoundTrip is the property that matters: whatever git puts in a
// field comes back out of the parser byte for byte.
//
// Change the record terminator to something a subject can contain and this
// fails on the first commit message that contains it.
func FuzzParseLogRoundTrip(f *testing.F) {
	f.Add("a1b2c3", "", "Ada Lovelace", "first commit", "")
	f.Add("ffff", "aaaa bbbb", "Grace Hopper", "Merge branch 'feature'",
		"HEAD -> main, origin/main, tag: v1.0")
	f.Add("deadbeef", "cafe", "Barbara Liskov", `Revert "add \x01 handling"`, "")
	f.Add("1234", "", "Ada", `feat(api): “quotes”, $variables and 🌳`, "")
	f.Add("5678", "  ", "  spaced  ", "\ttab\tseparated\t", " , , ")

	f.Fuzz(func(t *testing.T, sha, parents, author, subject, decoration string) {
		if !gitCanEmit(sha, parents, author, subject, decoration) {
			t.Skip("git cannot emit a NUL or a newline inside a field")
		}

		output := logOutput(logRecord(sha, parents, author, fixedDate, subject, decoration))

		commits, err := git.ParseLog(output)
		if err != nil {
			t.Fatalf("a record git could produce failed to parse: %v", err)
		}
		if len(commits) != 1 {
			t.Fatalf("one record parsed as %d commits — the framing leaked", len(commits))
		}

		commit := commits[0]
		if commit.SHA != sha {
			t.Errorf("SHA = %q, put in %q", commit.SHA, sha)
		}
		if commit.Author != author {
			t.Errorf("author = %q, put in %q", commit.Author, author)
		}
		if commit.Subject != subject {
			t.Errorf("subject = %q, put in %q", commit.Subject, subject)
		}
		if expected := strings.Fields(parents); !slices.Equal(commit.Parents, expected) {
			t.Errorf("parents = %v, expected %v", commit.Parents, expected)
		}
	})
}

// FuzzParseLogNeverPanics feeds it what git would never send.
//
// The daemon parses whatever comes back from a git it did not write, in a
// repository the user only had to open. A panic in a handler is answered by
// this project's own middleware, but it is still a request that dies for a
// reason nobody can act on. Refusing the input is the correct outcome; the
// contract asserted here is that a refusal is total, never a half-filled slice
// alongside an error.
func FuzzParseLogNeverPanics(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("\x00"))
	f.Add([]byte("\x00\n"))
	f.Add([]byte("\n\n\n"))
	f.Add([]byte("far\x00too\x00few\x00fields\x00\n"))
	f.Add([]byte("a\x00b\x00c\x00yesterday\x00e\x00f\x00\n"))
	f.Add([]byte("\xff\xfe invalid utf-8 \x00\n"))

	f.Fuzz(func(t *testing.T, output []byte) {
		commits, err := git.ParseLog(output)
		if err != nil && commits != nil {
			t.Errorf("returned %d commits alongside an error: %v", len(commits), err)
		}
	})
}

// FuzzParseRefsRoundTrip is FuzzParseLogRoundTrip's counterpart on the other
// parser: the name, the object and the upstream survive the trip untouched.
//
// The SHA is the one field that is not a straight copy — an annotated tag
// carries the commit it points at in a second field, and the parser has to
// prefer that one, or every tag badge lands on an object absent from the graph.
func FuzzParseRefsRoundTrip(f *testing.F) {
	f.Add("refs/heads/main", "aaaa", "", "refs/remotes/origin/main", "[ahead 1, behind 2]")
	f.Add("refs/tags/v1.0", "tagobject", "commitobject", "", "")
	f.Add("refs/remotes/origin/feature", "bbbb", "", "", "[gone]")
	f.Add("refs/heads/release ", "cccc", "", "", "")
	f.Add("refs/heads/☃", "dddd", "", "refs/remotes/origin/☃", "[ahead 99999999999999999999]")

	f.Fuzz(func(t *testing.T, name, object, dereferenced, upstream, track string) {
		if !gitCanEmit(name, object, dereferenced, upstream, track) {
			t.Skip("git cannot emit a NUL or a newline inside a field")
		}
		if name == "" {
			t.Skip("an empty line is the end of the output, not a ref")
		}

		refs, err := git.ParseRefs([]byte(refLine(name, object, dereferenced, upstream, track) + "\n"))
		if err != nil {
			t.Fatalf("a line git could produce failed to parse: %v", err)
		}
		if len(refs) != 1 {
			t.Fatalf("one line parsed as %d refs — the framing leaked", len(refs))
		}

		ref := refs[0]
		if ref.Name != name {
			t.Errorf("name = %q, put in %q", ref.Name, name)
		}
		if ref.Upstream != upstream {
			t.Errorf("upstream = %q, put in %q", ref.Upstream, upstream)
		}

		expectedSHA := object
		if dereferenced != "" {
			expectedSHA = dereferenced
		}
		if ref.SHA != expectedSHA {
			t.Errorf("SHA = %q, expected %q", ref.SHA, expectedSHA)
		}

		// The counts are read out of a human-readable string, so an
		// unreadable one degrades to zero rather than failing the whole ref
		// list. What must never happen is a negative count: the interface
		// renders it as "N commits ahead".
		if ref.Ahead < 0 || ref.Behind < 0 {
			t.Errorf("negative tracking counts: ahead=%d behind=%d from %q",
				ref.Ahead, ref.Behind, track)
		}
	})
}

// FuzzParseRefsNeverPanics: same contract as its log counterpart.
func FuzzParseRefsNeverPanics(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("\n"))
	f.Add([]byte("\x00\x00\x00\x00"))
	f.Add([]byte("refs/heads/main\x00aaaa\n"))
	f.Add([]byte("refs/heads/main\x00a\x00b\x00c\x00[ahead notanumber]\n"))
	f.Add([]byte("\xff\xfe\x00\x00\x00\x00\n"))

	f.Fuzz(func(t *testing.T, output []byte) {
		refs, err := git.ParseRefs(output)
		if err != nil && refs != nil {
			t.Errorf("returned %d refs alongside an error: %v", len(refs), err)
		}
	})
}

// FuzzParseStashListRoundTrip is the log parser's property on the third
// record-and-field format in this package.
//
// A stash's subject is the one field here with no grammar at all: it holds
// whatever a person typed after `-m`, wrapped in a prefix git wrote. So the
// property has two halves. The framing must survive anything — one record in,
// one entry out — and the prefix cut must be reversible: what the parser calls
// the branch and what it calls the message have to put the subject back
// together exactly, or a row on screen is not the stash git described.
func FuzzParseStashListRoundTrip(f *testing.F) {
	f.Add("a1b2c3", "On main: fix the parser")
	f.Add("ffff", "WIP on release: 5956208 feat(cherry-pick): apply a commit")
	f.Add("deadbeef", "On (no branch): mid-bisect")
	f.Add("cafe", "On main: On main: doubled")
	f.Add("1234", "something no version of git ever wrote")
	f.Add("5678", "On : empty branch")
	f.Add("9abc", "On main:no space after the colon")

	f.Fuzz(func(t *testing.T, sha, subject string) {
		if !gitCanEmit(sha, subject) {
			t.Skip("git cannot emit a NUL or a newline inside a field")
		}

		output := stashOutput(stashRecord(sha, "stash@{0}", subject, fixedDate))

		stashes, err := git.ParseStashList(output)
		if err != nil {
			t.Fatalf("a record git could produce failed to parse: %v", err)
		}
		if len(stashes) != 1 {
			t.Fatalf("one record parsed as %d stashes — the framing leaked", len(stashes))
		}

		stash := stashes[0]
		if stash.SHA != sha {
			t.Errorf("SHA = %q, put in %q", stash.SHA, sha)
		}

		// The two halves recombined. A branch means a prefix was recognised
		// and the message is what followed the colon; no branch means either a
		// detached HEAD — where git's own "(no branch)" is dropped on purpose
		// — or a subject in no shape git writes, which comes back whole.
		switch {
		case stash.Branch != "":
			if !strings.Contains(subject, stash.Branch+": "+stash.Message) {
				t.Errorf("branch %q and message %q do not rebuild %q",
					stash.Branch, stash.Message, subject)
			}
		case stash.Message != subject:
			if !strings.HasSuffix(subject, ": "+stash.Message) {
				t.Errorf("message %q is neither the subject %q nor its tail",
					stash.Message, subject)
			}
		}
	})
}

// FuzzParseStashListNeverPanics: same contract as its log and ref
// counterparts, over the selector field that no other format has.
func FuzzParseStashListNeverPanics(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("\x00\n"))
	f.Add([]byte("aa\x00stash@{0}\x00On main: one\x002024-03-01T10:00:00+01:00\x00\n"))
	f.Add([]byte("aa\x00stash@{}\x00s\x002024-03-01T10:00:00+01:00\x00\n"))
	f.Add([]byte("aa\x00stash@{-1}\x00s\x002024-03-01T10:00:00+01:00\x00\n"))
	f.Add([]byte("aa\x00stash@{99999999999999999999}\x00s\x002024-03-01T10:00:00+01:00\x00\n"))
	f.Add([]byte("far\x00too\x00few\x00\n"))
	f.Add([]byte("\xff\xfe invalid utf-8 \x00\n"))

	f.Fuzz(func(t *testing.T, output []byte) {
		stashes, err := git.ParseStashList(output)
		if err != nil && stashes != nil {
			t.Errorf("returned %d stashes alongside an error: %v", len(stashes), err)
		}
	})
}

// FuzzParseWorktreesRoundTrip: a path git can emit comes back whole.
//
// `-z` is what makes that true for any path at all — `--porcelain` alone
// prints them raw — so the one byte a path cannot hold here is NUL, which is
// the one byte a POSIX path cannot hold either. A newline can, and does.
func FuzzParseWorktreesRoundTrip(f *testing.F) {
	f.Add("/repo", "refs/heads/main")
	f.Add("/oh\ndear", "refs/heads/main")
	f.Add("/spaces in it", "refs/heads/release/2.0")
	f.Add("/repo", "refs/heads/branch with spaces")
	f.Add("C:\\repo", "refs/heads/main")
	f.Add("/repo", "")
	f.Add(" leading", "refs/heads/ ")

	f.Fuzz(func(t *testing.T, path, branch string) {
		if path == "" || strings.ContainsRune(path, 0) || strings.ContainsRune(branch, 0) {
			t.Skip("git emits no NUL inside a field, and every worktree has a path")
		}

		record := "worktree " + path + "\x00HEAD " + strings.Repeat("a", 40) + "\x00"
		if branch != "" {
			record += "branch " + branch + "\x00"
		} else {
			record += "detached\x00"
		}
		record += "\x00"

		worktrees, err := git.ParseWorktrees([]byte(record), true)
		if err != nil {
			t.Fatalf("a record git could produce failed to parse: %v", err)
		}
		if len(worktrees) != 1 {
			t.Fatalf("one record parsed as %d worktrees — the framing leaked", len(worktrees))
		}
		if worktrees[0].Path != path {
			t.Errorf("path = %q, put in %q", worktrees[0].Path, path)
		}
		if branch == "" {
			if !worktrees[0].Detached || worktrees[0].Branch != "" {
				t.Errorf("worktree = %+v, want detached", worktrees[0])
			}
			return
		}
		if !strings.HasSuffix(branch, worktrees[0].Branch) {
			t.Errorf("branch %q is not the tail of %q", worktrees[0].Branch, branch)
		}
	})
}

// FuzzParseWorktreesNeverPanics: the same contract as every other parser here.
// An error and a result are never both returned.
//
// Both terminators on every input, because both are production paths: a git
// under 2.36 is read without `-z`, and the newline shape has its own framing —
// a trailing blank line, a C-quoted lock reason — that the NUL shape never
// hands it.
func FuzzParseWorktreesNeverPanics(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("\x00"))
	f.Add([]byte("\x00\x00\x00"))
	f.Add([]byte("worktree\x00\x00"))
	f.Add([]byte("worktree /a\x00worktree /b\x00\x00"))
	f.Add([]byte("HEAD abc\x00\x00"))
	f.Add([]byte("locked\x00"))
	f.Add([]byte("worktree /a\x00bare\x00detached\x00locked\x00prunable\x00\x00"))
	f.Add([]byte("\xff\xfe invalid utf-8 \x00\x00"))
	f.Add([]byte("worktree /a\nHEAD abc\n\n"))
	f.Add([]byte("worktree /a\nlocked \"needs a\\nrebuild\"\n\n"))
	f.Add([]byte("worktree /a\nlocked \"unterminated\n\n"))

	f.Fuzz(func(t *testing.T, output []byte) {
		for _, nul := range []bool{true, false} {
			worktrees, err := git.ParseWorktrees(output, nul)
			if err != nil && worktrees != nil {
				t.Errorf("nul=%v returned %d worktrees alongside an error: %v",
					nul, len(worktrees), err)
			}
		}
	})
}
