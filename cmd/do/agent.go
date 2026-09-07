package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// agentManifest lists rules, skills and denied paths under agent/. The YAML is
// parsed by hand so ./do stays free of third-party dependencies — the shape is
// fixed.
type agentManifest struct {
	Rules  []agentRule
	Skills []string
	Deny   []string
}

type agentRule struct {
	ID          string
	Description string
	Always      bool
	Paths       []string
	File        string
}

func runAgent(p *project, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ./do agent sync|check")
	}

	switch args[0] {
	case "sync":
		return agentSync(p)
	case "check":
		return agentCheck(p, true)
	default:
		return fmt.Errorf("unknown agent subcommand: %q. Use sync or check", args[0])
	}
}

func agentSync(p *project) error {
	manifest, err := loadAgentManifest(p.path("agent", "manifest.yaml"))
	if err != nil {
		return err
	}

	info("syncing agent configuration")

	if err := writeShimReadme(p.path("cursor"), "cursor"); err != nil {
		return err
	}
	if err := writeShimReadme(p.path("claude"), "claude"); err != nil {
		return err
	}

	if err := os.MkdirAll(p.path("cursor", "rules"), 0o755); err != nil {
		return fmt.Errorf("creating cursor/rules: %w", err)
	}
	if err := os.MkdirAll(p.path("claude", "rules"), 0o755); err != nil {
		return fmt.Errorf("creating claude/rules: %w", err)
	}
	if err := os.MkdirAll(p.path("cursor", "skills"), 0o755); err != nil {
		return fmt.Errorf("creating cursor/skills: %w", err)
	}
	if err := os.MkdirAll(p.path("claude", "skills"), 0o755); err != nil {
		return fmt.Errorf("creating claude/skills: %w", err)
	}

	for _, rule := range manifest.Rules {
		body, err := os.ReadFile(p.path("agent", rule.File))
		if err != nil {
			return fmt.Errorf("reading agent/%s: %w", rule.File, err)
		}

		cursorPath := p.path("cursor", "rules", rule.ID+".mdc")
		if err := os.WriteFile(cursorPath, renderCursorRule(rule, body), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", cursorPath, err)
		}

		claudePath := p.path("claude", "rules", rule.ID+".md")
		if err := os.WriteFile(claudePath, renderClaudeRule(rule, body), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", claudePath, err)
		}
	}

	for _, skill := range manifest.Skills {
		target := filepath.Join("..", "..", "agent", "skills", skill)
		if err := p.replaceSymlink(p.path("cursor", "skills", skill), target); err != nil {
			return fmt.Errorf("linking cursor/skills/%s: %w", skill, err)
		}
		if err := p.replaceSymlink(p.path("claude", "skills", skill), target); err != nil {
			return fmt.Errorf("linking claude/skills/%s: %w", skill, err)
		}
	}

	if err := p.replaceSymlink(p.path("claude", "CLAUDE.md"), filepath.Join("..", "agent", "AGENTS.md")); err != nil {
		return fmt.Errorf("linking claude/CLAUDE.md: %w", err)
	}

	settings, err := renderClaudeSettings(manifest.Deny)
	if err != nil {
		return err
	}
	settingsPath := p.path("claude", "settings.json")
	if err := os.WriteFile(settingsPath, settings, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", settingsPath, err)
	}

	rootLinks := []struct {
		link   string
		target string
	}{
		{p.path("AGENTS.md"), filepath.Join("agent", "AGENTS.md")},
		{p.path("CLAUDE.md"), filepath.Join("claude", "CLAUDE.md")},
		{p.path(".cursor"), "cursor"},
		{p.path(".claude"), "claude"},
	}

	for _, entry := range rootLinks {
		if err := p.replaceSymlink(entry.link, entry.target); err != nil {
			return fmt.Errorf("linking %s: %w", filepath.Base(entry.link), err)
		}
	}

	return nil
}

