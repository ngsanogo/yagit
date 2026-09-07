package git

import (
	"strings"
	"testing"
)

func TestProgressWriterSplitsOnCarriageReturn(t *testing.T) {
	var got []string
	writer := &progressWriter{
		buffer: &boundedBuffer{limit: maxStderr},
		onLine: func(line string) { got = append(got, line) },
	}

	if _, err := writer.Write([]byte("Receiving objects:  50% (1/2)\rReceiving objects: 100% (2/2)\n")); err != nil {
		t.Fatal(err)
	}
	writer.flush()

	if len(got) < 2 {
		t.Fatalf("segments = %v, want both progress updates", got)
	}
	if !strings.Contains(got[0], "50%") || !strings.Contains(got[1], "100%") {
		t.Fatalf("segments = %v", got)
	}
	if !strings.Contains(writer.buffer.String(), "50%") {
		t.Fatal("full stderr buffer lost the progress text")
	}
}

func TestProgressWriterDropsEmptySegments(t *testing.T) {
	var got []string
	writer := &progressWriter{
		buffer: &boundedBuffer{limit: maxStderr},
		onLine: func(line string) { got = append(got, line) },
	}
	if _, err := writer.Write([]byte("done\n\n\r\n")); err != nil {
		t.Fatal(err)
	}
	writer.flush()
	if len(got) != 1 || got[0] != "done" {
		t.Fatalf("segments = %v, want only done", got)
	}
}

// stderr is bounded where stdout is capped, and the difference matters: a
// command whose error output is long must still report the error.
func TestBoundedBufferTruncatesAndSaysSo(t *testing.T) {
	buffer := &boundedBuffer{limit: 16}

	// Written in two goes, because the interesting case is the write that
	// straddles the limit rather than one that starts past it.
	for _, chunk := range []string{"the reason it ", "failed, at length, repeatedly"} {
		n, err := buffer.Write([]byte(chunk))
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		// The write must be reported as complete even when nothing was kept:
		// a short write is how a writer tells git to stop, and what is being
		// dropped here is commentary.
		if n != len(chunk) {
			t.Errorf("Write reported %d of %d bytes: git would stop on a short write", n, len(chunk))
		}
	}

	got := buffer.String()
	if !strings.HasPrefix(got, "the reason it f") {
		t.Errorf("the beginning was not kept: %q", got)
	}
	if !strings.Contains(got, "not kept") {
		t.Errorf("output stops with no sign of it, which reads as git having said only that much: %q", got)
	}
}

func TestBoundedBufferSaysNothingWhenNothingWasDropped(t *testing.T) {
	buffer := &boundedBuffer{limit: 64}
	if _, err := buffer.Write([]byte("fatal: pathspec did not match")); err != nil {
		t.Fatal(err)
	}
	if got := buffer.String(); got != "fatal: pathspec did not match" {
		t.Errorf("a message under the limit was altered: %q", got)
	}
}
