package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// A pointer as git-lfs writes one: the version line the specification pins,
// then the keys in alphabetical order, each terminated by a newline.
const pointer = "version https://git-lfs.github.com/spec/v1\n" +
	"oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n" +
	"size 12345\n"

func TestParseLFSPointerReadsWhatGitLFSWrites(t *testing.T) {
	read, ok := git.ParseLFSPointer([]byte(pointer))
	if !ok {
		t.Fatal("a pointer git-lfs itself wrote was not recognised")
	}
	if read.OID != "sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393" {
		t.Errorf("OID = %q", read.OID)
	}
	if read.Size != 12345 {
		t.Errorf("Size = %d, want 12345", read.Size)
	}
}

// The whole risk in this parser is a plausible wrong answer rather than a
// crash: a text file described as three hundred megabytes, or a real pointer
// read as an ordinary file. Both directions are here.
func TestParseLFSPointerRefusesWhatIsNotOne(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"prose about LFS": "version https://example.invalid/spec/v1\noid sha256:x\nsize 1\n",
		"no oid":          "version https://git-lfs.github.com/spec/v1\nsize 12\n",
		// Read as a pointer, this describes a file of no size at all — which
		// the pane then prints as "0 bytes" for a file it knows nothing about.
		"no size": "version https://git-lfs.github.com/spec/v1\noid sha256:abc\n",
		"size not a number": "version https://git-lfs.github.com/spec/v1\n" +
			"oid sha256:abc\nsize twelve\n",
		"negative size": "version https://git-lfs.github.com/spec/v1\n" +
			"oid sha256:abc\nsize -1\n",
		"line without a space": "version https://git-lfs.github.com/spec/v1\noid\nsize 1\n",
	}

	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, ok := git.ParseLFSPointer([]byte(content)); ok {
				t.Errorf("%q was read as a pointer", content)
			}
		})
	}
}

// A file whose first line happens to be the version line, and which is
// megabytes long: the cap exists so that answering "no" costs a comparison
// rather than a copy of somebody's data.
func TestParseLFSPointerRefusesAnythingPastThePointerSize(t *testing.T) {
	padded := pointer
	for len(padded) <= 1024 {
		padded += "a line of something that is not a pointer\n"
	}
	if _, ok := git.ParseLFSPointer([]byte(padded)); ok {
		t.Error("a file larger than any pointer was read as one")
	}
}

func TestParseLFSPatternsReadsTheFilterAttribute(t *testing.T) {
	attributes := "# what this repository keeps outside itself\n" +
		"*.psd filter=lfs diff=lfs merge=lfs -text\n" +
		"\"design assets/*.png\" filter=lfs diff=lfs merge=lfs -text\n" +
		"*.md text\n" +
		"*.bin diff=lfs\n"

	patterns := git.ParseLFSPatterns([]byte(attributes))

	want := []string{"*.psd", "design assets/*.png"}
	if len(patterns) != len(want) {
		t.Fatalf("patterns = %v, want %v", patterns, want)
	}
	for index, pattern := range want {
		if patterns[index] != pattern {
			t.Errorf("patterns[%d] = %q, want %q", index, patterns[index], pattern)
		}
	}
}

// `diff=lfs` and `merge=lfs` travel with the filter and neither of them
// decides where the bytes live. A line carrying only one of those is a line
// this panel must not offer to untrack.
func TestParseLFSPatternsIgnoresALineWithoutTheFilter(t *testing.T) {
	if patterns := git.ParseLFSPatterns([]byte("*.bin diff=lfs merge=lfs\n")); len(patterns) != 0 {
		t.Errorf("patterns = %v, want none", patterns)
	}
}

func TestDetectLFSNamesEitherSideThatIsAPointer(t *testing.T) {
	// A file moved into LFS: its content on the old side, a pointer on the
	// new. The case that reads as a deletion unless it is named.
	diff := git.FileDiff{
		Hunks: []git.Hunk{{
			OldStart: 1, OldLines: 1,
			NewStart: 1, NewLines: 3,
			Lines: append(
				[]git.DiffLine{{Kind: git.LineRemoved, Text: "the whole picture, in bytes"}},
				pointerLines()...,
			),
		}},
	}

	found := git.DetectLFS(diff)
	if found == nil {
		t.Fatal("a pointer on the new side was not detected")
	}
	if found.Old != nil {
		t.Error("the old side is not a pointer and was read as one")
	}
	if found.New == nil || found.New.Size != 12345 {
		t.Errorf("new = %+v, want the pointer's own size", found.New)
	}
}

