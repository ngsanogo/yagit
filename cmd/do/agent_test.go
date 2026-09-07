package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAgentManifest(t *testing.T) {
	t.Parallel()

	manifest, err := parseAgentManifest(`rules:
  - id: core
    description: Core principles
    always: true
    file: rules/core.md
  - id: go
    description: Go conventions
    paths:
      - "**/*.go"
    file: rules/go.md

skills:
  - dev-workflow
  - commit

deny:
  # A comment between entries, which the manifest uses to say why each path
  # is denied.
  - ".env"
  - "web/node_modules/**"
`)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Rules) != 2 {
		t.Fatalf("rules: got %d, want 2", len(manifest.Rules))
	}
	if manifest.Rules[0].ID != "core" || !manifest.Rules[0].Always {
		t.Fatalf("core rule: %+v", manifest.Rules[0])
	}
	if manifest.Rules[1].ID != "go" || len(manifest.Rules[1].Paths) != 1 {
		t.Fatalf("go rule: %+v", manifest.Rules[1])
	}
	if len(manifest.Skills) != 2 || manifest.Skills[0] != "dev-workflow" {
		t.Fatalf("skills: %+v", manifest.Skills)
	}
	// Unquoted, and filed under deny rather than skills: the two sections hold
	// the same YAML shape, and the parser tells them apart by which key it saw
	// last.
	if len(manifest.Deny) != 2 || manifest.Deny[0] != ".env" || manifest.Deny[1] != "web/node_modules/**" {
		t.Fatalf("deny: %+v", manifest.Deny)
	}
}

// A manifest that lost a section must fail, not render an emptier shim.
//
// The deny list is the one where silence is dangerous: no rules and no skills
// give an agent nothing to work from and are noticed within a session, while
// an empty deny list produces a valid settings file that grants everything and
// reads exactly like one that was never meant to deny anything.
func TestParseAgentManifestRefusesAMissingSection(t *testing.T) {
	t.Parallel()

	// The error is asserted, not just its presence: all three refusals read
	// alike from the outside, so a parser that answered "no rules" to every one
	// of them would pass a test that only checked err != nil.
	sections := []struct {
		missing string
		says    string
		text    string
	}{
		{"rules", "rules", `skills:
  - commit

deny:
  - ".env"
`},
		{"skills", "skills", `rules:
  - id: core
    description: Core principles
    always: true
    file: rules/core.md

deny:
  - ".env"
`},
		{"deny", "denies no path", `rules:
  - id: core
    description: Core principles
    always: true
    file: rules/core.md

skills:
  - commit
`},
	}

	for _, section := range sections {
		t.Run("no "+section.missing, func(t *testing.T) {
			t.Parallel()

			_, err := parseAgentManifest(section.text)
			if err == nil {
				t.Fatalf("a manifest with no %s section must be refused", section.missing)
			}
			if !strings.Contains(err.Error(), section.says) {
				t.Errorf("error = %q, want it to name the missing %s section", err, section.missing)
			}
		})
	}
}

// A key the parser does not know ends the section above it.
//
// deny: is the last section in the manifest today, so the next top-level key
// anyone adds lands directly under it. Filed by the section that came before,
// its entries would be rendered into settings.json as Read() rules over paths
// nobody meant to deny — and nothing would report it.
func TestParseAgentManifestEndsASectionAtAnUnknownKey(t *testing.T) {
	t.Parallel()

	manifest, err := parseAgentManifest(`rules:
  - id: core
    description: Core principles
    always: true
    file: rules/core.md

skills:
  - commit

deny:
  - ".env"

hooks:
  - format-on-save
`)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Deny) != 1 || manifest.Deny[0] != ".env" {
		t.Errorf("deny = %+v, want only the entry written under deny:", manifest.Deny)
	}
}

// A quoted entry is a quoted entry, whichever quote YAML was written with.
//
// Left on, they travel into the rendered rule: Read(/'.env') matches no file,
// and a deny rule that matches nothing fails silently by construction.
func TestParseAgentManifestReadsEitherQuote(t *testing.T) {
	t.Parallel()

	manifest, err := parseAgentManifest(`rules:
  - id: core
    description: Core principles
    always: true
    file: rules/core.md

skills:
  - commit

deny:
  - '.env'
  - dist/**
`)
	if err != nil {
		t.Fatal(err)
	}

	if len(manifest.Deny) != 2 || manifest.Deny[0] != ".env" || manifest.Deny[1] != "dist/**" {
		t.Errorf("deny = %+v, want the entries without their quotes", manifest.Deny)
	}
}

