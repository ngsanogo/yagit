package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Blame of one path at one revision: who last touched each line.
//
// --line-porcelain rather than --porcelain: the shorter form omits commit
// fields after the first time a SHA appears, so a parser has to keep a map of
// every commit it has seen. Repeating the fields on every line costs more
// bytes and means one fewer place for a silent wrong answer — the same trade
// ParseRemotes makes by refusing a line it cannot read.
//
// The content of each line travels in the same answer. A second `git show`
// for the blob would be another definition of "the file at this revision",
// and the two could disagree about a rename --follow would have resolved.

// BlameLine is one annotated line of a file.
type BlameLine struct {
	// Number is 1-based in the final file, matching what editors show.
	Number int `json:"number"`

	Text string `json:"text"`

	SHA     string    `json:"sha"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`
}

// BlameResult is what Blame answers.
type BlameResult struct {
	Path     string      `json:"path"`
	Revision string      `json:"revision"`
	Lines    []BlameLine `json:"lines"`
}

// Blame annotates path as it stood at revision (or HEAD when revision is empty).
func (r *Runner) Blame(ctx context.Context, dir, revision, path string) (BlameResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return BlameResult{}, ErrNoPaths
	}

	revision = strings.TrimSpace(revision)
	if revision == "" {
		revision = "HEAD"
	}
	if err := checkRevision(revision); err != nil {
		return BlameResult{}, err
	}

	args := []string{"blame", "--line-porcelain", revision, "--", path}

	// The path goes after `--` rather than through literalPathspecs. Blame does
	// not expand pathspec magic — `:(literal)notes.md` is looked up as a file
	// of that exact name — so the separator is the protection against a path
	// that looks like an option, and checkedPath on the API side is the
	// protection against one that leaves the repository.
	output, err := r.Exec(ctx, Command{Dir: dir, Args: args, MaxOutput: maxDiffBytes})
	if err != nil {
		return BlameResult{}, err
	}

	lines, err := ParseBlame(output)
	if err != nil {
		return BlameResult{}, fmt.Errorf("blame of %s: %w", path, err)
	}
	return BlameResult{Path: path, Revision: revision, Lines: lines}, nil
}

// ParseBlame turns `git blame --line-porcelain` into lines. Pure and exported
// so a broken record is a unit-test failure rather than a silent wrong gutter.
func ParseBlame(output []byte) ([]BlameLine, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	// A single blame line's text can be long; keep the default token size and
	// raise the ceiling to the same order as maxDiffBytes so a huge line is
	// refused by the Exec cap above rather than by the scanner.
	scanner.Buffer(make([]byte, 0, 64*1024), maxDiffBytes)

	var lines []BlameLine
	for scanner.Scan() {
		header := scanner.Text()
		if header == "" {
			continue
		}

		sha, final, err := parseBlameHeader(header)
		if err != nil {
			return nil, err
		}

		author := ""
		summary := ""
		var authorTime time.Time
		var text string
		sawText := false

		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "\t") {
				text = strings.TrimPrefix(line, "\t")
				sawText = true
				break
			}
			key, value, cut := strings.Cut(line, " ")
			if !cut {
				// `boundary` and similar flags have no value.
				continue
			}
			switch key {
			case "author":
				author = value
			case "author-time":
				seconds, parseErr := strconv.ParseInt(value, 10, 64)
				if parseErr != nil {
					return nil, fmt.Errorf("author-time %q: %w", value, parseErr)
				}
				authorTime = time.Unix(seconds, 0).UTC()
			case "summary":
				summary = value
			}
		}
		if !sawText {
			return nil, fmt.Errorf("blame header %q was not followed by a content line", header)
		}

		lines = append(lines, BlameLine{
			Number:  final,
			Text:    text,
			SHA:     sha,
			Author:  author,
			Date:    authorTime,
			Subject: summary,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func parseBlameHeader(header string) (sha string, final int, err error) {
	fields := strings.Fields(header)
	// sha orig final [group-size]
	if len(fields) < 3 || len(fields) > 4 {
		return "", 0, fmt.Errorf("blame header %q: want sha orig final [n]", header)
	}
	if len(fields[0]) != 40 && len(fields[0]) != 64 {
		return "", 0, fmt.Errorf("blame header %q: object name is %d characters", header, len(fields[0]))
	}
	final, err = strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, fmt.Errorf("blame header %q: final line: %w", header, err)
	}
	return fields[0], final, nil
}
