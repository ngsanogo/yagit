package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mise.toml pins mise itself, and CI has to repeat the number. These tests pin
// the check that holds the repeat to the original: each refusal below is a way
// the mise CI installs could part from the one mise.toml asks for while every
// other gate stays green.

func TestReadMinVersion(t *testing.T) {
	valid := map[string]string{
		"above the first table": "min_version = \"2026.10.2\"\n\n[settings]\nlockfile = true\n",
		"a literal string with a comment": "# mise itself.\nmin_version = '2026.10.2' # the floor\n" +
			"[tools]\ngo = \"1.27.1\"\n",
	}
	for name, contents := range valid {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mise.toml")
			writeFile(t, path, contents)

			got, err := readMinVersion(path)
			if err != nil {
				t.Fatalf("readMinVersion: %v", err)
			}
			if got != "2026.10.2" {
				t.Errorf("readMinVersion = %q, want 2026.10.2", got)
			}
		})
	}

	refused := map[string]string{
		// Valid TOML, but a key of [settings] rather than mise's floor. This
		// is the mistake the placement rule exists for.
		"under a table":      "[settings]\nmin_version = \"2026.10.2\"\n",
		"absent":             "[tools]\ngo = \"1.27.1\"\n",
		"commented out":      "# min_version = \"2026.10.2\"\n[tools]\n",
		"a floating major":   "min_version = \"2026\"\n",
		"a floating minor":   "min_version = \"2026.10\"\n",
		"a range":            "min_version = \">=2026.10.2\"\n",
		"latest":             "min_version = \"latest\"\n",
		"a prefixed version": "min_version = \"v2026.10.2\"\n",

		// mise accepts this form. The check does not: a step installs one
		// mise, so there has to be one number to hold it to.
		"a table of two floors": "min_version = { hard = \"2026.10.2\", soft = \"2026.10.2\" }\n",
	}
	for name, contents := range refused {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mise.toml")
			writeFile(t, path, contents)

			if got, err := readMinVersion(path); err == nil {
				t.Errorf("readMinVersion = %q, want an error", got)
			}
		})
	}
}

func TestMiseActionSteps(t *testing.T) {
	cases := []struct {
		name     string
		workflow string
		want     []miseActionStep
	}{
		{
			name: "the version under with",
			workflow: `steps:
  - uses: jdx/mise-action@abc # v5
    with:
      version: 2026.10.2
      install: false
`,
			want: []miseActionStep{{line: 2, version: "2026.10.2", read: true}},
		},
		{
			name: "quoted, after a comment and a blank line",
			workflow: `steps:
  - name: Install mise
    uses: "jdx/mise-action@abc"
    with:
      # Why the cache is off.

      cache: false
      version: '2026.10.2' # pinned
`,
			want: []miseActionStep{{line: 3, version: "2026.10.2", read: true}},
		},
		{
			// uses: is not always a step's first key, so the step is read in
			// both directions from it.
			name: "with before uses",
			workflow: `steps:
  - with:
      version: 2026.10.2
    uses: jdx/mise-action@abc
`,
			want: []miseActionStep{{line: 4, version: "2026.10.2", read: true}},
		},
		{
			// The next step's version must not be credited to this one.
			name: "no with, before a step that has one",
			workflow: `steps:
  - uses: jdx/mise-action@abc
  - uses: actions/cache@abc
    with:
      version: 2026.10.2
`,
			want: []miseActionStep{{line: 2, version: "", read: true}},
		},
		{
			name: "a version under env",
			workflow: `steps:
  - uses: jdx/mise-action@abc
    with:
      install: false
    env:
      version: 2026.10.2
`,
			want: []miseActionStep{{line: 2, version: "", read: true}},
		},
		{
			name: "a version inside a block scalar",
			workflow: `steps:
  - uses: jdx/mise-action@abc
    with:
      install_args: >-
        version: 2026.10.2
`,
			want: []miseActionStep{{line: 2, version: "", read: true}},
		},
		{
			name: "every step, across jobs",
			workflow: `jobs:
  lint:
    steps:
      - uses: jdx/mise-action@abc
        with:
          version: 2026.10.2
  release:
    steps:
      - uses: jdx/mise-action@abc
`,
			want: []miseActionStep{
				{line: 4, version: "2026.10.2", read: true},
				{line: 9, version: "", read: true},
			},
		},
		{
			name: "another action's version",
			workflow: `steps:
  - uses: actions/setup-node@abc
    with:
      version: 2026.10.2
`,
		},
		{
			name: "the action named in comments",
			workflow: `steps:
  # jdx/mise-action installs mise before mise.toml is read.
  - run: ./do lint # see uses: jdx/mise-action above
`,
		},
		{
			// Valid YAML this reader does not parse. Skipping it would let a
			// step pass by being written differently.
			name: "a flow-style step",
			workflow: `steps:
  - {uses: jdx/mise-action@abc, with: {version: 2026.10.2}}
`,
			want: []miseActionStep{{line: 2, version: "", read: false}},
		},
		{
			name: "a flow-style with",
			workflow: `steps:
  - uses: jdx/mise-action@abc
    with: {version: 2026.10.2}
`,
			want: []miseActionStep{{line: 2, version: "", read: false}},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := miseActionSteps(testCase.workflow); !slices.Equal(got, testCase.want) {
				t.Errorf("miseActionSteps = %+v, want %+v", got, testCase.want)
			}
		})
	}
}

