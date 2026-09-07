package api

import "sync"

// Which open repositories are not being watched, and why.
//
// yagit refreshes itself because it watches each repository's git directories
// (ADR 0008). When that fails the daemon writes one line to its own journal and
// carries on serving a repository that will never refresh again: a commit made
// in the user's terminal changes nothing on screen, and nothing on screen says
// so. Silent staleness is the failure this project refuses everywhere else —
// the event stream shows "lost" rather than pretending, and the graph names the
// width it will not draw rather than drawing a narrower one.
//
// It is not hypothetical. The watch refuses a repository with more than 512 ref
// directories, which a company repository with namespaced branches and a few
// mirrored remotes reaches, and fsnotify can run out of watches on the machine
// long before that.
//
// In memory and keyed by repository, the same shape and lifetime as deletions
// beside it: the fact is worth exactly as long as the repository stays open,
// because opening it again is what would retry the watch.
type unwatched struct {
	mutex  sync.Mutex
	reason map[string]string
}

func newUnwatched() *unwatched {
	return &unwatched{reason: make(map[string]string)}
}

// record marks a repository as unwatched, with the sentence to show.
func (u *unwatched) record(repositoryID, reason string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	u.reason[repositoryID] = reason
}

// lookup answers the reason, and whether there is one at all.
func (u *unwatched) lookup(repositoryID string) (string, bool) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	reason, failed := u.reason[repositoryID]
	return reason, failed
}

// forget drops a repository's record, on close and on a re-open that watched
// it successfully. Keyed by repository for the reason deletions is: closing one
// tab must not clear the warning on another.
func (u *unwatched) forget(repositoryID string) {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	delete(u.reason, repositoryID)
}
