package api

import "sync"

// What yagit remembers about a branch it deleted, so Undo can put it back.
//
// Every other undo reads the HEAD reflog. This one cannot: `git branch -d`
// writes nothing to that reflog and deletes the branch's own along with the
// branch, so once the command has run, the object the branch pointed at exists
// nowhere git will name it. The tip is therefore read BEFORE the delete and
// kept here — see
// docs/adr/0032-a-deleted-branch-is-remembered-not-recalled.md.
//
// In memory, and deliberately: the record is worth exactly as long as the
// repository stays open, which is the same span "the last thing you did" means
// to somebody looking at the screen. Persisting it would promise a restore
// across restarts that `git gc` is free to make impossible in the meantime.

// deletedBranch is one such record.
type deletedBranch struct {
	// Name and SHA are the branch and the object it pointed at.
	Name string
	SHA  string

	// Head is where HEAD stood when the branch was deleted, and it is the
	// whole of how this record competes with the reflog for the single Undo
	// offer. While HEAD has not moved, the deletion is the most recent thing
	// that happened to any ref here and Undo offers it; the moment anything
	// moves HEAD, that something is more recent and the reflog answers
	// instead. Neither side needs a clock, which matters because the reflog
	// does not hand out a reliable one per entry.
	Head string
}

// deletions holds at most one record per open repository — the most recent
// deletion, because Undo offers one action and that is the one it would be.
//
// A second deletion replaces the first: the branch deleted before it is then
// past what Undo reaches, exactly as the commit before the tip is.
type deletions struct {
	mutex sync.Mutex
	byID  map[string]deletedBranch
}

func newDeletions() *deletions {
	return &deletions{byID: make(map[string]deletedBranch)}
}

func (d *deletions) record(repositoryID string, branch deletedBranch) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.byID[repositoryID] = branch
}

func (d *deletions) lookup(repositoryID string) (deletedBranch, bool) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	branch, ok := d.byID[repositoryID]
	return branch, ok
}

// forget drops a repository's record — when it has been used, when it has gone
// stale, and when the repository is closed. The last one is why this is keyed
// by repository at all rather than being a single field: closing one tab must
// not take the undo offer out of another.
func (d *deletions) forget(repositoryID string) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	delete(d.byID, repositoryID)
}
