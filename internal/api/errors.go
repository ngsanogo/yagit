package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
)

// The shape of error responses enforces the project's central rule: when git
// fails, the interface must be able to show the exact command, its exit code
// and its raw stderr. Never "Something went wrong".

type errorPayload struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Message string      `json:"message"`
	Git     *gitFailure `json:"git,omitempty"`
}

type gitFailure struct {
	// Command is the line as it would be typed in a terminal, so the user can
	// replay it and work out what happened on their own.
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	ExitCode int      `json:"exit_code"`
	Stderr   string   `json:"stderr"`
}

// gitFailureOf pulls git's own account out of an error chain, or nil when git
// was not what failed.
//
// One function rather than one per response shape: a git failure carries the
// same four things wherever it surfaces, and a second copy of this mapping is
// how one of them ends up missing from one of the routes.
func gitFailureOf(cause error) *gitFailure {
	var gitError *git.Error
	if !errors.As(cause, &gitError) {
		return nil
	}
	return &gitFailure{
		Command:  gitError.CommandLine(),
		Args:     gitError.Args,
		ExitCode: gitError.ExitCode,
		Stderr:   gitError.Stderr,
	}
}

// maxLoggedValue bounds what one untrusted field may add to a log line.
//
// One bound for the two kinds that reach forLog, sized for the larger: a
// branch name is never near it, and a git failure's stderr — the useful half
// of the line it appears in — usually is not either. Without a bound, a name
// of a megabyte is a megabyte in the log, once per attempt, from a mistake
// nobody would notice until the disk filled.
const maxLoggedValue = 512

// forLog makes an untrusted string safe to put in a log line.
//
// A log line is a LINE. A branch name holding a newline writes a second entry
// of somebody else's choosing into the daemon's log, and the reader of that
// log has no way to tell it from one yagit wrote — which is the whole of
// CodeQL's go/log-injection, and it is right. slog's own handlers quote most
// of it, but which handler is configured is a property of how the daemon was
// started, and a guarantee that depends on that is not a guarantee.
//
// Two kinds of string reach it, and the second is the less obvious one: a git
// failure's text carries the command's raw stderr, and the command carried a
// name that came from a request. So the value travels back out of git and into
// the same line. Sanitised here rather than at the source, because the raw
// stderr is what the project promises a USER — on screen, in a JSON error,
// where a newline is a newline and not a forged record.
//
// Replaced with a space rather than dropped: a name written to trick a reader
// should still show that something was there.
func forLog(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, value)
	if len(value) > maxLoggedValue {
		return value[:maxLoggedValue] + "…"
	}
	return value
}

// writeError answers in JSON while keeping everything the cause said.
func writeError(writer http.ResponseWriter, logger *slog.Logger, status int, cause error) {
	detail := errorDetail{Message: cause.Error(), Git: gitFailureOf(cause)}

	logger.Warn("error response", "status", status, "error", cause)
	writeJSON(writer, logger, status, errorPayload{Error: detail})
}

// writeJSON serializes into a buffer before writing anything out.
//
// Encoding straight into the ResponseWriter would send the status header
// before an encoding failure surfaced, and the client would get a truncated
// body along with a 200 — the worst kind of response, because it looks fine.
func writeJSON(writer http.ResponseWriter, logger *slog.Logger, status int, payload any) {
	var body bytes.Buffer

	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		logger.Error("cannot encode JSON", "error", err, "payload", fmt.Sprintf("%T", payload))
		http.Error(writer, "cannot encode the response", http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)

	if _, err := body.WriteTo(writer); err != nil {
		// The client left mid-write. There is nobody left to answer, but the
		// incident gets recorded rather than swallowed.
		logger.Warn("response write interrupted", "error", err)
	}
}