// An entry anchored the way .gitignore anchors its paths must be refused.
//
// "/dist/**" renders as Read(//dist/**), which Claude Code reads as an
// absolute path from the filesystem root: a rule over a /dist that does not
// exist, in place of the one the manifest asked for. The settings file stays
// valid, the gate stays green, and the path is read.
func TestRenderClaudeSettingsRefusesAnAnchoredEntry(t *testing.T) {
	t.Parallel()

	for _, entry := range []string{"/dist/**", "/.env", ""} {
		if _, err := renderClaudeSettings([]string{entry}); err == nil {
			t.Errorf("deny entry %q must be refused", entry)
		}
	}
}

// The leading slash is the whole point of the renderer.
//
// Without it a deny rule is a gitignore name that matches at any depth, so
// "dist/**" would also cover internal/assets/dist and every dist under
// node_modules — a rule wider than the manifest asked for. With it, the
// pattern is anchored at the repository root, which is what each entry means.
func TestRenderClaudeSettings(t *testing.T) {
	t.Parallel()

	rendered, err := renderClaudeSettings([]string{".env", "dist/**"})
	if err != nil {
		t.Fatal(err)
	}

	var settings claudeSettings
	if err := json.Unmarshal(rendered, &settings); err != nil {
		t.Fatalf("the rendered settings are not valid JSON: %v\n%s", err, rendered)
	}

	want := []string{"Read(/.env)", "Read(/dist/**)"}
	if len(settings.Permissions.Deny) != len(want) {
		t.Fatalf("deny = %+v, want %+v", settings.Permissions.Deny, want)
	}
	for i, rule := range want {
		if settings.Permissions.Deny[i] != rule {
			t.Errorf("deny[%d] = %q, want %q", i, settings.Permissions.Deny[i], rule)
		}
	}

	// agentCheck compares this file byte for byte, so a trailing newline it
	// does not render is a gate that fails on a file no editor would leave
	// alone.
	if !bytes.HasSuffix(rendered, []byte("\n")) {
		t.Errorf("rendered settings = %q, want a trailing newline", rendered)
	}
}

func TestRenderCursorRule(t *testing.T) {
	t.Parallel()

	body := []byte("# Title\n")
	got := string(renderCursorRule(agentRule{
		ID:          "go",
		Description: "Go conventions",
		Paths:       []string{"**/*.go", "cmd/**"},
		File:        "rules/go.md",
	}, body))

	if !strings.Contains(got, "globs: **/*.go,cmd/**") || !strings.Contains(got, "alwaysApply: false") || !strings.Contains(got, "# Title") {
		t.Fatalf("cursor rule:\n%s", got)
	}
}

func TestRenderClaudeRule(t *testing.T) {
	t.Parallel()

	body := []byte("# Title\n")
	got := string(renderClaudeRule(agentRule{
		ID:          "core",
		Description: "Core principles",
		Always:      true,
		File:        "rules/core.md",
	}, body))

	if !strings.Contains(got, "description: Core principles") || !strings.Contains(got, "# Title") {
		t.Fatalf("claude rule:\n%s", got)
	}
	if strings.Contains(got, "paths:") {
		t.Fatalf("always rule should not list paths:\n%s", got)
	}
}

