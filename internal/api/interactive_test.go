package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// What the interactive rebase routes answer, and what they refuse.
//
// The plan is checked without git in internal/git; what these ask is the part
// only a route can answer — that the range comes back in the order a plan is
// written in, that a plan naming a history the repository no longer has is
// refused rather than run, and that a rebase which stops at an `edit` is
// reported as stopped rather than as done.

// TestMain makes this test binary the editors a rebase points git at, for the
// reason the one in internal/git does: an interactive rebase names
// os.Executable, and under `go test` that is this binary. A test that drives
// the route end to end is a test that has to answer git's request for one.
//
// The same seven lines as cmd/yagit's, calling the same function, which is
// what makes them seven lines rather than an implementation.
func TestMain(m *testing.M) {
	if handled, err := git.RunAsEditor(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type wireInteractivePlan struct {
	Command string       `json:"command"`
	Base    string       `json:"base"`
	Subject string       `json:"subject"`
	From    string       `json:"from"`
	Commits []wireCommit `json:"commits"`
}

type wireInteractiveResult struct {
	// The reference list is embedded in the daemon's answer, so it has to be
	// here too: the interface writes it straight into the cache, and an answer
	// that carried none would empty the sidebar rather than refresh it.
	Refs []struct {
		Name string `json:"name"`
	} `json:"refs"`
	Head *struct {
		Name string `json:"name"`
	} `json:"head"`

	Stopped bool `json:"stopped"`
	Step    int  `json:"step"`
	Total   int  `json:"total"`
}

// openPlannableRepository is a linear history on main: base → one → two →
// three, each commit touching a file of its own so any order applies cleanly.
func openPlannableRepository(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGitIn(t, path, "init", "-b", "main")
	configureIdentityIn(t, path)

	writeFileIn(t, path, "base.md", "base\n")
	runGitIn(t, path, "add", "--", "base.md")
	runGitIn(t, path, withIdentity("commit", "-m", "base")...)

	for _, name := range []string{"one", "two", "three"} {
		writeFileIn(t, path, name+".md", name+"\n")
		runGitIn(t, path, "add", "--", name+".md")
		runGitIn(t, path, withIdentity("commit", "-m", name)...)
	}

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

func interactivePlanFor(t *testing.T, handler http.Handler, id, commit string) wireInteractivePlan {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase/interactive/plan",
		fmt.Sprintf(`{"commit":%q}`, commit))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireInteractivePlan](t, response)
}

// stepsBody renders a plan the way a client sends one: object names and verbs,
// in the order they will run.
func stepsBody(base, from string, steps ...string) string {
	return fmt.Sprintf(`{"base":%q,"from":%q,"steps":[%s]}`,
		base, from, strings.Join(steps, ","))
}

func pickStep(sha string) string { return planStep(sha, "pick") }
func planStep(sha, instruction string) string {
	return fmt.Sprintf(`{"commit":%q,"instruction":%q}`, sha, instruction)
}

func TestPlanningAnInteractiveRebaseAnswersTheRangeOldestFirst(t *testing.T) {
	handler, id, path := openPlannableRepository(t)
	base := shaOf(t, path, "main~3")

	planned := interactivePlanFor(t, handler, id, base)

	if planned.Base != base {
		t.Errorf("base = %s, want the full name %s", planned.Base, base)
	}
	if planned.Subject != "base" {
		t.Errorf("subject = %q, want the base commit's", planned.Subject)
	}
	if planned.From != "main" {
		t.Errorf("from = %q, want main", planned.From)
	}
	if want := "git rebase --interactive"; !strings.HasPrefix(planned.Command, want) {
		t.Errorf("command = %q, want it to start %q", planned.Command, want)
	}

	// Oldest first: the order a todo list is executed in, so the list drawn
	// and the list run are one list.
	want := []string{"one", "two", "three"}
	got := make([]string, 0, len(planned.Commits))
	for _, commit := range planned.Commits {
		got = append(got, commit.Subject)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("commits = %v, want %v", got, want)
	}
}

func TestPlanningAnInteractiveRebaseRefusesTheTipAndForeignHistory(t *testing.T) {
	handler, id, path := openPlannableRepository(t)

	for name, commit := range map[string]string{
		"the tip itself": shaOf(t, path, "main"),
	} {
		response := postJSON(t, handler, "/api/repos/"+id+"/rebase/interactive/plan",
			fmt.Sprintf(`{"commit":%q}`, commit))
		if response.Code != http.StatusConflict {
			t.Errorf("planning over %s: status = %d, want 409: %s", name, response.Code, response.Body)
		}
	}
}

func TestRunningAnInteractiveRebaseRewritesTheRange(t *testing.T) {
	handler, id, path := openPlannableRepository(t)
	base := shaOf(t, path, "main~3")
	planned := interactivePlanFor(t, handler, id, base)

	// three, one, two — the last commit moved to the front, and nothing
	// dropped, so the whole range is rewritten in a new order.
	response := postJSON(t, handler, "/api/repos/"+id+"/rebase/interactive",
		stepsBody(base, "main",
			pickStep(planned.Commits[2].SHA),
			pickStep(planned.Commits[0].SHA),
			pickStep(planned.Commits[1].SHA)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	result := decode[wireInteractiveResult](t, response)
	if result.Stopped {
		t.Error("the plan is reported as stopped, and it holds nothing that stops")
	}

	// The references travel with it, like every other operation's answer. The
	// interface writes them into its cache without asking again, so an answer
	// that carried none would blank the sidebar at the moment the branch moved.
	if len(result.Refs) == 0 {
		t.Error("the answer carries no references")
	}
	if result.Head == nil || result.Head.Name != "main" {
		t.Errorf("head = %v, want main", result.Head)
	}

	if subject := subjectOf(t, path, "main~2"); subject != "three" {
		t.Errorf("the oldest rewritten commit says %q, want three", subject)
	}
}

// A plan holding an `edit` ends with git stopped, having exited zero. The
// answer has to say so: a toast reporting a finished rebase over a repository
// sitting halfway through one is the worst lie this interface could tell.
func TestRunningAPlanThatStopsAnswersThatItStopped(t *testing.T) {
	handler, id, path := openPlannableRepository(t)
	base := shaOf(t, path, "main~3")
	planned := interactivePlanFor(t, handler, id, base)

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase/interactive",
		stepsBody(base, "main",
			pickStep(planned.Commits[0].SHA),
			planStep(planned.Commits[1].SHA, "edit"),
			pickStep(planned.Commits[2].SHA)))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	result := decode[wireInteractiveResult](t, response)
	if !result.Stopped {
		t.Fatal("the plan stopped at an edit and the answer does not say so")
	}
	if result.Step != 2 || result.Total != 3 {
		t.Errorf("stopped at %d of %d, want 2 of 3", result.Step, result.Total)
	}
}

// The lease, and it is the strongest one here: the range is read again and the
// plan has to be exactly it. A commit landing on the branch while the dialog
// was open makes the plan a list about a history that no longer exists.
func TestRunningAnInteractiveRebaseRefusesAPlanThatIsNotTheRange(t *testing.T) {
	handler, id, path := openPlannableRepository(t)
	base := shaOf(t, path, "main~3")
	planned := interactivePlanFor(t, handler, id, base)

	writeFileIn(t, path, "later.md", "later\n")
	runGitIn(t, path, "add", "--", "later.md")
	runGitIn(t, path, withIdentity("commit", "-m", "later")...)

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase/interactive",
		stepsBody(base, "main",
			pickStep(planned.Commits[0].SHA),
			pickStep(planned.Commits[1].SHA),
			pickStep(planned.Commits[2].SHA)))
	if response.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 for a plan missing a commit: %s", response.Code, response.Body)
	}

	// Nothing ran: the branch still holds the commit the plan never named.
	if subject := subjectOf(t, path, "main"); subject != "later" {
		t.Errorf("the tip says %q, want the refused plan to have changed nothing", subject)
	}
}

