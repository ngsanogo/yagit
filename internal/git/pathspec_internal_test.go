package git

import (
	"strconv"
	"strings"
	"testing"
)

// The decision this file pins is which of the two forms a list of paths takes,
// because only one of them is readable in the log panel and only one of them
// survives Windows.

func longEnoughToCross(count int) []string {
	paths := make([]string, 0, count)
	for index := range count {
		paths = append(paths, strings.Repeat("n", 40)+strconv.Itoa(index))
	}
	return paths
}

func TestAShortListStaysOnTheCommandLine(t *testing.T) {
	args, stdin := pathspecs([]string{"notes.md", "src/main.go"})

	if stdin != nil {
		t.Error("a two-file list went to standard input: the log panel would stop naming the files")
	}
	want := []string{"--", ":(literal)notes.md", ":(literal)src/main.go"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for index, expected := range want {
		if args[index] != expected {
			t.Errorf("args[%d] = %q, want %q", index, args[index], expected)
		}
	}
}

func TestALongListGoesToStandardInput(t *testing.T) {
	paths := longEnoughToCross(1000)
	args, stdin := pathspecs(paths)

	if stdin == nil {
		t.Fatal("a list past the limit stayed on the command line: Windows refuses it")
	}

	// The two flags have to travel together. Without --pathspec-file-nul git
	// splits on newlines, and a file name may contain one.
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--pathspec-from-file=-", "--pathspec-file-nul"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("args = %v, missing %s", args, flag)
		}
	}
	// And no pathspec may remain on the command line: git refuses a pathspec
	// argument and a pathspec file together.
	if strings.Contains(joined, ":(literal)") {
		t.Errorf("args = %v: pathspecs must not stay on the command line", args)
	}

	specs := strings.Split(strings.TrimSuffix(string(stdin), "\x00"), "\x00")
	if len(specs) != len(paths) {
		t.Fatalf("standard input holds %d pathspecs, want %d", len(specs), len(paths))
	}
	for index, spec := range specs {
		if want := ":(literal)" + paths[index]; spec != want {
			t.Errorf("pathspec %d = %q, want %q — the literal rule has to survive the move", index, spec, want)
		}
	}
}

func TestTheThresholdIsCrossedOnceAndNotEarlier(t *testing.T) {
	// Just under, then just over, so a change to the number is a deliberate
	// one rather than something a refactor slid past.
	under := strings.Repeat("x", maxPathspecArgv-len(":(literal)")-1)
	if _, stdin := pathspecs([]string{under}); stdin != nil {
		t.Error("a list that fits was sent to standard input")
	}

	over := strings.Repeat("x", maxPathspecArgv)
	if _, stdin := pathspecs([]string{over}); stdin == nil {
		t.Error("a list that does not fit stayed on the command line")
	}
}

func TestEveryCleanBatchFitsOnACommandLine(t *testing.T) {
	// `git clean` takes no pathspec file, so the only way under the limit is
	// to run it more than once. Each command has to fit on its own.
	paths := longEnoughToCross(2000)
	batches := DiscardUntrackedBatches(paths)

	if len(batches) < 2 {
		t.Fatalf("2000 paths produced %d command(s): nothing was split", len(batches))
	}

	seen := 0
	for index, batch := range batches {
		if len(batch) < 4 {
			t.Errorf("batch %d carries no pathspec: %v", index, batch)
		}
		length := 0
		for _, argument := range batch[3:] {
			length += len(argument) + 1
			seen++
		}
		if length > maxPathspecArgv {
			t.Errorf("batch %d is %d characters, past the %d a command line holds",
				index, length, maxPathspecArgv)
		}
	}
	if seen != len(paths) {
		t.Errorf("the batches carry %d paths between them, want %d — some were dropped", seen, len(paths))
	}
}

func TestASingleCleanCommandWhenTheListIsShort(t *testing.T) {
	batches := DiscardUntrackedBatches([]string{"build/out.js", "build/out.map"})
	if len(batches) != 1 {
		t.Fatalf("a two-file discard produced %d commands, want one", len(batches))
	}
	want := []string{"clean", "--force", "--", ":(literal)build/out.js", ":(literal)build/out.map"}
	if strings.Join(batches[0], " ") != strings.Join(want, " ") {
		t.Errorf("command = %v, want %v", batches[0], want)
	}
}
