// Package graph turns a topologically ordered history into the picture of it:
// the column each commit's dot sits in, and the line running from every commit
// down to each of its parents.
//
// It is a pure function — no git, no HTTP, no SVG — because this is one of the
// two places in yagit where a bug is silent. A wrong lane does not crash; it
// draws a plausible graph that lies about which commit descends from which.
//
// Depends on internal/git for the Commit type, of which it reads the SHA and
// the parents and nothing else. Nothing depends back on it.
package graph

import (
	"slices"
	"sort"

	"github.com/ngsanogo/yagit/internal/git"
)

// Absent is the destination of an edge whose parent is not in the history it
// was assigned from — a shallow clone, or a history cut short. The line is
// real and it leaves the bottom of the picture, which is exactly what the
// renderer must draw rather than pretend the commit is a root.
const Absent = -1

// Edge is one line in the picture: from a commit's dot down to one of its
// parents' dots, running the whole way down a single column.
//
// A whole graph is its rows and its edges, and nothing else. The earlier shape
// of this type carried the segments crossing each row instead, which is the
// same picture written out per row: correct, and quadratic — a history with a
// few hundred concurrent branches wrote hundreds of segments on every one of
// its rows. An edge is written once however far it runs.
type Edge struct {
	// From and To are row indices into the assigned history. To is Absent
	// when the parent was not part of it.
	From int `json:"from"`
	To   int `json:"to"`

	// Lane is the column the line runs down between its two ends, and it is
	// what gives the line its colour.
	//
	// It is not in general the column of either end, and that is the point: a
	// branch absorbed by a merge runs down its own column and only bends onto
	// the merge commit's at the very last row. Colouring by either end would
	// repaint it at the join.
	Lane int `json:"lane"`
}

// Graph is the assignment of a history to columns.
type Graph struct {
	// Lanes[i] is the column of commit i's dot, counted from zero at the
	// left. One entry per commit, in the order they were given.
	Lanes []int `json:"lanes"`

	// Edges are ordered by the row they leave, which is the order the commits
	// were given in.
	Edges []Edge `json:"edges"`

	// Width is the number of columns the picture needs.
	//
	// Measured over the whole history rather than per window on purpose: a
	// graph column whose width changed as you scrolled would shift every
	// commit subject on the screen beside it.
	Width int `json:"width"`

	// open[bucket] is every edge already running when row bucket*stride is
	// reached: the lines a window starting there inherits from the rows above
	// it.
	//
	// Held because they cannot be searched for. Edges is ordered by From, so
	// the edges starting inside a window are two binary searches away — but
	// an edge from row 0 down to row 80,000 crosses every window in between
	// and starts nowhere near any of them. Without this, finding it means
	// reading the whole list, which is what answering a page used to cost.
	open [][]Edge
}

// Assign places every commit in a column.
//
// The commits must be in topological order, children before parents — what
// `git log --topo-order` guarantees — and no SHA may appear twice. Out of that
// order the result is still well formed, and it draws a history nobody has: a
// parent reached before one of its children opens a fresh column instead of
// continuing the child's, and the edge between them points back up the screen.
//
// Assign does not verify the precondition. Checking it costs a walk of the
// whole history to catch a mistake that can only come from changing the git
// command, and that command is one line with a comment on it.
func Assign(commits []git.Commit) Graph {
	rowOf := make(map[string]int, len(commits))
	for index := range commits {
		rowOf[commits[index].SHA] = index
	}

	lanes := make([]int, len(commits))
	// Sized for a history where roughly one commit in eight is a merge, which
	// is the shape of every long-lived repository looked at while writing
	// this. Being wrong here costs one regrowth, not correctness.
	edges := make([]Edge, 0, len(commits)+len(commits)/8)

	walk := walk{}
	width := 0

	for index := range commits {
		commit := &commits[index]

		// Every column waiting for this commit converges on it. The leftmost
		// is where the dot goes; the rest end here.
		//
		// Freeing those others is the step most implementations omit, and
		// omitting it is what leaves phantom columns open across hundreds of
		// rows — the single defect that makes a commit graph unreadable.
		converging := walk.waiting(commit.SHA)

		var column int
		if len(converging) > 0 {
			column = converging[0]
		} else {
			column = walk.firstFree()
		}
		for _, ending := range converging {
			walk.release(ending)
		}

		lanes[index] = column
		width = max(width, column+1)

		// Each parent gets a line leaving the dot. The first continues this
		// commit's own column, which is what keeps a branch on one colour for
		// its whole length; the others take a column already waiting for them
		// if there is one, and the leftmost free column otherwise.
		//
		// A commit with no parent is a root: its column was released above and
		// nothing is placed back into it, so no line leaves its dot.
		for position, parent := range commit.Parents {
			target := column
			if position > 0 {
				target = walk.laneFor(parent)
			}
			walk.occupy(target, parent)

			destination, known := rowOf[parent]
			if !known {
				destination = Absent
			}
			edges = append(edges, Edge{From: index, To: destination, Lane: target})
			width = max(width, target+1)
		}
	}

	// The index is built here rather than on demand because there is no
	// later moment to build it in: a Graph is a value, copied by everything
	// that holds one, and an index is only ever right for the assignment it
	// was cut from. Built beside the lanes, thrown away with them.
	return Graph{Lanes: lanes, Edges: edges, Width: width, open: openAt(edges, len(commits))}
}

