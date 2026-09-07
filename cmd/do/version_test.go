package main

import "testing"

// The version is not a number anyone picks: it is a consequence of what the
// commits said about themselves. That makes these rules the contract between a
// commit message and a promise to whoever installs the result, so each one is
// pinned rather than trusted.

func TestClassify(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    bump
	}{
		// The four types that change something a user can observe.
		{"a feature", "feat: virtualised commit list", bumpMinor},
		{"a fix", "fix: the tablist owned a button that was not a tab", bumpPatch},
		{"a performance change", "perf: parse the log in one pass", bumpPatch},
		{"a revert", "revert: the tablist change", bumpPatch},

		// A scope changes nothing about the decision.
		{"a scoped feature", "feat(api): open a repository by path", bumpMinor},
		{"a scoped fix", "fix(repo): an inode number is only unique among live objects", bumpPatch},

		// Breaking, both ways it may be said.
		{"a bang", "feat!: the token moves to a header", bumpMajor},
		{"a scoped bang", "feat(api)!: the token moves to a header", bumpMajor},
		{"a bang on a fix", "fix!: reject a root that is a file", bumpMajor},
		{
			name:    "a footer",
			message: "feat: rename the root flag\n\nBREAKING CHANGE: -root is now -repositories.",
			want:    bumpMajor,
		},
		{
			// Conventional Commits allows the hyphenated spelling in a footer,
			// because a footer token may not contain a space.
			name:    "a hyphenated footer",
			message: "feat: rename the root flag\n\nBREAKING-CHANGE: -root is now -repositories.",
			want:    bumpMajor,
		},

		// Everything that changes the repository and not the product. A
		// release for one of these is a release nobody can tell apart from
		// the one before it.
		{"documentation", "docs: name an owner", bumpNone},
		{"tests", "test(api): the routes that serve repositories", bumpNone},
		{"CI", "ci: never cancel a run on main", bumpNone},
		{"build tooling", "build: lock the toolchain by checksum", bumpNone},
		{"a chore", "chore: tidy the scratch files", bumpNone},
		{"a refactor", "refactor: extract the frontend handler", bumpNone},
		{"style", "style: reflow the comments", bumpNone},

		// Not a conventional commit. Treating it as a fix would ship a release
		// nobody described.
		{"a merge commit", "Merge pull request #1 from example/topic", bumpNone},
		{"a bare sentence", "make the thing work again", bumpNone},
		{"empty", "", bumpNone},

		// A colon in prose is not a type separator, and a word before it is
		// not a type.
		{"prose with a colon", "reverted the change: it was wrong", bumpNone},

		// The word appearing in the body is not the footer. A commit that
		// merely discusses a breaking change does not make one.
		{
			name:    "the phrase mentioned in prose",
			message: "docs: explain what BREAKING CHANGE means in CONTRIBUTING",
			want:    bumpNone,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := classify(testCase.message); got != testCase.want {
				t.Errorf("classify(%q) = %v, want %v", testCase.message, got, testCase.want)
			}
		})
	}
}

func TestNextVersion(t *testing.T) {
	cases := []struct {
		name      string
		current   version
		messages  []string
		want      string
		warranted bool
	}{
		{
			name:      "nothing to release",
			current:   version{0, 3, 1},
			messages:  []string{"docs: a word", "ci: a job", "test: a case"},
			warranted: false,
		},
		{
			name:      "no commits at all",
			current:   version{0, 3, 1},
			messages:  nil,
			warranted: false,
		},
		{
			name:      "a fix moves the patch",
			current:   version{1, 2, 3},
			messages:  []string{"fix: something"},
			want:      "v1.2.4",
			warranted: true,
		},
		{
			name:      "a feature moves the minor and resets the patch",
			current:   version{1, 2, 3},
			messages:  []string{"fix: something", "feat: something else"},
			want:      "v1.3.0",
			warranted: true,
		},
		{
			name:      "the largest bump in the set wins",
			current:   version{1, 2, 3},
			messages:  []string{"fix: a", "feat: b", "docs: c"},
			want:      "v1.3.0",
			warranted: true,
		},
		{
			name:      "a breaking change after 1.0 moves the major",
			current:   version{1, 2, 3},
			messages:  []string{"feat!: something"},
			want:      "v2.0.0",
			warranted: true,
		},
		{
			// Semver's own rule: while the major is zero, anything may change
			// at any time. Bumping the major here would reach 4.0.0 before
			// there is a product, which tells a reader only that the project
			// changed its mind four times.
			name:      "a breaking change before 1.0 moves the minor",
			current:   version{0, 3, 1},
			messages:  []string{"feat!: something"},
			want:      "v0.4.0",
			warranted: true,
		},
		{
			name:      "the first release of all",
			current:   version{0, 0, 0},
			messages:  []string{"feat: the first thing"},
			want:      "v0.1.0",
			warranted: true,
		},
		{
			// Only fixes, and nothing released yet: 0.0.1 is honest. It says
			// the thing exists and promises nothing.
			name:      "the first release is a fix",
			current:   version{0, 0, 0},
			messages:  []string{"fix: the first thing"},
			want:      "v0.0.1",
			warranted: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, warranted := nextVersion(testCase.current, testCase.messages)

			if warranted != testCase.warranted {
				t.Fatalf("warranted = %v, want %v", warranted, testCase.warranted)
			}
			if warranted && got.String() != testCase.want {
				t.Errorf("nextVersion = %s, want %s", got, testCase.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	valid := map[string]version{
		"v0.0.0":    {0, 0, 0},
		"v1.2.3":    {1, 2, 3},
		"v10.20.30": {10, 20, 30},
		"0.1.0":     {0, 1, 0}, // the prefix is optional on the way in
	}

	// Surrounding whitespace is deliberately NOT accepted here. Both layers
	// above already remove it — capture trims the command's output, and
	// lastRelease splits on fields — so tolerating it a third time would be
	// defending against a case that cannot arrive, and the failure if one ever
	// did is a loud parse error rather than a wrong version.
	for tag, want := range valid {
		got, err := parseVersion(tag)
		if err != nil {
			t.Errorf("parseVersion(%q): %v", tag, err)
			continue
		}
		if got != want {
			t.Errorf("parseVersion(%q) = %+v, want %+v", tag, got, want)
		}
	}

	// A tag that is not a version must be refused rather than guessed at: a
	// release named after a misread tag is worse than no release.
	for _, tag := range []string{"v1.2", "v1.2.3.4", "vX.Y.Z", "release-1", "v1.2.x", "", "v-1.0.0"} {
		if got, err := parseVersion(tag); err == nil {
			t.Errorf("parseVersion(%q) = %+v, want an error", tag, got)
		}
	}
}

// TestVersionRoundTrips guards the one place the two directions have to agree:
// the tag this command prints is the tag it will read back next time.
func TestVersionRoundTrips(t *testing.T) {
	for _, original := range []version{{0, 0, 0}, {0, 1, 0}, {1, 2, 3}, {10, 0, 99}} {
		parsed, err := parseVersion(original.String())
		if err != nil {
			t.Errorf("parseVersion(%s): %v", original, err)
			continue
		}
		if parsed != original {
			t.Errorf("%s parsed back as %+v", original, parsed)
		}
	}
}