// miseProject is a checkout whose mise.toml asks for mise 2026.10.2, holding
// the files given under their repository-relative paths.
func miseProject(t *testing.T, files map[string]string) *project {
	t.Helper()

	p := newProject(t)
	writeFile(t, p.path("mise.toml"), "min_version = \"2026.10.2\"\n\n[tools]\ngo = \"1.27.1\"\n")
	for name, contents := range files {
		writeFile(t, p.path(filepath.FromSlash(name)), contents)
	}
	return p
}

// miseStep is a workflow with one mise step, at the given version or, when
// that is empty, at none.
func miseStep(version string) string {
	workflow := "jobs:\n  lint:\n    steps:\n      - uses: jdx/mise-action@abc # v5\n        with:\n"
	if version != "" {
		workflow += "          version: " + version + "\n"
	}
	return workflow + "          install: false\n"
}

func TestCheckMiseVersionAcceptsStepsThatAgree(t *testing.T) {
	p := miseProject(t, map[string]string{
		".github/workflows/ci.yml":       miseStep("2026.10.2"),
		".github/workflows/release.yaml": miseStep("2026.10.2"),
		".github/dependabot.yml":         "version: 2\nupdates: []\n",
	})

	if err := p.checkMiseVersion(); err != nil {
		t.Errorf("checkMiseVersion: %v", err)
	}
}

func TestCheckMiseVersionRefusesAStepWithoutAVersion(t *testing.T) {
	p := miseProject(t, map[string]string{
		".github/workflows/ci.yml":      miseStep("2026.10.2"),
		".github/workflows/release.yml": miseStep(""),
	})

	err := p.checkMiseVersion()
	if err == nil {
		t.Fatal("a step that leaves the mise version to the action must be refused")
	}
	message := err.Error()
	for _, want := range []string{".github/workflows/release.yml:4", "no version", "version: 2026.10.2"} {
		if !strings.Contains(message, want) {
			t.Errorf("error = %q, want it to contain %q", message, want)
		}
	}
	if strings.Contains(message, "ci.yml") {
		t.Errorf("error = %q names a workflow that agrees", message)
	}
}

func TestCheckMiseVersionRefusesAnotherVersion(t *testing.T) {
	p := miseProject(t, map[string]string{
		".github/workflows/ci.yml": miseStep("2026.9.0"),
	})

	err := p.checkMiseVersion()
	if err == nil {
		t.Fatal("a step on a version mise.toml does not name must be refused")
	}
	if !strings.Contains(err.Error(), "2026.9.0") || !strings.Contains(err.Error(), "2026.10.2") {
		t.Errorf("error = %q, want both versions named", err)
	}
}

// One run names every step that is wrong. Stopping at the first would turn
// raising the version into one red lint per forgotten workflow.
func TestCheckMiseVersionNamesEveryStepThatDisagrees(t *testing.T) {
	p := miseProject(t, map[string]string{
		".github/workflows/ci.yml":             miseStep(""),
		".github/workflows/supply-chain.yml":   miseStep("2026.9.0"),
		".github/actions/toolchain/action.yml": miseStep(""),
	})

	err := p.checkMiseVersion()
	if err == nil {
		t.Fatal("three disagreeing steps must be refused")
	}
	for _, want := range []string{"ci.yml", "supply-chain.yml", ".github/actions/toolchain/action.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to name %s", err, want)
		}
	}
}

// Nothing to check is not agreement: the workflows moved, or the action was
// renamed, and the check would otherwise pass on an empty set forever.
func TestCheckMiseVersionRefusesToFindNoStep(t *testing.T) {
	p := miseProject(t, map[string]string{
		".github/workflows/ci.yml": "jobs:\n  lint:\n    steps:\n      - run: ./do lint\n",
	})

	if err := p.checkMiseVersion(); err == nil {
		t.Error("workflows with no mise step must be refused")
	}
}
