package api

import (
	"context"
	"net/http"
)

// How long a git command is allowed to outlive the browser that asked for it.
//
// Every request context is cancelled the moment the client disconnects, and
// internal/git runs each command under the context it is given: a reload, a
// closed tab, a laptop lid, and the process is killed with SIGKILL wherever it
// had got to. For a `log` or a `status` that is exactly right — nobody is left
// to read the answer, so the work is waste.
//
// For a command that WRITES it is the worst thing the daemon can do. A merge
// killed between checking out the tree and recording the commit leaves a
// half-written merge and an index.lock, and it does it in the one situation
// where a user is most likely to hit reload: the button has been spinning for
// twenty seconds, because git is running their pre-commit hook or waiting on a
// pinentry. Nothing is on screen to say that the repository was left in
// pieces by the reload rather than by git.
//
// So the deadline for those is the command's own timeout and nothing else.
// That is a real deadline — thirty seconds for a local command, ten minutes
// for one that rewrites the work tree or talks to another machine — so this
// cannot leak a process indefinitely; it only stops the browser from being
// one of the things that can end a write halfway.

// uninterrupted detaches a git command from the browser's attention span.
//
// The values on the request context are kept, so anything read from them —
// the logger's fields, the repository — still resolves. Only the cancellation
// is dropped, which is the point.
//
// Every route that writes uses it. Not the reads: a client that has left is a
// client whose `git log` may as well stop, and cancelling those is what keeps
// a scrolled-past page from holding the repository open.
func uninterrupted(request *http.Request) context.Context {
	return context.WithoutCancel(request.Context())
}
