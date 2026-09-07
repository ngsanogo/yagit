package graph_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/graph"
)

// history builds commits from "sha parent parent..." descriptions, taken in
// the order given — which the caller is responsible for making topological,
// children before parents, exactly as git log --topo-order hands them over.
func history(descriptions ...string) []git.Commit {
	commits := make([]git.Commit, 0, len(descriptions))
	for _, description := range descriptions {
		fields := strings.Fields(description)
		commits = append(commits, git.Commit{SHA: fields[0], Parents: fields[1:]})
	}
	return commits
}

func TestEmptyHistory(t *testing.T) {
	assigned := graph.Assign(nil)

	if len(assigned.Lanes) != 0 || len(assigned.Edges) != 0 {
		t.Fatalf("expected an empty graph, got %+v", assigned)
	}
	if assigned.Width != 0 {
		t.Errorf("an empty graph is zero columns wide, got %d", assigned.Width)
	}
}

func TestSingleRootCommit(t *testing.T) {
	commits := history("a")
	assigned := graph.Assign(commits)

	if !slices.Equal(assigned.Lanes, []int{0}) {
		t.Errorf("lanes = %v, expected [0]", assigned.Lanes)
	}
	// A root has no parent, so no line leaves its dot.
	if len(assigned.Edges) != 0 {
		t.Errorf("a lone root draws no line, got %v", assigned.Edges)
	}
	checkInvariants(t, commits, assigned)
}

func TestLinearHistoryStaysInOneColumn(t *testing.T) {
	commits := history("c b", "b a", "a")
	assigned := graph.Assign(commits)

	if !slices.Equal(assigned.Lanes, []int{0, 0, 0}) {
		t.Errorf("a linear history never leaves column 0, got %v", assigned.Lanes)
	}
	if assigned.Width != 1 {
		t.Errorf("width = %d, expected 1", assigned.Width)
	}
	checkInvariants(t, commits, assigned)
}

// TestBranchAndMerge is the shape every graph is judged on: a branch leaves
// the trunk, lives beside it, and comes back.
func TestBranchAndMerge(t *testing.T) {
	commits := history("m t f", "f b", "t b", "b a", "a")
	assigned := graph.Assign(commits)

	if !slices.Equal(assigned.Lanes, []int{0, 1, 0, 0, 0}) {
		t.Errorf("lanes = %v, expected [0 1 0 0 0]", assigned.Lanes)
	}
	if assigned.Width != 2 {
		t.Errorf("a single branch needs two columns, got %d", assigned.Width)
	}
	checkInvariants(t, commits, assigned)
}

// TestMergedBranchFreesItsColumn is the defect this module exists to avoid.
//
// Once a branch has been absorbed, its column must be free again. Leaving it
// open makes every later branch step one column further right, and a history
// with a few hundred merges becomes a picture nobody can read.
//
// The shape is a trunk with two branches, the first fully absorbed before the
// second leaves. Two columns is the honest answer; anything wider means the
// first branch is still holding one.
func TestMergedBranchFreesItsColumn(t *testing.T) {
	commits := history(
		"m2 t2 f2", // the second merge
		"f2 m1",    // its branch, forked from the first merge
		"t2 m1",    // the trunk between the two merges
		"m1 t1 f1", // the first merge
		"f1 b",     // its branch, forked from the base
		"t1 b",     // the trunk between the base and the first merge
		"b a", "a",
	)
	assigned := graph.Assign(commits)

	if assigned.Width != 2 {
		t.Errorf("two merges one after the other reuse one column: width = %d, expected 2",
			assigned.Width)
	}
	if !slices.Equal(assigned.Lanes, []int{0, 1, 0, 0, 1, 0, 0, 0}) {
		t.Errorf("lanes = %v, expected [0 1 0 0 1 0 0 0]", assigned.Lanes)
	}
	checkInvariants(t, commits, assigned)
}

