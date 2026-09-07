package git

import "strings"

// A git command, written out the way a person would type it.
//
// Every destructive operation shows its command before it runs, so this is
// what the user reads and then confirms. It is also the quoting the one string
// git itself hands to a shell goes through — GIT_SEQUENCE_EDITOR, see
// docs/adr/0029 — which is why shellQuote lives beside it rather than being
// invented again there.

// CommandLine renders a git command you can paste into a terminal.
//
// Exported for the commands that are SHOWN before they run. A confirmation
// that composed its own text would be describing an argument list it never
// sees — and the pathspecs below are exactly the part that would be dropped,
// because they are the part nobody types by hand.
func CommandLine(args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "git")
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// shellQuote protects an argument so a POSIX shell reads it as one word.
//
// Written for display, and every git command yagit runs still goes through an
// argument slice rather than through a shell. There is exactly one string that
// is executed: GIT_SEQUENCE_EDITOR, which git's own contract defines as a
// shell command and which git appends to without quoting anything itself. See
// sequenceEditor in interactive.go, which quotes its parts with this.
//
// Correct for that, not merely readable enough for it. Each of the three
// branches below yields one word: a bare argument holds no character a shell
// reads; a double-quoted one is reached only when it holds none of `"`, `$`,
// a backtick or a backslash, which are the four a shell still reads inside
// double quotes; and the single-quoted fallback makes every byte literal.
//
// Two quoting styles, because one of them is unreadable at exactly the place
// this matters most. A merge commit's message is `Merge branch 'x' into y`,
// and single-quoting it has to break out of the quotes and back in around
// every apostrophe — four punctuation marks in place of each one. Correct,
// copyable, and nobody's idea of a sentence they are being asked to approve.
// Double quotes carry it whole. They are only reached where the argument holds
// no character the shell would read inside them, so what comes back still
// means the same thing when pasted.
func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " \t\n'\"\\$`*?[]{}()|&;<>#!~") {
		return arg
	}
	if strings.ContainsRune(arg, '\'') && !strings.ContainsAny(arg, "\"$`\\") {
		return `"` + arg + `"`
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}
