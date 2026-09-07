package api

import (
	"testing"

	"github.com/ngsanogo/yagit/internal/repo"
)

func TestARepositoryIsWatchedUnlessItIsRecordedOtherwise(t *testing.T) {
	opened := &repo.Repo{ID: "abc123", Path: "/tmp/x", Name: "x"}

	// The default has to be true rather than false: a zero value that read
	// "not watched" would put a warning on every repository, and a warning
	// that is always there is one nobody reads.
	if view := repositoryViewOf(opened, newUnwatched()); !view.Watched {
		t.Error("a repository with no recorded failure is reported as unwatched")
	}
}

func TestAWatchFailureReachesTheView(t *testing.T) {
	opened := &repo.Repo{ID: "abc123", Path: "/tmp/x", Name: "x"}

	failures := newUnwatched()
	failures.record(opened.ID, "more than 512 ref directories under refs/")

	view := repositoryViewOf(opened, failures)
	if view.Watched {
		t.Fatal("a repository that failed to be watched is reported as watched")
	}
	// The sentence, not a summary of it: "more than 512 ref directories" is
	// something a person can act on and "could not watch" is not.
	if view.WatchFailure != "more than 512 ref directories under refs/" {
		t.Errorf("WatchFailure = %q, want the reason verbatim", view.WatchFailure)
	}
}

func TestForgettingAFailureRestoresTheView(t *testing.T) {
	// Opening a repository retries the watch, so a warning must not outlive
	// the thing it was about.
	opened := &repo.Repo{ID: "abc123", Path: "/tmp/x", Name: "x"}

	failures := newUnwatched()
	failures.record(opened.ID, "no watches left")
	failures.forget(opened.ID)

	view := repositoryViewOf(opened, failures)
	if !view.Watched || view.WatchFailure != "" {
		t.Errorf("a forgotten failure still shows: %+v", view)
	}
}

func TestOneRepositorysFailureDoesNotReachAnother(t *testing.T) {
	failures := newUnwatched()
	failures.record("broken", "no watches left")

	fine := repositoryViewOf(&repo.Repo{ID: "fine", Name: "fine"}, failures)
	if !fine.Watched {
		t.Error("a failure recorded for one repository was reported on another")
	}
}

func TestAViewSurvivesWithoutAnyRecord(t *testing.T) {
	// A daemon started with no watcher at all hands nil here, and the answer
	// has to be a view rather than a panic.
	view := repositoryViewOf(&repo.Repo{ID: "abc123", Name: "x"}, nil)
	if !view.Watched {
		t.Error("with no record to consult, a repository is reported as unwatched")
	}
}