// TestConvergenceFreesEveryWaitingColumn covers what the freeing step is
// really about: three columns waiting for one commit at once.
func TestConvergenceFreesEveryWaitingColumn(t *testing.T) {
	commits := history("t3 b", "t2 b", "t1 b", "b a", "a")
	assigned := graph.Assign(commits)

	if !slices.Equal(assigned.Lanes, []int{0, 1, 2, 0, 0}) {
		t.Errorf("lanes = %v, expected [0 1 2 0 0]", assigned.Lanes)
	}
	// Below the commit that absorbs all three, the picture is one column wide.
	for _, edge := range assigned.Edges {
		if edge.From >= 3 && edge.Lane != 0 {
			t.Errorf("row %d still draws in column %d after the convergence",
				edge.From, edge.Lane)
		}
	}
	checkInvariants(t, commits, assigned)
}

func TestOctopusMerge(t *testing.T) {
	commits := history("o a b c d", "a r", "b r", "c r", "d r", "r")
	assigned := graph.Assign(commits)

	if !slices.Equal(assigned.Lanes, []int{0, 0, 1, 2, 3, 0}) {
		t.Errorf("lanes = %v, expected [0 0 1 2 3 0]", assigned.Lanes)
	}

	leaving := 0
	for _, edge := range assigned.Edges {
		if edge.From == 0 {
			leaving++
		}
	}
	if leaving != 4 {
		t.Errorf("an octopus of four parents draws four lines from its dot, got %d", leaving)
	}
	checkInvariants(t, commits, assigned)
}

// TestCrissCrossMerge is the topology that breaks naive implementations: two
// branches that each merge the other before merging for good.
func TestCrissCrossMerge(t *testing.T) {
	commits := history(
		"top x y",
		"x a b",
		"y b a",
		"a base", "b base",
		"base",
	)
	assigned := graph.Assign(commits)
	checkInvariants(t, commits, assigned)

	if assigned.Width > 3 {
		t.Errorf("a criss-cross needs three columns at most, got %d", assigned.Width)
	}
}

func TestSeveralRootsEachEndTheirColumn(t *testing.T) {
	// Two histories with nothing in common, as an imported project leaves.
	commits := history("b2 r2", "b1 r1", "r2", "r1")
	assigned := graph.Assign(commits)

	if !slices.Equal(assigned.Lanes, []int{0, 1, 0, 1}) {
		t.Errorf("lanes = %v, expected [0 1 0 1]", assigned.Lanes)
	}
	// Both roots close their column: nothing leaves the bottom of the picture.
	for _, edge := range assigned.Edges {
		if edge.To == graph.Absent {
			t.Errorf("a complete history leaves no line hanging, got %+v", edge)
		}
	}
	checkInvariants(t, commits, assigned)
}

// TestMissingParentLeavesTheBottom covers a truncated or shallow history,
// where the oldest rows name parents that were never sent. The line is real
// and it has to be drawn leaving the picture, not quietly dropped.
func TestMissingParentLeavesTheBottom(t *testing.T) {
	commits := history("b a-not-here")
	assigned := graph.Assign(commits)

	if len(assigned.Edges) != 1 {
		t.Fatalf("expected one line leaving the dot, got %v", assigned.Edges)
	}
	if assigned.Edges[0].To != graph.Absent {
		t.Errorf("a line to an absent parent ends at Absent, got %d", assigned.Edges[0].To)
	}
	checkInvariants(t, commits, assigned)
}

// TestMergedBranchKeepsItsColour pins the reason an Edge carries a Lane of its
// own. The line that ends on a merge commit belongs to the branch being
// absorbed, not to the column it lands in.
func TestMergedBranchKeepsItsColour(t *testing.T) {
	commits := history("m t f", "f b", "t b", "b")
	assigned := graph.Assign(commits)

	// The merge's second parent is the branch, and it runs down column 1
	// while the merge commit itself sits in column 0.
	var branch *graph.Edge
	for index, edge := range assigned.Edges {
		if edge.From == 0 && edge.Lane != assigned.Lanes[0] {
			branch = &assigned.Edges[index]
		}
	}
	if branch == nil {
		t.Fatalf("expected the merged branch to run down its own column, got %v", assigned.Edges)
	}
	if branch.Lane != assigned.Lanes[1] {
		t.Errorf("the line to the branch runs down column %d, but its tip sits in column %d",
			branch.Lane, assigned.Lanes[1])
	}
}