func agentCheck(p *project, verbose bool) error {
	manifest, err := loadAgentManifest(p.path("agent", "manifest.yaml"))
	if err != nil {
		return err
	}

	if verbose {
		info("checking agent configuration")
	}

	for _, rule := range manifest.Rules {
		body, err := os.ReadFile(p.path("agent", rule.File))
		if err != nil {
			return fmt.Errorf("reading agent/%s: %w", rule.File, err)
		}

		cursorWant := renderCursorRule(rule, body)
		cursorHave, err := os.ReadFile(p.path("cursor", "rules", rule.ID+".mdc"))
		if err != nil {
			return fmt.Errorf("cursor/rules/%s.mdc is missing or unreadable: run ./do agent sync", rule.ID)
		}
		if !bytes.Equal(cursorHave, cursorWant) {
			return fmt.Errorf("cursor/rules/%s.mdc is out of date: run ./do agent sync", rule.ID)
		}

		claudeWant := renderClaudeRule(rule, body)
		claudeHave, err := os.ReadFile(p.path("claude", "rules", rule.ID+".md"))
		if err != nil {
			return fmt.Errorf("claude/rules/%s.md is missing or unreadable: run ./do agent sync", rule.ID)
		}
		if !bytes.Equal(claudeHave, claudeWant) {
			return fmt.Errorf("claude/rules/%s.md is out of date: run ./do agent sync", rule.ID)
		}
	}

	// Beside the rules, because it is rendered from the manifest exactly as
	// they are: everything below this point is a link rather than a file, and
	// checks a different kind of mistake.
	settingsWant, err := renderClaudeSettings(manifest.Deny)
	if err != nil {
		return err
	}
	settingsHave, err := os.ReadFile(p.path("claude", "settings.json"))
	if err != nil {
		return fmt.Errorf("claude/settings.json is missing or unreadable: run ./do agent sync")
	}
	if !bytes.Equal(settingsHave, settingsWant) {
		return fmt.Errorf("claude/settings.json is out of date: run ./do agent sync")
	}

	for _, skill := range manifest.Skills {
		want := filepath.Join("..", "..", "agent", "skills", skill)
		for _, layer := range []string{"cursor", "claude"} {
			link := p.path(layer, "skills", skill)
			if err := checkSymlink(link, want); err != nil {
				return fmt.Errorf("%s/skills/%s: %w: run ./do agent sync", layer, skill, err)
			}
		}
	}

	rootLinks := []struct {
		link   string
		target string
	}{
		{p.path("AGENTS.md"), filepath.Join("agent", "AGENTS.md")},
		{p.path("CLAUDE.md"), filepath.Join("claude", "CLAUDE.md")},
		{p.path(".cursor"), "cursor"},
		{p.path(".claude"), "claude"},
	}

	for _, entry := range rootLinks {
		if err := checkSymlink(entry.link, entry.target); err != nil {
			return fmt.Errorf("%s: %w: run ./do agent sync", filepath.Base(entry.link), err)
		}
	}

	if err := checkSymlink(p.path("claude", "CLAUDE.md"), filepath.Join("..", "agent", "AGENTS.md")); err != nil {
		return fmt.Errorf("claude/CLAUDE.md: %w: run ./do agent sync", err)
	}

	return nil
}

