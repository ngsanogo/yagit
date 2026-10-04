package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// The version of mise itself
// ---------------------------------------------------------------------------

// miseAction is the action every workflow installs mise with.
const miseAction = "jdx/mise-action"

// exactVersion is three numbers and nothing else. min_version is a floor to
// mise, and a floor of "2026" would admit every release of the year.
var exactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// checkMiseVersion holds every jdx/mise-action step under .github/ to the
// min_version in mise.toml.
//
// mise.toml holds mise's own version too, as min_version, but CI cannot take
// it from there: the action installs mise before anything reads the file. So
// the number is written twice — min_version, which mise enforces on every
// `./do`, and `version:` on each step, which is what CI installs. A step with
// no `version` is the quiet way the two part: the action then picks a mise of
// its own, a newer one each week, and nothing says so.
func (p *project) checkMiseVersion() error {
	pinned, err := readMinVersion(p.path("mise.toml"))
	if err != nil {
		return err
	}

	var problems []error
	found := 0
	err = filepath.WalkDir(p.path(".github"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml") {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(p.directory, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)

		for _, step := range miseActionSteps(string(contents)) {
			found++
			switch {
			case !step.read:
				problems = append(problems, fmt.Errorf(
					"%s:%d: names %s in a shape this check cannot read: write the step in block style, with `version:` under `with:`",
					name, step.line, miseAction))
			case step.version == "":
				problems = append(problems, fmt.Errorf(
					"%s:%d: %s passes no version, so the action picks the mise: add `version: %s` under `with:`",
					name, step.line, miseAction, pinned))
			case step.version != pinned:
				problems = append(problems, fmt.Errorf(
					"%s:%d: %s installs mise %s, but min_version in mise.toml is %s",
					name, step.line, miseAction, step.version, pinned))
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reading .github: %w", err)
	}

	// A check that finds nothing to check passes forever, and a workflow moved
	// out of .github would look exactly like agreement.
	if found == 0 {
		return fmt.Errorf("no %s step under .github: nothing holds the mise CI installs to mise.toml", miseAction)
	}
	return errors.Join(problems...)
}

// readMinVersion reads the top-level min_version of a mise.toml.
//
// Only the lines above the first table header count. Below one, the same line
// is a key of that table, which mise does not read as its own floor — and
// accepting it there would approve the one mistake TOML makes easy here.
func readMinVersion(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading mise.toml: %w", err)
	}

	for _, line := range strings.Split(string(contents), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			break
		}
		key, value, isAssignment := strings.Cut(line, "=")
		if !isAssignment || strings.TrimSpace(key) != "min_version" {
			continue
		}

		// A version holds no `#`, so the first one starts the comment.
		value, _, _ = strings.Cut(value, "#")
		value = strings.TrimSpace(value)
		if version := unquote(value); exactVersion.MatchString(version) {
			return version, nil
		}
		return "", fmt.Errorf("mise.toml: min_version = %s is not an exact MAJOR.MINOR.PATCH version", value)
	}
	return "", fmt.Errorf("mise.toml sets no top-level min_version, above its first [table]: nothing pins mise itself")
}

// miseActionStep is one use of jdx/mise-action in a workflow.
type miseActionStep struct {
	line    int    // of the `uses:` line, counted from 1
	version string // what it passes as `version`, or "" when nothing
	read    bool   // false for a step written in a shape this does not parse
}

// yamlKey matches a `key: value` line of a block mapping, item marker
// included. Group 1 is everything before the key, so its length is the key's
// column.
var yamlKey = regexp.MustCompile(`^( *(?:- +)?)([A-Za-z0-9_-]+):(?:\s+(.*))?$`)

// yamlLine is one line of a workflow, as far as finding a step's `with:`
// needs it.
type yamlLine struct {
	column int    // where the content starts, after any `- ` marker
	item   bool   // it opens a sequence item
	key    string // "" when the line is not `key: value`
	value  string // the key's value, comment and quotes removed
	blank  bool   // empty or only a comment, so it bounds nothing
	text   string // the line without its comment
}

// miseActionSteps finds every jdx/mise-action step in a workflow and what
// `version` it passes.
//
// A line reader rather than a YAML parser: this program has no YAML parser
// and one check is not a reason to depend on one. Workflows are block-style
// mappings, which is what it reads. Any other `uses:` line that names the
// action comes back unread rather than skipped, so a shape it does not know
// fails the check instead of escaping it.
func miseActionSteps(contents string) []miseActionStep {
	texts := strings.Split(contents, "\n")
	lines := make([]yamlLine, len(texts))
	for index, text := range texts {
		lines[index] = readYAMLLine(text)
	}

	var steps []miseActionStep
	for index, line := range lines {
		switch {
		case line.key == "uses" && strings.HasPrefix(line.value, miseAction+"@"):
			start, end := stepAround(lines, index)
			version, read := withVersion(lines[start:end], line.column)
			steps = append(steps, miseActionStep{line: index + 1, version: version, read: read})
		case strings.Contains(line.text, "uses:") && strings.Contains(line.text, miseAction):
			steps = append(steps, miseActionStep{line: index + 1})
		}
	}
	return steps
}

func readYAMLLine(text string) yamlLine {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return yamlLine{blank: true}
	}

	// A YAML comment starts at a `#` that follows whitespace.
	if at := strings.Index(text, " #"); at >= 0 {
		text = text[:at]
	}

	if match := yamlKey.FindStringSubmatch(text); match != nil {
		return yamlLine{
			column: len(match[1]),
			item:   strings.Contains(match[1], "-"),
			key:    match[2],
			value:  unquote(strings.TrimSpace(match[3])),
			text:   text,
		}
	}

	// Anything else — a scalar item, a line of a `run: |` block — counts only
	// by where it starts, which is all that decides whether it is still inside
	// a step.
	return yamlLine{column: len(text) - len(strings.TrimLeft(text, " ")), text: text}
}

// stepAround returns the lines of the step whose key sits at index: back to
// the `- ` that opens it, forward to the next line that is not inside it.
//
// `uses:` is not always the step's first key, so the search goes both ways —
// `with:` may come before it.
func stepAround(lines []yamlLine, index int) (start, end int) {
	column := lines[index].column

	start = index
	for start > 0 && (!lines[start].item || lines[start].column != column) {
		previous := lines[start-1]
		if !previous.blank && previous.column < column {
			break
		}
		start--
	}

	end = index + 1
	for end < len(lines) {
		next := lines[end]
		if !next.blank && (next.column < column || (next.column == column && next.item)) {
			break
		}
		end++
	}
	return start, end
}

// withVersion returns the `version` a step passes under `with:`, or "" — and
// false when `with:` is written inline, as a flow mapping this does not read.
func withVersion(step []yamlLine, column int) (string, bool) {
	for index, line := range step {
		if line.key != "with" || line.column != column {
			continue
		}
		if line.value != "" {
			return "", false
		}

		// The first key under `with:` sets the column of all of them. A
		// `version:` deeper than that belongs to something else — a line of
		// a block scalar, say.
		inner := -1
		for _, nested := range step[index+1:] {
			if nested.blank {
				continue
			}
			if nested.column <= column {
				break
			}
			if inner < 0 {
				inner = nested.column
			}
			if nested.column == inner && nested.key == "version" {
				return nested.value, true
			}
		}
	}
	return "", true
}