func TestCrossingKeepsLinesThatOnlyPassThrough(t *testing.T) {
	// A branch forked at the very top and merged at the very bottom: in the
	// middle of the history neither of its ends is on screen, and the line
	// still has to be drawn.
	descriptions := []string{"tip long spine8"}
	for index := 8; index > 0; index-- {
		descriptions = append(descriptions, fmt.Sprintf("spine%d spine%d", index, index-1))
	}
	descriptions = append(descriptions, "long spine0", "spine0")
	commits := history(descriptions...)
	assigned := graph.Assign(commits)
	checkInvariants(t, commits, assigned)

	middle := assigned.Crossing(4, 6)
	passing := 0
	for _, edge := range middle {
		if edge.From < 4 && edge.To >= 6 {
			passing++
		}
	}
	if passing == 0 {
		t.Errorf("the branch crossing rows 4 to 6 is missing from %v", middle)
	}

	// And a window sees nothing that ends above it.
	for _, edge := range assigned.Crossing(6, 8) {
		if edge.To != graph.Absent && edge.To < 6 {
			t.Errorf("%+v ends above the window and should not be in it", edge)
		}
	}
}

func TestCrossingAnEmptyRange(t *testing.T) {
	assigned := graph.Assign(history("b a", "a"))
	if crossing := assigned.Crossing(1, 1); len(crossing) != 0 {
		t.Errorf("an empty range crosses nothing, got %v", crossing)
	}
}

// TestCrossingMatchesTheScanItReplaced is the whole safety of the edge index.
//
// The index is an accelerator and nothing else, so the only thing that has to
// be proved about it is that it changes no answer: for any window, on a
// history of any shape, Crossing must return the identical list — the same
// edges, in the same order — as reading every edge and keeping what crosses.
// That scan is kept below as the oracle it now is, and it is the code this
// replaced, character for character.
//
// The windows are not the ones a caller sends. They are the ones an index gets
// wrong: empty, inverted, negative, and past the end of the history, where a
// bucket lookup has nothing to answer with.
func TestCrossingMatchesTheScanItReplaced(t *testing.T) {
	for _, subject := range crossingSubjects() {
		t.Run(subject.name, func(t *testing.T) {
			rows := len(subject.graph.Lanes)
			for _, window := range crossingWindows(rows) {
				first, last := window[0], window[1]
				indexed := subject.graph.Crossing(first, last)
				scanned := scanCrossing(subject.graph, first, last)
				if !slices.Equal(indexed, scanned) {
					t.Fatalf("Crossing(%d, %d) over %d rows: %d edges against the scan's %d, %s",
						first, last, rows, len(indexed), len(scanned),
						difference(indexed, scanned))
				}
			}
		})
	}
}

// scanCrossing is Crossing as it stood before the index: every edge in the
// history looked at to answer one window. Kept here as the oracle, because a
// faster answer that is not the same answer is not an answer.
func scanCrossing(assigned graph.Graph, first, last int) []graph.Edge {
	if first >= last {
		return nil
	}

	var visible []graph.Edge
	for _, edge := range assigned.Edges {
		if edge.From >= last {
			continue
		}
		if edge.To != graph.Absent && edge.To < first {
			continue
		}
		visible = append(visible, edge)
	}
	return visible
}

// difference says where two answers to one window first disagree. Printing
// both lists whole is unreadable: a window of a large history holds hundreds
// of edges, and what matters is the first one that is wrong.
func difference(indexed, scanned []graph.Edge) string {
	at := 0
	for at < len(indexed) && at < len(scanned) && indexed[at] == scanned[at] {
		at++
	}

	held := func(edges []graph.Edge) string {
		if at >= len(edges) {
			return "nothing"
		}
		return fmt.Sprintf("%+v", edges[at])
	}
	return fmt.Sprintf("at %d the index has %s and the scan %s", at, held(indexed), held(scanned))
}

type crossingSubject struct {
	name  string
	graph graph.Graph
}