func TestDetectLFSAnswersNothingForAnOrdinaryDiff(t *testing.T) {
	diff := git.FileDiff{
		Hunks: []git.Hunk{{
			OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
			Lines: []git.DiffLine{
				{Kind: git.LineRemoved, Text: "before"},
				{Kind: git.LineAdded, Text: "after"},
			},
		}},
	}
	if found := git.DetectLFS(diff); found != nil {
		t.Errorf("an ordinary diff was read as a pointer: %+v", found)
	}
}

// The lines in hand are the whole file only when there is one hunk covering it
// from line 1. A diff of a large file arrives as hunks with gaps, and reading
// those as content would describe a fragment as a pointer — or, worse, fail to
// recognise a real one and say nothing.
func TestDetectLFSRefusesASideItOnlyHasPartOf(t *testing.T) {
	partial := git.FileDiff{
		Hunks: []git.Hunk{{
			OldStart: 40, OldLines: 3, NewStart: 40, NewLines: 3,
			Lines: pointerLines(),
		}},
	}
	if found := git.DetectLFS(partial); found != nil {
		t.Errorf("a hunk starting at line 40 was read as a whole file: %+v", found)
	}

	twoHunks := git.FileDiff{
		Hunks: []git.Hunk{
			{OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 3, Lines: pointerLines()},
			{OldStart: 9, OldLines: 1, NewStart: 12, NewLines: 1},
		},
	}
	if found := git.DetectLFS(twoHunks); found != nil {
		t.Errorf("a file arriving in two hunks was read whole: %+v", found)
	}
}

// git decided there is nothing to show, so there are no lines to read. A
// pointer is text and never lands here.
func TestDetectLFSSaysNothingAboutABinaryDiff(t *testing.T) {
	if found := git.DetectLFS(git.FileDiff{Binary: true}); found != nil {
		t.Errorf("a binary diff answered %+v", found)
	}
}

// pointerLines is the pointer as added lines of a diff.
func pointerLines() []git.DiffLine {
	return []git.DiffLine{
		{Kind: git.LineAdded, Text: "version https://git-lfs.github.com/spec/v1"},
		{
			Kind: git.LineAdded,
			Text: "oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393",
		},
		{Kind: git.LineAdded, Text: "size 12345"},
	}
}

// The case the "whole file" flag used to promise away and could not: a hunk
// starting at line 1 of a long document, whose first lines happen to look like
// a pointer. The grammar is what refuses it — an ordinary line of prose is not
// a pointer key — and without that refusal the pane reports somebody's writing
// as a file of three hundred megabytes kept elsewhere.
func TestParseLFSPointerRefusesAPrefixOfALongerFile(t *testing.T) {
	prefix := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + strings.Repeat("a", 64) + "\n" +
		"size 314572800\n" +
		"is what a pointer file looks like.\n" +
		"The rest of this document explains why.\n"

	if pointer, ok := git.ParseLFSPointer([]byte(prefix)); ok {
		t.Errorf("ParseLFSPointer read prose as %+v", pointer)
	}
}

// And what strictness must not cost: the specification's own extension keys,
// which git-lfs may write and which describe a pointer that is perfectly good.
func TestParseLFSPointerKeepsTheExtensionKeys(t *testing.T) {
	text := "version https://git-lfs.github.com/spec/v1\n" +
		"ext-0-foo sha256:" + strings.Repeat("b", 64) + "\n" +
		"oid sha256:" + strings.Repeat("a", 64) + "\n" +
		"size 12\n"

	pointer, ok := git.ParseLFSPointer([]byte(text))
	if !ok {
		t.Fatal("ParseLFSPointer refused a pointer carrying an ext- key")
	}
	if pointer.Size != 12 {
		t.Errorf("size = %d, want 12", pointer.Size)
	}
}
