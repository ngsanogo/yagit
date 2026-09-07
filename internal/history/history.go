// Package history keeps the assigned history of each open repository, so that
// a window onto it can be served without reading the whole thing again.
//
// It exists because of one property of the commit graph: a commit's column
// cannot be derived from the commits around it. It follows from every commit
// above it, so the assignment is over the whole history or it is wrong. Paging
// the transport is therefore not paging the computation, and something has to
// hold the computed answer between two requests. This is that something.
//
// A history is held per repository and per set of refs — which the walk covers
// (docs/adr/0016, docs/adr/0033) — for the same reason: two sets see different
// commits, so they are different assignments and not two views of one. Held,
// but not forever: the sets a person picks are unbounded and the memory they
// cost is not, so the picked ones have a ceiling. See chosenHistories.
//
// Depends on git, repo and graph. Nothing depends back on it.
package history

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"sync"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/graph"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Window is a range of rows of an assigned history, with everything needed to
// draw that range and nothing more.
type Window struct {
	// Commits are the window's rows, newest first, as git listed them.
	Commits []git.Commit

	// Edges are every line with something to draw across the window,
	// including those whose two ends are both outside it. Their row indices
	// are absolute in the history, never relative to the window.
	Edges []graph.Edge

	First int // index of the window's first row, in the whole history
	Total int // how many commits the history holds
	Width int // how many columns the picture needs

	// lanes is the whole history's assignment, held rather than copied.
	//
	// Safe to keep: a refresh replaces the slice wholesale instead of writing
	// into it, so a window handed out before one goes on describing the
	// history it was cut from, entire and consistent, rather than tearing
	// halfway through.
	lanes []int
}

// LaneOf returns the column of a row's dot — of any row in the history, not
// only those inside the window. An edge crossing the window bends onto dots
// that lie outside it, and the renderer needs to know where.
//
// A row outside the history answers graph.Absent, which is what an edge with
// no parent already means: a line that leaves the picture.
func (w Window) LaneOf(row int) int {
	if row < 0 || row >= len(w.lanes) {
		return graph.Absent
	}
	return w.lanes[row]
}

// Store holds the assigned history of every open repository.
type Store struct {
	runner *git.Runner

	mutex  sync.Mutex
	byWalk map[walk]*entry

	// handedOut counts entries given out, and is the clock eviction reads.
	// A counter rather than a time: it is strictly ordered, it costs nothing,
	// and two walks handed out in the same nanosecond still have an order.
	handedOut uint64
}

// walk is one repository seen through one choice of refs.
//
// The scope is part of the key rather than a field to compare, because the
// answers are different assignments and none can be cut out of another: a
// commit's column follows from every commit above it, so the commits one scope
// left out move the ones it kept. Holding them all is what makes the choice on
// screen a switch rather than a reload — a person comparing the current branch
// with every ref would otherwise pay for the whole walk on every press.
//
// chosen is the same argument one level down. Under git.ScopeRefs the chosen
// set IS the scope: main alone and main with a topic branch are two histories,
// and a key that ignored the choice would answer the second from the first's
// assignment — every column wrong, and wrong in a way that looks right.
type walk struct {
	repository string
	scope      git.Scope
	chosen     string
}

// chosenKey identifies a set of refs by its contents rather than by the order
// they arrived in.
//
// Sorted, because the picker's order is nobody's business: the same three refs
// ticked in a different sequence are the same walk, and keying on the sequence
// would pay for the whole log again to answer with identical rows.
// Deduplicated with it — a repeated ref changes no walk either.
//
// NUL joins them for the reason it separates fields everywhere else in this
// project: a ref name can hold neither it nor a newline, so no two different
// sets can produce one key.
func chosenKey(selected []string) string {
	if len(selected) == 0 {
		return ""
	}
	unique := slices.Clone(selected)
	slices.Sort(unique)
	return strings.Join(slices.Compact(unique), "\x00")
}