func renderCursorRule(rule agentRule, body []byte) []byte {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("description: ")
	builder.WriteString(rule.Description)
	builder.WriteByte('\n')
	if rule.Always {
		builder.WriteString("alwaysApply: true\n")
	} else {
		builder.WriteString("globs: ")
		builder.WriteString(strings.Join(rule.Paths, ","))
		builder.WriteByte('\n')
		builder.WriteString("alwaysApply: false\n")
	}
	builder.WriteString("---\n\n")
	builder.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func renderClaudeRule(rule agentRule, body []byte) []byte {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("description: ")
	builder.WriteString(rule.Description)
	builder.WriteByte('\n')
	if !rule.Always && len(rule.Paths) > 0 {
		builder.WriteString("paths:\n")
		for _, path := range rule.Paths {
			builder.WriteString("  - \"")
			builder.WriteString(path)
			builder.WriteString("\"\n")
		}
	}
	builder.WriteString("---\n\n")
	builder.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

// claudeSettings is the shape written to claude/settings.json. A struct rather
// than a map: encoding/json sorts a map's keys but keeps a struct's field
// order, and agentCheck compares this file byte for byte.
type claudeSettings struct {
	Permissions claudePermissions `json:"permissions"`
}

type claudePermissions struct {
	Deny []string `json:"deny"`
}

// renderClaudeSettings turns the manifest's repository-relative paths into
// Claude Code's permission rules.
//
// The leading slash is neither a typo nor an absolute path. In a project
// settings file it anchors the pattern at the working directory, which is the
// repository root; a pattern without it is a gitignore-style name that a deny
// rule matches at *any* depth. "dist/**" would therefore also cover
// internal/assets/dist and every dist inside node_modules — a wider rule than
// the manifest asked for, and one nobody reading the manifest would expect.
//
// Only Claude gets a file. Cursor's shim has no counterpart here because its
// settings schema is not something this project has established; an invented
// one that silently matches nothing is worse than none.
func renderClaudeSettings(deny []string) ([]byte, error) {
	rules := make([]string, 0, len(deny))
	for _, path := range deny {
		// A leading slash here renders "Read(//dist/**)", which Claude Code
		// reads as an absolute path from the filesystem root: a rule that
		// matches nothing in the checkout and says so nowhere. This
		// repository's own .gitignore anchors its paths exactly that way
		// (/.env, /dist/, /web/node_modules/), so copying one across is the
		// obvious way to write an entry — and the silent way to write one that
		// denies nothing.
		if path == "" || strings.HasPrefix(path, "/") {
			return nil, fmt.Errorf("deny entry %q: write it relative to the repository root, without a leading slash", path)
		}
		rules = append(rules, "Read(/"+path+")")
	}

	encoded, err := json.MarshalIndent(claudeSettings{Permissions: claudePermissions{Deny: rules}}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the agent settings: %w", err)
	}
	return append(encoded, '\n'), nil
}

func writeShimReadme(directory, name string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	readme := fmt.Sprintf(`# %s shim

This directory is generated from [agent/](../agent/). Do not edit files here by hand.

Run:

`+"```sh"+`
./do agent sync
`+"```"+`

See [agent/README.md](../agent/README.md) for how agent configuration works.
`, name)

	return os.WriteFile(filepath.Join(directory, "README.md"), []byte(readme), 0o644)
}

// replaceSymlink creates link pointing at targetRelative, which is interpreted
// relative to link's parent directory.
func (p *project) replaceSymlink(link, targetRelative string) error {
	if err := os.RemoveAll(link); err != nil {
		return err
	}

	parent := filepath.Dir(link)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	absoluteTarget := filepath.Clean(filepath.Join(parent, targetRelative))
	relative, err := filepath.Rel(parent, absoluteTarget)
	if err != nil {
		return err
	}

	if p.copyShims {
		return copyAsLinkFallback(link, absoluteTarget)
	}

	if err := os.Symlink(relative, link); err != nil {
		// Windows without the symlink privilege cannot create one, and a
		// duplicate is the only shim such a checkout can have. Anywhere else a
		// failed symlink is a real failure: copying would hide it behind a
		// shim that `agent check` then accepts.
		if runtime.GOOS == "windows" {
			return copyAsLinkFallback(link, absoluteTarget)
		}
		return err
	}
	return nil
}

func copyAsLinkFallback(link, target string) error {
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDirectory(link, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	return os.WriteFile(link, data, info.Mode().Perm())
}

func copyDirectory(destination, source string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}

// checkSymlink reports whether link stands for the file or directory at
// wantRelative, which is read relative to link's own parent.
//
// "Stands for", not "points at", because on Windows it may not point at
// anything. replaceSymlink falls back to copying where the process has no
// right to create a symlink, and asking a copy where it points is the wrong
// question — one that used to answer "points to X, want Y: run ./do agent
// sync", to a contributor for whom running sync would produce the same copy
// and the same complaint, forever.
func checkSymlink(link, wantRelative string) error {
	parent := filepath.Dir(link)
	want := filepath.Clean(filepath.Join(parent, wantRelative))

	entry, err := os.Lstat(link)
	if err != nil {
		return fmt.Errorf("missing")
	}

	if entry.Mode()&os.ModeSymlink == 0 {
		return sameContent(link, want)
	}

	// EvalSymlinks resolves the whole chain, so one call answers it. Both
	// sides go through it: the target may itself sit behind a link — a
	// checkout under a symlinked home directory is the ordinary case — and
	// comparing a resolved path against an unresolved one fails on a tree
	// that is perfectly correct.
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		return fmt.Errorf("broken symlink")
	}
	resolvedWant, err := filepath.EvalSymlinks(want)
	if err != nil {
		resolvedWant = want
	}

	if got != resolvedWant {
		return fmt.Errorf("points to %q, want %q", got, resolvedWant)
	}
	return nil
}

// sameContent compares a copy against what it was copied from — the check for
// the Windows fallback, where a shim entry is a duplicate rather than a link.
func sameContent(copied, source string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("nothing at %q to compare against", source)
	}

	if !sourceInfo.IsDir() {
		want, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("reading %q: %w", source, err)
		}
		have, err := os.ReadFile(copied)
		if err != nil {
			return fmt.Errorf("reading %q: %w", copied, err)
		}
		if !bytes.Equal(have, want) {
			return fmt.Errorf("is a copy of %q that has fallen behind it", source)
		}
		return nil
	}

	return filepath.WalkDir(source, func(path string, item os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if item.IsDir() {
			return nil
		}
		return sameContent(filepath.Join(copied, relative), path)
	})
}

