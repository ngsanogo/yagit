package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sync produces something check accepts, and check notices when a copy stops
// matching.
//
// A symlink where the process may create one, and a duplicate where it may
// not — Windows without the privilege, which is the half `./do agent sync`
// used to run on no machine that tested it. The copy is asked for rather than
// waited for: os.Symlink succeeds everywhere these tests run.
//
// Against a copy of agent/, never the checkout itself. Running sync from a
// test rewrote the links of whoever ran `./do test go` — which is exactly the
// gate `./do agent check` exists to be: a contributor who edited agent/ and
// forgot to sync would have had the tests quietly do it for them, and the
// gate would only ever pass.
func TestAgentSyncAndCheck(t *testing.T) {
	source := filepath.Join("..", "..", "agent")
	if _, err := os.Stat(filepath.Join(source, "AGENTS.md")); err != nil {
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

			// A symlink stands for whatever its target holds today; a duplicate
			// stands for what the target held when sync ran. The drift has to
			// be a skill: .agents/skills is the directory copy, and a file copy
			// of AGENTS.md would not exercise the walk.
			skill := filepath.Join(directory, "agent", "skills", "commit", "SKILL.md")
			appendToFile(t, skill, "\n## A step nobody synced\n")

			err = agentCheck(project, false)
			if shims.copied {
				if err == nil {
					t.Fatal("a copied skills directory that has fallen behind agent/ must be refused")
				}
				if !strings.Contains(err.Error(), ".agents/skills") || !strings.Contains(err.Error(), "./do agent sync") {
					t.Errorf("error = %q, want it to name the link and the command that fixes it", err)
				}
			} else if err != nil {
				t.Fatalf("a symlinked skills directory cannot fall behind its target: %v", err)
			}

			if err := agentSync(project); err != nil {
				t.Fatalf("re-sync: %v", err)
			}
			if err := agentCheck(project, false); err != nil {
				t.Fatalf("check after re-syncing: %v", err)
			}

			instructions := filepath.Join(directory, "agent", "AGENTS.md")
			appendToFile(t, instructions, "\n## A section nobody synced\n")

			err = agentCheck(project, false)
			if shims.copied {
				if err == nil {
					t.Fatal("a copied AGENTS.md that has fallen behind agent/ must be refused")
				}
				if !strings.Contains(err.Error(), "AGENTS.md") || !strings.Contains(err.Error(), "./do agent sync") {
					t.Errorf("error = %q, want it to name the link and the command that fixes it", err)
				}
			} else if err != nil {
				t.Fatalf("a symlinked AGENTS.md cannot fall behind its target: %v", err)
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
		copied := filepath.Join(directory, "copy.md")
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

	// Skills are a directory: the copy has to walk, not just read.
	t.Run("a directory", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		source := filepath.Join(directory, "agent", "skills", "commit")
		copied := filepath.Join(directory, "skills-copy", "commit")
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

// sameContent is the other half: what `agent check` asks of a link that is a
// duplicate rather than a symlink. It has to refuse, or a Windows checkout
// with a stale copy passes the gate forever.
func TestSameContentRefusesADriftedCopy(t *testing.T) {
	t.Parallel()

	t.Run("content that moved on", func(t *testing.T) {
		t.Parallel()

		directory := t.TempDir()
		source := filepath.Join(directory, "AGENTS.md")
		copied := filepath.Join(directory, "copy.md")
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