// chosenHistories is how many picked-ref walks one repository keeps.
//
// A ceiling is needed here and nowhere else in this map, because this is the
// only part of the key a person can invent. The other two scopes are two keys
// — head and all — and holding both is the whole point: they are what the
// switch on screen switches between, and paying for the walk again on every
// press is the thing this package exists to avoid.
//
// The picked set is unbounded by construction. Every tick of a box is another
// key, so a repository with twenty references has a million of them, and each
// entry holds a whole assigned history — the commits and their lanes, tens of
// megabytes on a large repository — until the tab is closed. Somebody
// exploring the picker for a minute could hold a gigabyte of walks they will
// never look at again.
//
// Four, because that is the number of pictures somebody compares in one
// sitting. The fifth costs a re-walk and costs nothing else: an assignment
// dropped here is an assignment computed again, never an answer that is wrong.
const chosenHistories = 4

// entry is one repository's cached history, under one scope.
//
// It carries its own mutex, held for as long as git takes. A single lock over
// the whole store would make one repository's first load — seconds, on a large
// one — block every request about every other.
type entry struct {
	mutex       sync.Mutex
	fingerprint string
	commits     []git.Commit
	graph       graph.Graph

	// used orders eviction: the store's counter at the last time this entry
	// was handed out. Read and written under the STORE's mutex and never the
	// entry's, which is what keeps it out of the lock held across git.
	used uint64
}

func NewStore(runner *git.Runner) *Store {
	return &Store{runner: runner, byWalk: make(map[walk]*entry)}
}

// Window returns the rows [first, first+count) of a repository's history under
// one scope, assigning the whole of that scope's walk first if what is held is
// missing or out of date. selected is the set git.ScopeRefs walks and is read
// under no other scope.
//
// A range reaching past the end is clamped rather than refused: the interface
// asks for a fixed number of rows and the history shrinks under it whenever a
// branch is deleted.
func (s *Store) Window(
	ctx context.Context, opened *repo.Repo, first, count int, scope git.Scope, selected []string,
) (Window, error) {
	held := s.entryFor(walk{repository: opened.ID, scope: scope, chosen: chosenKey(selected)})
	held.mutex.Lock()
	defer held.mutex.Unlock()

	if err := s.refresh(ctx, held, opened, scope, selected); err != nil {
		return Window{}, err
	}

	total := len(held.commits)
	from := min(max(first, 0), total)
	to := min(from+max(count, 0), total)

	return Window{
		Commits: held.commits[from:to],
		Edges:   held.graph.Crossing(from, to),
		First:   from,
		Total:   total,
		Width:   held.graph.Width,
		lanes:   held.graph.Lanes,
	}, nil
}

// Located is a commit and where it sits: the row the list scrolls to, and the
// column its dot is drawn in.
type Located struct {
	Row    int
	Commit git.Commit
	Lane   int
}

// Locate finds a commit by SHA within one scope's walk, reassigning the
// history first when the refs or HEAD have moved. The second result is false
// when that walk does not hold it.
//
// The row is why this exists. It cannot be derived from a page — a branch's
// tip is usually in none of the pages the interface happens to hold — and it
// follows from the whole assignment, which lives here.
//
// The scope is not optional, and the reason is what a row means: it is a
// position in one walk, so the same commit sits at different rows under
// different scopes and at no row at all under one that does not reach it. A
// branch that was never merged is exactly that case, and answering from
// another scope's assignment would scroll the list to a row holding a
// different commit.
//
// A scan rather than an index kept beside the commits. A map from SHA to row
// costs tens of megabytes on a history with a million commits and would be
// held for as long as the repository is open, to answer a question asked once
// per click. The scan costs milliseconds nobody perceives.
func (s *Store) Locate(
	ctx context.Context, opened *repo.Repo, sha string, scope git.Scope, selected []string,
) (Located, bool, error) {
	held := s.entryFor(walk{repository: opened.ID, scope: scope, chosen: chosenKey(selected)})
	held.mutex.Lock()
	defer held.mutex.Unlock()

	if err := s.refresh(ctx, held, opened, scope, selected); err != nil {
		return Located{}, false, err
	}

	for row := range held.commits {
		if held.commits[row].SHA == sha {
			return Located{
				Row:    row,
				Commit: held.commits[row],
				Lane:   held.graph.Lanes[row],
			}, true, nil
		}
	}

	return Located{}, false, nil
}