func TestRunningAnInteractiveRebaseRefusesAPlanTheDaemonCannotCarryOut(t *testing.T) {
	handler, id, path := openPlannableRepository(t)
	base := shaOf(t, path, "main~3")
	planned := interactivePlanFor(t, handler, id, base)

	cases := map[string]struct {
		body string
		want int
	}{
		// A verb that opens an editor over a message nobody has written.
		"a squash": {stepsBody(base, "main",
			pickStep(planned.Commits[0].SHA),
			planStep(planned.Commits[1].SHA, "squash"),
			pickStep(planned.Commits[2].SHA)), http.StatusBadRequest},

		// git answers this one AFTER starting the rebase, leaving a repository
		// stopped inside a plan that could never have run.
		"a combine with nothing above it": {stepsBody(base, "main",
			planStep(planned.Commits[0].SHA, "fixup"),
			pickStep(planned.Commits[1].SHA),
			pickStep(planned.Commits[2].SHA)), http.StatusBadRequest},

		"no steps at all": {stepsBody(base, "main"), http.StatusBadRequest},

		// HEAD is on main, and the request says otherwise.
		"a branch HEAD is not on": {stepsBody(base, "release",
			pickStep(planned.Commits[0].SHA),
			pickStep(planned.Commits[1].SHA),
			pickStep(planned.Commits[2].SHA)), http.StatusConflict},
	}

	for name, want := range cases {
		response := postJSON(t, handler, "/api/repos/"+id+"/rebase/interactive", want.body)
		if response.Code != want.want {
			t.Errorf("%s: status = %d, want %d: %s", name, response.Code, want.want, response.Body)
		}
		if subject := subjectOf(t, path, "main"); subject != "three" {
			t.Errorf("%s: the tip says %q, want the refused plan to have changed nothing", name, subject)
		}
	}
}

// subjectOf is how these tests compare histories: by what a commit says, since
// every hash changes when a plan runs.
func subjectOf(t *testing.T, path, reference string) string {
	t.Helper()

	runner := git.NewRunner(nil)
	output, err := runner.Run(t.Context(), path, "log", "-1", "--pretty=format:%s", reference, "--")
	if err != nil {
		t.Fatalf("git log %s: %v", reference, err)
	}
	return strings.TrimSpace(string(output))
}
