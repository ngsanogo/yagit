package git_test

import (
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// CheckRefName guards the one walk in this package whose revisions come off a
// query string. `git log` takes its revisions BEFORE any `--`, so there is no
// separator to hide them behind: a name that is an option is an option.
func TestCheckRefNameAcceptsWhatForEachRefLists(t *testing.T) {
	for _, name := range []string{
		"refs/heads/main",
		"refs/heads/feature/a-topic",
		"refs/remotes/origin/main",
		"refs/tags/v1.0.0",
		// A comma is legal in a ref name, which is the whole reason the wire
		// format repeats `ref=` instead of joining them with one.
		"refs/heads/a,b",
		"HEAD",
	} {
		if err := git.CheckRefName(name); err != nil {
			t.Errorf("CheckRefName(%q) = %v, want nil", name, err)
		}
	}
}

func TestCheckRefNameRefusesWhatGitWouldReadAsSomethingElse(t *testing.T) {
	cases := map[string]string{
		"nothing at all":       "",
		"an option":            "--output=/tmp/written",
		"a short option":       "-n",
		"a range":              "main..topic",
		"a symmetric range":    "main...topic",
		"a reflog entry":       "main@{yesterday}",
		"a parent":             "main^",
		"an ancestor":          "main~3",
		"a blob":               "main:path/to/file",
		"a glob":               "refs/heads/*",
		"a newline":            "refs/heads/main\nrefs/heads/other",
		"a NUL":                "refs/heads/main\x00",
		"a space":              "refs/heads/two words",
		"a backslash":          "refs\\heads\\main",
		"a question mark":      "refs/heads/main?",
		"a bracket":            "refs/heads/[main]",
		"a control character":  "refs/heads/\x01main",
		"the delete character": "refs/heads/main\x7f",
	}

	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			err := git.CheckRefName(candidate)
			if err == nil {
				t.Fatalf("CheckRefName(%q) = nil, want a refusal", candidate)
			}
			// Wrapped, because the route above turns exactly this into a 400
			// rather than reporting the daemon broke.
			if !errors.Is(err, git.ErrBadRefName) {
				t.Errorf("CheckRefName(%q) = %v, which is not an ErrBadRefName", candidate, err)
			}
		})
	}
}
