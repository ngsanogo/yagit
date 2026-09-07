package git

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// What git says, collected without letting it decide how much memory this
// process uses.
//
// Three writers, because three commands want three different things from the
// same stream. A read wants all of the output and a bound it will not pass; a
// failure wants the END of stderr, because git puts the sentence that matters
// last; a fetch wants each progress line as it arrives, and those arrive
// separated by carriage returns rather than newlines.

// errOutputTooLarge stops the copy from git as soon as the cap is passed. It
// never reaches a caller: Exec turns it into the *Error that names the command.
var errOutputTooLarge = errors.New("output limit reached")

// cappedBuffer collects output up to a limit, and stops the command rather
// than the machine when it is passed.
type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (w *cappedBuffer) Write(data []byte) (int, error) {
	if w.limit > 0 && w.buffer.Len()+len(data) > w.limit {
		// Failing the write closes the pipe, so git stops producing rather
		// than filling a buffer nobody will read. os/exec reports this as the
		// command's error, which Exec replaces with one that names the cap.
		w.exceeded = true
		return 0, errOutputTooLarge
	}
	return w.buffer.Write(data)
}

func (w *cappedBuffer) Bytes() []byte { return w.buffer.Bytes() }
func (w *cappedBuffer) Len() int      { return w.buffer.Len() }

// maxStderr bounds what is kept of a command's error output.
//
// stdout has had a cap since the day a diff could exhaust the process;
// stderr had none, and `--progress` made it the loud one. A `git push` whose
// pre-receive hook prints a build report — an ordinary thing on a server with
// CI — sends megabytes down this stream, and every byte of it is then held in
// this buffer, copied into the Execution, marshalled into JSON, broadcast to
// every open tab, and kept in the 500-command ring until 500 more commands
// push it out.
//
// 64 KiB is far more than any explanation git writes and far less than any
// report a hook produces.
const maxStderr = 64 << 10

// boundedBuffer keeps the beginning of what is written to it and counts the
// rest.
//
// It TRUNCATES where cappedBuffer FAILS, and the difference is the same one
// beginningOf documents: half a patch is dangerous, because it applies to the
// wrong thing, while half an explanation is still an explanation. Refusing a
// command because its error message was long would replace a message the user
// could act on with one they could not.
type boundedBuffer struct {
	buffer  bytes.Buffer
	limit   int
	dropped int
}

// Write keeps what fits and counts what does not.
//
// It always reports the whole input as written, including the write that
// straddles the limit. A short write is how a writer tells the other end to
// stop, and the other end here is git: reporting 2 of 29 bytes would end the
// command over commentary that nobody was going to read. io.Writer's contract
// asks for an error alongside a short write, so the honest way to keep both is
// to accept everything and drop it deliberately.
func (w *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)

	room := w.limit - w.buffer.Len()
	if room <= 0 {
		w.dropped += written
		return written, nil
	}
	if written > room {
		w.dropped += written - room
		data = data[:room]
	}
	if _, err := w.buffer.Write(data); err != nil {
		return 0, err
	}
	return written, nil
}

// String is the kept output, with a line saying what was left out when
// anything was. Silence there would read as git having said only this much.
func (w *boundedBuffer) String() string {
	text := w.buffer.String()
	if w.dropped == 0 {
		return text
	}
	return text + fmt.Sprintf("\n… (%d more bytes of output were not kept)", w.dropped)
}

// progressWriter tees stderr into a buffer and emits each finished segment to
// OnProgress.
//
// git's progress counter overwrites the same line with carriage returns rather
// than newlines, so both separators end a segment. Empty segments (a bare
// newline after a \r) are dropped — they are the cursor reset, not a report.
type progressWriter struct {
	buffer  *boundedBuffer
	pending []byte
	onLine  func(line string)
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.buffer.Write(data)
	if err != nil {
		return n, err
	}
	w.pending = append(w.pending, data...)
	for {
		i := bytes.IndexAny(w.pending, "\r\n")
		if i < 0 {
			break
		}
		w.emit(w.pending[:i])
		w.pending = w.pending[i+1:]
	}
	return n, nil
}

func (w *progressWriter) flush() {
	w.emit(w.pending)
	w.pending = nil
}

func (w *progressWriter) emit(segment []byte) {
	line := strings.TrimRight(string(segment), " \t")
	if line == "" {
		return
	}
	w.onLine(line)
}

// beginningOf is the start of some output, trimmed and bounded.
//
// The beginning rather than the end because git says why it refused first and
// lists what it refused afterwards. The cut lands on a rune boundary — a
// dangling half of a UTF-8 sequence would reach the log panel as a replacement
// character in the middle of a word — and says that it happened, because
// output that stops mid-sentence with no sign of it reads as git having said
// only that much.
func beginningOf(output []byte, limit int) string {
	text := strings.TrimSpace(string(output))
	if len(text) <= limit {
		return text
	}

	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + "\n… (truncated)"
}

// bothStreams joins what git wrote on its two streams into one explanation.
//
// Whichever is empty is dropped rather than joined: a blank line above git's
// complaint reads as output that went missing, and on the ordinary failure —
// every command that writes nothing to stdout — that blank line would be on
// every error message in the application.
func bothStreams(out, err string) string {
	out, err = strings.TrimSpace(out), strings.TrimSpace(err)
	switch {
	case out == "":
		return err
	case err == "":
		return out
	default:
		return out + "\n" + err
	}
}