// Sync produces something check accepts, and check notices when it stops.
//
// Both shim trees, because there are two. A symlink where the process may
// create one, and a duplicate where it may not — Windows without the
// privilege, which is the half `./do agent sync` used to run on no machine
// that tested it. The copy is asked for rather than waited for: os.Symlink
// succeeds everywhere these tests run.
//
// Against a copy of agent/, never the checkout itself. Running sync from a
// test rewrote the shims of whoever ran `./do test go` — which is exactly the
// gate `./do agent check` exists to be: a contributor who edited agent/ and
// forgot to sync would have had the tests quietly do it for them, and the
// gate would only ever pass.
func TestAgentSyncAndCheck(t *testing.T) {
	source := filepath.Join("..", "..", "agent")
	if _, err := os.Stat(filepath.Join(source, "manifest.yaml")); err != nil {
		t.Skip("agent/ not present in this checkout")
	}

	for _, shims := range []struct {
		name   string
		copied bool
	}{
		{"linked", false},
		{"copied", true},
	} {
		t.Run(shims.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := copyDirectory(filepath.Join(directory, "agent"), source); err != nil {
				t.Fatalf("copying agent/: %v", err)
			}

			project := &project{directory: directory, copyShims: shims.copied}

			if err := agentSync(project); err != nil {
				t.Fatalf("sync: %v", err)
			}
			if err := agentCheck(project, false); err != nil {
				t.Fatalf("check straight after sync: %v", err)
			}

			// Without this the copied case would pass by being the linked case
			// over again: a seam nothing reads leaves symlinks behind, and
			// every assertion below still holds.
			entry, err := os.Lstat(filepath.Join(directory, "AGENTS.md"))
			if err != nil {
				t.Fatalf("AGENTS.md: %v", err)
			}
			if linked := entry.Mode()&os.ModeSymlink != 0; linked == shims.copied {
				t.Fatalf("AGENTS.md is a symlink: %v, want %v", linked, !shims.copied)
			}

			// The deny list goes first, while nothing else has drifted: it is
			// rendered from the manifest rather than from a rule body, so an
			// edited manifest is its own way to fall behind — and the only one
			// that leaves an agent reading a path the manifest says it should
			// not. Checked here, the error is about the settings and not about
			// whatever the later assertions have already broken.
			// The key is written out again rather than the entry alone: an
			// appended list item belongs to whatever section the manifest ends
			// with, so the day a section is added below deny: this would stop
			// editing the deny list and start asserting nothing.
			manifest := filepath.Join(directory, "agent", "manifest.yaml")
			appendToFile(t, manifest, "\ndeny:\n  - \"a/path/nobody/synced/**\"\n")

			err = agentCheck(project, false)
			if err == nil {
				t.Fatal("a deny list the settings file has not caught up with must be refused")
			}
			if !strings.Contains(err.Error(), "settings.json") || !strings.Contains(err.Error(), "./do agent sync") {
				t.Errorf("error = %q, want it to name the file and the command that fixes it", err)
			}

			if err := agentSync(project); err != nil {
				t.Fatalf("re-sync: %v", err)
			}
			if err := agentCheck(project, false); err != nil {
				t.Fatalf("check after re-syncing the manifest: %v", err)
			}

			// And the half that makes the gate worth having, starting with the
			// half only a copy can fail. A symlink stands for whatever its
			// target holds today; a duplicate stands for what the target held
			// when sync ran, and check compares content to notice. It has to
			// be a skill that drifts: a drifted rule is caught by the byte
			// comparison over the rendered files, which runs before any shim
			// is looked at and is the same code in both trees — so on a rule
			// alone the copied case would assert nothing a Windows checkout
			// does differently.
			skill := filepath.Join(directory, "agent", "skills", "commit", "SKILL.md")
			appendToFile(t, skill, "\n## A step nobody synced\n")

			err = agentCheck(project, false)
			if shims.copied {
				if err == nil {
					t.Fatal("a copied skill shim that has fallen behind agent/ must be refused")
				}
				if !strings.Contains(err.Error(), "skills/commit") || !strings.Contains(err.Error(), "./do agent sync") {
					t.Errorf("error = %q, want it to name the shim and the command that fixes it", err)
				}
			} else if err != nil {
				t.Fatalf("a symlinked skill cannot fall behind its target: %v", err)
			}

			// Then the same mistake on the side of the tree that is rendered
			// rather than linked, which both trees have to refuse.
			rule := filepath.Join(directory, "agent", "rules", "core.md")
			appendToFile(t, rule, "\n## A section nobody synced\n")

			err = agentCheck(project, false)
			if err == nil {
				t.Fatal("an edited rule with stale shims must be refused")
			}
			if !strings.Contains(err.Error(), "./do agent sync") {
				t.Errorf("error = %q, want it to name the command that fixes it", err)
			}
		})
	}
}