// crossingSubjects are the histories an edge index has to survive: too short
// to fill one bucket, long enough to fill several, empty, carrying lines that
// leave the bottom of the picture, and carrying one line that runs the whole
// length of it.
func crossingSubjects() []crossingSubject {
	long := randomHistory(11, 2000)
	assigned := graph.Assign(long)

	return []crossingSubject{
		{"an empty history", graph.Assign(nil)},
		{"a lone root", graph.Assign(history("a"))},
		{"shorter than one bucket", graph.Assign(randomHistory(7, 40))},
		{"several buckets", assigned},
		{"lines leaving the bottom", graph.Assign(withMissingParents(long))},
		{"one line down the whole picture", graph.Assign(spineUnderOneLongBranch(2000))},

		// A Graph nobody assigned. The exported fields allow one, and it has
		// no index, so this is the fallback path answering on its own.
		{"no index at all", graph.Graph{
			Lanes: assigned.Lanes, Edges: assigned.Edges, Width: assigned.Width,
		}},
	}
}

// crossingWindows are the ranges to compare the index against the scan over:
// the awkward ones by hand, and then random ones reaching a long way outside
// the history at both ends, so that no bucket boundary is special.
func crossingWindows(rows int) [][2]int {
	windows := [][2]int{
		{0, 0}, {5, 5}, {rows, rows},
		{10, 3}, {rows, 0}, {1, -1},
		{-100, -50}, {-100, 10}, {-1, rows + 1},
		{rows - 1, rows}, {rows, rows + 500}, {rows + 500, rows + 900},
		{0, rows}, {0, 1}, {0, 200}, {255, 257}, {256, 512},
	}

	// Wider than a bucket, so a window can start outside the index on either
	// side without the generator having to know how wide a bucket is.
	const beyond = 1000

	random := rand.New(rand.NewPCG(0x43, 0x9e3779b97f4a7c15))
	for range 1500 {
		first := random.IntN(rows+2*beyond) - beyond
		windows = append(windows, [2]int{first, first + random.IntN(beyond) - beyond/4})
	}
	return windows
}

// withMissingParents is what a shallow clone or a truncated log hands over:
// the oldest commits are gone, and the rows left behind name parents that were
// never sent. The newest row is given one too, so that the index is measured
// against a line leaving the bottom of the picture from the very top of it —
// the one edge that crosses every window there is.
func withMissingParents(commits []git.Commit) []git.Commit {
	kept := slices.Clone(commits[:len(commits)*3/4])
	kept[0].Parents = append(slices.Clone(kept[0].Parents), "never-sent")
	return kept
}

// spineUnderOneLongBranch is a straight history whose newest commit also has
// the oldest for a parent: one line running from the first row to the last,
// crossing every bucket the index holds.
func spineUnderOneLongBranch(rows int) []git.Commit {
	descriptions := []string{fmt.Sprintf("%s %s %s", name(rows-1), name(rows-2), name(0))}
	for index := rows - 2; index > 0; index-- {
		descriptions = append(descriptions, fmt.Sprintf("%s %s", name(index), name(index-1)))
	}
	descriptions = append(descriptions, name(0))
	return history(descriptions...)
}

// TestRandomHistories runs the invariants over generated histories. The shapes
// below are the ones nobody writes by hand, and they are where an assignment
// that merely looks right stops being right.
func TestRandomHistories(t *testing.T) {
	for seed := range 300 {
		commits := randomHistory(uint64(seed), 150)
		assigned := graph.Assign(commits)
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) {
			checkInvariants(t, commits, assigned)
		})
	}
}

// randomHistory generates a tangled but well-formed history: parents always
// older than their children, a spine of single-parent commits, merges reaching
// back to older commits, and now and then an unrelated root.
func randomHistory(seed uint64, count int) []git.Commit {
	random := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))

	// Built oldest first, so a parent can only be chosen among the commits
	// already generated, then reversed: children before parents is what the
	// caller of Assign has to provide.
	commits := make([]git.Commit, count)
	for index := range count {
		var parents []string
		switch {
		case index == 0 || random.IntN(20) == 0:
			// A root: the first commit, and now and then an unrelated history.
		default:
			parents = append(parents, name(index-1-random.IntN(min(index, 4))))
			for random.IntN(3) == 0 {
				parents = append(parents, name(random.IntN(index)))
			}
		}
		commits[index] = git.Commit{SHA: name(index), Parents: parents}
	}

	slices.Reverse(commits)
	return commits
}