func loadAgentManifest(path string) (*agentManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseAgentManifest(string(data))
}

func parseAgentManifest(text string) (*agentManifest, error) {
	manifest := &agentManifest{}
	lines := strings.Split(text, "\n")

	// section names the top-level key being read. One variable rather than a
	// flag per section: with flags, adding a fourth key means remembering to
	// clear three others, and the one that gets forgotten files entries under
	// the wrong list without ever failing.
	var (
		section string
		rule    *agentRule
		inPaths bool
	)

	flushRule := func() {
		if rule != nil && rule.ID != "" {
			manifest.Rules = append(manifest.Rules, *rule)
		}
		rule = nil
		inPaths = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		switch {
		case trimmed == "rules:" || trimmed == "skills:" || trimmed == "deny:":
			flushRule()
			section = strings.TrimSuffix(trimmed, ":")
			continue
		// Any other unindented line is a top-level key this parser does not
		// know, and it ends the section above it. Without this, a fourth key
		// added below deny: — the last section today — would have its entries
		// filed as denied paths and rendered into settings.json as rules that
		// protect nothing.
		case !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trimmed, "- "):
			flushRule()
			section = ""
			continue
		case section == "skills" && strings.HasPrefix(trimmed, "- "):
			manifest.Skills = append(manifest.Skills, listEntry(trimmed))
			continue
		case section == "deny" && strings.HasPrefix(trimmed, "- "):
			manifest.Deny = append(manifest.Deny, listEntry(trimmed))
			continue
		case section == "rules" && strings.HasPrefix(trimmed, "- id:"):
			flushRule()
			rule = &agentRule{ID: strings.TrimSpace(strings.TrimPrefix(trimmed, "- id:"))}
			continue
		case section == "rules" && rule != nil && strings.HasPrefix(trimmed, "description:"):
			rule.Description = strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
			continue
		case section == "rules" && rule != nil && trimmed == "always: true":
			rule.Always = true
			continue
		case section == "rules" && rule != nil && trimmed == "paths:":
			inPaths = true
			continue
		case section == "rules" && rule != nil && strings.HasPrefix(trimmed, "file:"):
			rule.File = strings.TrimSpace(strings.TrimPrefix(trimmed, "file:"))
			inPaths = false
			continue
		case section == "rules" && rule != nil && inPaths && strings.HasPrefix(trimmed, "- "):
			rule.Paths = append(rule.Paths, listEntry(trimmed))
		}
	}
	flushRule()

	if len(manifest.Rules) == 0 {
		return nil, fmt.Errorf("manifest has no rules")
	}
	if len(manifest.Skills) == 0 {
		return nil, fmt.Errorf("manifest has no skills")
	}
	// An empty deny list would render a settings file that grants everything,
	// silently, from a manifest that merely lost a section.
	if len(manifest.Deny) == 0 {
		return nil, fmt.Errorf("manifest denies no path")
	}
	return manifest, nil
}

// listEntry reads one "- value" item, with or without the quotes YAML allows
// around a glob — either kind. A single-quoted entry left with its quotes on
// renders as Read(/'.env'), a rule that matches no file and reports nothing.
func listEntry(line string) string {
	return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "- ")), `"'`)
}