// appendToFile edits a file under the copied agent/, which is the state a
// contributor who changed agent/ and forgot to sync leaves a checkout in.
func appendToFile(t *testing.T, path, text string) {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if err := os.WriteFile(path, append(body, text...), 0o644); err != nil {
		t.Fatalf("editing %s: %v", path, err)
	}
}

// copyAsLinkFallback is what sync writes where it may not create a symlink.
// os.Symlink succeeds on every machine these tests run on, so the only way to
// reach it is to call it.
func TestCopyAsLinkFallback(t *testing.T) {
	t.Parallel()

	t.Run("a file", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		source := filepath.Join(directory, "AGENTS.md")
		copied := filepath.Join(directory, "CLAUDE.md")
		if err := os.WriteFile(source, []byte("# yagit\n"), 0o644); err != nil {
			t.Fatalf("writing the source: %v", err)
		}

		if err := copyAsLinkFallback(copied, source); err != nil {
			t.Fatalf("copying: %v", err)
		}

		have, err := os.ReadFile(copied)
		if err != nil {
			t.Fatalf("reading the copy: %v", err)
		}
		if string(have) != "# yagit\n" {
			t.Errorf("copy = %q, want the source verbatim", have)
		}
		if err := sameContent(copied, source); err != nil {
			t.Errorf("the copy it just made: %v", err)
		}
	})

	// Skills are directories, and so are .cursor and .claude: the copy has to
	// walk, not just read.
	t.Run("a directory", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		source := filepath.Join(directory, "agent", "skills", "commit")
		copied := filepath.Join(directory, "claude", "skills", "commit")
		if err := os.MkdirAll(filepath.Join(source, "references"), 0o755); err != nil {
			t.Fatalf("creating the source: %v", err)
		}
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill\n"), 0o644); err != nil {
			t.Fatalf("writing the skill: %v", err)
		}
		if err := os.WriteFile(filepath.Join(source, "references", "types.md"), []byte("types\n"), 0o644); err != nil {
			t.Fatalf("writing the reference: %v", err)
		}

		if err := copyAsLinkFallback(copied, source); err != nil {
			t.Fatalf("copying: %v", err)
		}

		nested, err := os.ReadFile(filepath.Join(copied, "references", "types.md"))
		if err != nil {
			t.Fatalf("reading the nested copy: %v", err)
		}
		if string(nested) != "types\n" {
			t.Errorf("nested copy = %q, want the source verbatim", nested)
		}
		if err := sameContent(copied, source); err != nil {
			t.Errorf("the copy it just made: %v", err)
		}
	})
}

// sameContent is the other half: what `agent check` asks of a shim that is a
// duplicate rather than a link. It has to refuse, or a Windows checkout with
// stale shims passes the gate forever.
func TestSameContentRefusesADriftedCopy(t *testing.T) {
	t.Parallel()

	t.Run("content that moved on", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		source := filepath.Join(directory, "AGENTS.md")
		copied := filepath.Join(directory, "CLAUDE.md")
		if err := os.WriteFile(source, []byte("current\n"), 0o644); err != nil {
			t.Fatalf("writing the source: %v", err)
		}
		if err := os.WriteFile(copied, []byte("stale\n"), 0o644); err != nil {
			t.Fatalf("writing the copy: %v", err)
		}

		if err := sameContent(copied, source); err == nil {
			t.Fatal("a copy that has fallen behind its source must be refused")
		}
	})

	t.Run("a file the copy never got", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		source := filepath.Join(directory, "source")
		copied := filepath.Join(directory, "copy")
		if err := os.MkdirAll(source, 0o755); err != nil {
			t.Fatalf("creating the source: %v", err)
		}
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("skill\n"), 0o644); err != nil {
			t.Fatalf("writing the skill: %v", err)
		}
		if err := copyAsLinkFallback(copied, source); err != nil {
			t.Fatalf("copying: %v", err)
		}
		if err := os.WriteFile(filepath.Join(source, "ADDED.md"), []byte("added\n"), 0o644); err != nil {
			t.Fatalf("adding a file to the source: %v", err)
		}

		if err := sameContent(copied, source); err == nil {
			t.Fatal("a copy missing a file the source has must be refused")
		}
	})
}
