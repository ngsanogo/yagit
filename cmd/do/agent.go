package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// agentLink is one published name for a file that lives under agent/. The
// name is what check reports; the link is where an agent looks; the target is
// relative to the link's parent, the same way a symlink is stored.
type agentLink struct {
	name   string
	link   string
	target string
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

// agentLinks are the only names the repository publishes for agent/. Anything
// a particular tool looks for besides these is that person's checkout, and
// .gitignore keeps it there.
func agentLinks(p *project) []agentLink {
	return []agentLink{
		{name: "AGENTS.md", link: p.path("AGENTS.md"), target: filepath.Join("agent", "AGENTS.md")},
		{name: ".agents/skills", link: p.path(".agents", "skills"), target: filepath.Join("..", "agent", "skills")},
	}
}

func agentSync(p *project) error {
	info("syncing agent configuration")

	for _, entry := range agentLinks(p) {
		// A link to a path that is not there is a quiet success followed by a
		// check that says "broken". Fail while the missing file is still the
		// subject of the sentence.
		parent := filepath.Dir(entry.link)
		if _, err := os.Stat(filepath.Clean(filepath.Join(parent, entry.target))); err != nil {
			return fmt.Errorf("linking %s: %w", entry.name, err)
		}
		if err := p.replaceSymlink(entry.link, entry.target); err != nil {
			return fmt.Errorf("linking %s: %w", entry.name, err)
		}
	}
	return nil
}

func agentCheck(p *project, verbose bool) error {
	if verbose {
		info("checking agent configuration")
	}

	for _, entry := range agentLinks(p) {
		if err := checkSymlink(entry.link, entry.target); err != nil {
			return fmt.Errorf("%s: %w: run ./do agent sync", entry.name, err)
		}
	}
	return nil
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
		// duplicate is the only link such a checkout can have. Anywhere else a
		// failed symlink is a real failure: copying would hide it behind a
		// file that `agent check` then accepts.
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
// the Windows fallback, where a published link is a duplicate rather than a
// symlink.
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
