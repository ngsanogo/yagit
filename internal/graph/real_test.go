package graph_test

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/graph"
)

// TestARealHistory runs the invariants against a repository on this machine.
//
// The generated topologies beside this file are stronger in one way — there
// are three hundred of them, and eleven million more behind the fuzz target —
// and weaker in another: they contain what their generator was written to
// produce. A real history contains what twenty years of people did, which is
// the only source of a shape nobody thought to generate.
//
// Skipped unless YAGIT_REAL_REPOSITORY names one, because it needs a clone
// that this repository has no business carrying:
//
//	git clone --filter=blob:none https://github.com/git/git.git /tmp/git
//	YAGIT_REAL_REPOSITORY=/tmp/git go test ./internal/graph/ -run TestARealHistory -v
func TestARealHistory(t *testing.T) {
	path := os.Getenv("YAGIT_REAL_REPOSITORY")
	if path == "" {
		t.Skip("set YAGIT_REAL_REPOSITORY to a repository to check the invariants against it")
	}

	// Whatever configuration the machine has must not decide what git prints:
	// log.decorate=full alone changes the shape of every ref badge.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	// Reading a history of this size is not a unit test's second.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	commits, err := git.NewRunner(nil).Log(ctx, path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if len(commits) == 0 {
		t.Fatalf("%s has no history to check", path)
	}

	started := time.Now()
	assigned := graph.Assign(commits)
	t.Logf("%d commits from %s: %d columns, %d lines, assigned in %v",
		len(commits), path, assigned.Width, len(assigned.Edges),
		time.Since(started).Round(time.Millisecond))

	checkInvariants(t, commits, assigned)
}

// BenchmarkCrossingAPage measures what one page of a real history costs to
// answer, by the index and by the scan it replaced.
//
// The last page, because that is the worst one. Edges is ordered by From, so a
// window near the top of the history is cheap however it is answered — every
// edge below it fails one comparison. At the bottom the scan has nothing left
// to skip and reads all of it, and the bottom is where scrolling ends up.
//
// Skipped unless YAGIT_REAL_REPOSITORY names a repository, for the reason the
// test above is:
//
//	YAGIT_REAL_REPOSITORY=/tmp/git go test ./internal/graph/ -run '^$' -bench Crossing
func BenchmarkCrossingAPage(b *testing.B) {
	path := os.Getenv("YAGIT_REAL_REPOSITORY")
	if path == "" {
		b.Skip("set YAGIT_REAL_REPOSITORY to a repository to measure Crossing against it")
	}

	b.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	b.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	commits, err := git.NewRunner(nil).Log(ctx, path)
	if err != nil {
		b.Fatalf("reading %s: %v", path, err)
	}
	if len(commits) == 0 {
		b.Fatalf("%s has no history to measure", path)
	}

	assigned := graph.Assign(commits)

	// The daemon pages the history 200 rows at a time. This package is not
	// allowed to know that — the number is in internal/api and stays there —
	// and the answer would have the same shape at any page size.
	const page = 200
	first, last := max(len(commits)-page, 0), len(commits)

	// A benchmark of a wrong answer measures nothing.
	if !slices.Equal(assigned.Crossing(first, last), scanCrossing(assigned, first, last)) {
		b.Fatalf("the index and the scan disagree about rows [%d, %d)", first, last)
	}
	b.Logf("%d commits from %s, %d lines in the picture", len(commits), path, len(assigned.Edges))

	b.Run("index", func(b *testing.B) {
		benchmarkCrossing(b, assigned.Crossing, first, last)
	})
	b.Run("scan", func(b *testing.B) {
		benchmarkCrossing(b, func(first, last int) []graph.Edge {
			return scanCrossing(assigned, first, last)
		}, first, last)
	})
}

// benchmarkCrossing times one way of answering a window, and reports how many
// edges the answer held.
//
// The count is summed rather than dropped on the floor: b.Loop keeps the call
// itself alive, but nothing keeps a result that is never looked at, and a
// benchmark of a call whose answer was never built is a benchmark of nothing.
func benchmarkCrossing(b *testing.B, crossing func(first, last int) []graph.Edge, first, last int) {
	b.Helper()

	found := 0
	for b.Loop() {
		found += len(crossing(first, last))
	}
	b.ReportMetric(float64(found)/float64(b.N), "edges/page")
}