func name(index int) string { return fmt.Sprintf("%04d", index) }

// checkInvariants is the real test. Every case above hands its graph to it,
// because what matters is not the columns a particular history produces but
// that the picture is a faithful drawing of the history:
//
//   - every commit has one line per parent, ending on that parent;
//   - no line ever points back up the screen;
//   - a commit's first parent continues its column, so a branch keeps one
//     colour for its whole length;
//   - two branches never share a column at the same height.
func checkInvariants(t *testing.T, commits []git.Commit, assigned graph.Graph) {
	t.Helper()

	if len(assigned.Lanes) != len(commits) {
		t.Fatalf("%d lanes for %d commits", len(assigned.Lanes), len(commits))
	}

	rowOf := make(map[string]int, len(commits))
	for index, commit := range commits {
		rowOf[commit.SHA] = index
	}

	widest := -1
	for index, lane := range assigned.Lanes {
		if lane < 0 {
			t.Fatalf("commit %d sits in column %d", index, lane)
		}
		widest = max(widest, lane)
	}

	// One edge per parent, in the order the parents are listed, so that the
	// first parent — the one that continues the trunk — is identifiable.
	next := 0
	for index, commit := range commits {
		for position, parent := range commit.Parents {
			if next >= len(assigned.Edges) {
				t.Fatalf("commit %s has no line for parent %s", commit.SHA, parent)
			}
			edge := assigned.Edges[next]
			next++
			widest = max(widest, edge.Lane)

			if edge.From != index {
				t.Fatalf("expected a line leaving row %d, got %+v", index, edge)
			}

			expected, present := rowOf[parent]
			if !present {
				expected = graph.Absent
			}
			if edge.To != expected {
				t.Errorf("commit %s: the line to parent %s ends at %d, expected %d",
					commit.SHA, parent, edge.To, expected)
			}
			if edge.To != graph.Absent && edge.To <= edge.From {
				t.Errorf("commit %s: the line to parent %s points back up the screen: %+v",
					commit.SHA, parent, edge)
			}
			if position == 0 && edge.Lane != assigned.Lanes[index] {
				t.Errorf("commit %s: its first parent leaves column %d, but the commit "+
					"sits in column %d — the branch changes colour at every commit",
					commit.SHA, edge.Lane, assigned.Lanes[index])
			}
		}
	}
	if next != len(assigned.Edges) {
		t.Errorf("%d lines drawn for %d parents", len(assigned.Edges), next)
	}

	if assigned.Width != widest+1 {
		t.Errorf("width = %d, but the picture reaches column %d", assigned.Width, widest)
	}

	checkColumnsAreExclusive(t, commits, assigned)
}

// checkColumnsAreExclusive: two lines may share a column only where they end
// on the same commit, which is one line arriving at a dot from two directions.
// Anywhere else it is two branches drawn on top of each other, and the picture
// is a lie.
func checkColumnsAreExclusive(t *testing.T, commits []git.Commit, assigned graph.Graph) {
	t.Helper()

	// An absent parent means the line runs to the bottom of the picture.
	bottom := func(edge graph.Edge) int {
		if edge.To == graph.Absent {
			return len(commits)
		}
		return edge.To
	}

	byLane := make(map[int][]graph.Edge)
	for _, edge := range assigned.Edges {
		byLane[edge.Lane] = append(byLane[edge.Lane], edge)
	}

	for lane, edges := range byLane {
		for i := range edges {
			for j := i + 1; j < len(edges); j++ {
				first, second := edges[i], edges[j]
				if first.To == second.To {
					continue
				}
				// A column is released on the row its line ends, so the next
				// line may start on that same row.
				if bottom(first) <= second.From || bottom(second) <= first.From {
					continue
				}
				t.Errorf("column %d carries %+v and %+v at the same height", lane, first, second)
			}
		}
	}
}