// stride is how many rows one bucket of the index covers.
//
// Deliberately not the daemon's page size: this package knows nothing about
// how the history is paged, and answers the same set at any stride. What the
// number settles is a trade — a window costs the edges running at the top of
// its bucket plus every edge starting between there and the window's end, so
// halving the stride halves that walk and doubles what the index holds. At
// 256, git's own history is 334 buckets of at most its 280 columns: one
// megabyte, beside the ten that holding the assignment already costs.
const stride = 256

// openAt records, for the top of every bucket, the edges running across it.
//
// One sweep down the history, carrying the set of lines that have started and
// not yet ended. Two lines share a column only where they end on the same
// commit, so what is running at a row is at most one line per column, and the
// whole index is bounded by the width of the picture times the number of
// buckets.
func openAt(edges []Edge, rows int) [][]Edge {
	if rows <= 0 {
		return nil
	}

	buckets := make([][]Edge, (rows+stride-1)/stride)

	// Nothing is running above row 0, so bucket 0 stays empty and the sweep
	// starts at the boundary below it.
	var running []Edge
	next := 0
	for bucket := 1; bucket < len(buckets); bucket++ {
		row := bucket * stride

		// Edges is ordered by From, so everything starting above this row is
		// the next stretch of the list and never a second pass over it.
		for next < len(edges) && edges[next].From < row {
			running = append(running, edges[next])
			next++
		}

		kept := running[:0]
		for _, edge := range running {
			// An absent parent means the line leaves the bottom of the
			// picture, so it is still running at every row below its start.
			if edge.To == Absent || edge.To >= row {
				kept = append(kept, edge)
			}
		}
		running = kept

		if len(running) > 0 {
			// Copied because the sweep goes on appending to running, which
			// would otherwise write over the bucket just recorded.
			buckets[bucket] = slices.Clone(running)
		}
	}

	return buckets
}

// Crossing returns every edge with something to draw in the row range
// [first, last), in the order the edges were assigned.
//
// This is what makes a window of the history drawable on its own: an edge that
// enters the window from above and leaves below it appears here, even though
// neither of its ends is on screen.
func (g Graph) Crossing(first, last int) []Edge {
	// An empty range holds no row, so no line can be drawn in it. The reach
	// test below reads the range as a set of rows, and would otherwise let
	// through an edge that merely ends where the range begins.
	if first >= last {
		return nil
	}

	// Two halves that between them are every edge the range can hold: the
	// lines already running where the range's bucket begins, and the lines
	// starting between there and the range's end. Both are read in the order
	// Edges holds them, and the first half starts strictly above the second,
	// so the answer comes out in that order too.
	top, running := g.runningAt(first)
	starting := sort.Search(len(g.Edges), func(i int) bool { return g.Edges[i].From >= top })
	ending := sort.Search(len(g.Edges), func(i int) bool { return g.Edges[i].From >= last })

	visible := reaching(nil, running, first)
	return reaching(visible, g.Edges[starting:ending], first)
}

// reaching appends the edges that have not already ended above first. An edge
// covers the rows from From down to To, or down to the bottom of the history
// when its parent is Absent.
func reaching(visible, edges []Edge, first int) []Edge {
	for _, edge := range edges {
		if edge.To != Absent && edge.To < first {
			continue
		}
		visible = append(visible, edge)
	}
	return visible
}

// runningAt returns the top of the bucket a row falls in, and the edges the
// index recorded as running across it.
//
// Three rows have no bucket to look in: a negative one, one past the end of
// the history, and any row at all in a Graph that did not come from Assign.
// All three answer the top of the picture with nothing running, which turns
// the caller back into the scan of the whole edge list this replaced — slower,
// and the same set.
func (g Graph) runningAt(row int) (int, []Edge) {
	if row < 0 {
		return 0, nil
	}
	bucket := row / stride
	if bucket >= len(g.open) {
		return 0, nil
	}
	return bucket * stride, g.open[bucket]
}

// walk is the state of the traversal, and it is the whole of it: lanes[k] is
// the SHA that column k is waiting for, or the empty string when the column is
// free.
type walk struct {
	lanes []string
}

// waiting returns every column waiting for a SHA, in ascending order.
func (w *walk) waiting(sha string) []int {
	var found []int
	for lane, awaited := range w.lanes {
		if awaited == sha {
			found = append(found, lane)
		}
	}
	return found
}

// laneFor returns the column a parent should continue in: the one already
// waiting for it, so that two children of one commit meet in the same column
// rather than each opening their own, and otherwise the leftmost free one.
func (w *walk) laneFor(sha string) int {
	if existing := w.waiting(sha); len(existing) > 0 {
		return existing[0]
	}
	return w.firstFree()
}

// firstFree returns the leftmost free column, widening the graph only when
// every column is taken. Leftmost rather than next-available is what keeps the
// picture narrow: a column freed by a merge is reused by the next branch,
// instead of the graph growing one step to the right at every merge.
//
// The column is not reserved. Every caller places a SHA in it immediately, and
// the walk is sequential.
func (w *walk) firstFree() int {
	for lane, awaited := range w.lanes {
		if awaited == "" {
			return lane
		}
	}
	w.lanes = append(w.lanes, "")
	return len(w.lanes) - 1
}

func (w *walk) occupy(lane int, sha string) { w.lanes[lane] = sha }

func (w *walk) release(lane int) { w.lanes[lane] = "" }