// Forget drops a repository's history, under every scope it was read through.
// Called when a repository is closed: without it, a tab shut after browsing a
// large history would leave every byte of it held for the life of the daemon.
func (s *Store) Forget(identifier string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for held := range s.byWalk {
		if held.repository == identifier {
			delete(s.byWalk, held)
		}
	}
}

// Held says how many walks of one repository the store is keeping.
//
// Exported because the ceiling is a promise about memory, and a promise about
// memory that nothing can observe is a comment. This is what the tests hold it
// to.
func (s *Store) Held(identifier string) int {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var count int
	for held := range s.byWalk {
		if held.repository == identifier {
			count++
		}
	}
	return count
}

func (s *Store) entryFor(key walk) *entry {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	held, present := s.byWalk[key]
	if !present {
		held = &entry{}
		s.byWalk[key] = held
	}

	s.handedOut++
	held.used = s.handedOut

	if key.scope == git.ScopeRefs {
		s.evictChosen(key.repository)
	}
	return held
}

// evictChosen drops a repository's least recently used picked-ref walks until
// it is within chosenHistories. The caller holds the store's mutex.
//
// Dropping an entry another request is using at this moment is safe, and it is
// worth saying why: that request holds a POINTER, not a map lookup, so it goes
// on holding a whole and consistent entry to the end of its work — the same
// property that lets Window hand out a lanes slice. What it loses is the
// cache, not the answer. A later request for the same walk finds nothing,
// makes a second entry and reads git again, which is slower and never wrong.
func (s *Store) evictChosen(repository string) {
	// At most one entry can have been added since the last check, so this
	// removes at most one — the loop is the invariant, not the expected work.
	for {
		var count int
		var oldest walk
		var oldestUse uint64

		for key, held := range s.byWalk {
			if key.repository != repository || key.scope != git.ScopeRefs {
				continue
			}
			count++
			if oldestUse == 0 || held.used < oldestUse {
				oldest, oldestUse = key, held.used
			}
		}

		if count <= chosenHistories {
			return
		}
		delete(s.byWalk, oldest)
	}
}

// refresh reassigns the history when the refs or HEAD have moved, and does
// nothing when they have not. The caller holds the entry's lock.
func (s *Store) refresh(
	ctx context.Context, held *entry, opened *repo.Repo, scope git.Scope, selected []string,
) error {
	refs, err := s.runner.ForEachRef(ctx, opened.Path)
	if err != nil {
		return err
	}

	head, err := s.runner.ReadHEAD(ctx, opened.Path)
	if err != nil {
		return err
	}

	// What a walk answers is decided entirely by which commits the refs and
	// HEAD point at: the objects behind them cannot change without one of them
	// moving, because a commit's name is a hash of its content. So the two
	// together are a complete fingerprint of the history, not a heuristic that
	// is usually right.
	//
	// HEAD is in it because a checkout moves HEAD without moving a single ref,
	// and two of the three scopes walk from it — one starts there, the other
	// includes it. The refs alone would leave the picture on the branch you
	// just left.
	//
	// The chosen set is not in the fingerprint and does not need to be: it is
	// in the key of the entry this fingerprint belongs to, so two choices
	// never share one.
	fingerprint := fingerprintOf(refs, head)
	if held.commits != nil && held.fingerprint == fingerprint {
		return nil
	}

	commits, err := s.runner.LogScope(ctx, opened.Path, scope, refs, head, selected)
	if err != nil {
		return err
	}

	held.commits = commits
	held.graph = graph.Assign(commits)
	held.fingerprint = fingerprint

	return nil
}

func fingerprintOf(refs []git.Ref, head git.HEAD) string {
	sum := sha256.New()
	sum.Write([]byte(head.SHA))
	sum.Write([]byte{0})
	sum.Write([]byte(head.Name))
	sum.Write([]byte{0})
	for _, ref := range refs {
		// NUL separates the fields for the same reason it does in the git
		// layer: a ref name can hold neither it nor a newline, so no two
		// different ref lists can hash the same bytes.
		sum.Write([]byte(ref.Name))
		sum.Write([]byte{0})
		sum.Write([]byte(ref.SHA))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}
