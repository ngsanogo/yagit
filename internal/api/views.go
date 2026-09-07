package api

import (
	"time"

	"github.com/ngsanogo/yagit/internal/repo"
)

// repositoryView is what the API exposes about an open repository.
//
// It is a view and not the repo.Repo itself because the two answer to
// different things: repo.Repo carries whatever the registry needs to run git,
// and this carries what a browser tab has a use for. The git directory, the
// bare flag's origin, anything added to the registry later — none of it
// crosses this line unless somebody writes it here on purpose.
//
// The path does cross it, and it is worth saying why, because an earlier
// version of this type left it out on the grounds that it reveals filesystem
// layout to whoever holds the token.
//
// Whoever holds the token is the user. They typed the path in to open the
// repository, /api/repos/discover hands them the absolute path of every
// repository it finds — that is the whole feature — and POST /api/repos takes
// a path, so nothing the daemon does is reachable without them. Withholding
// it here bought no secrecy and cost the tab bar the only thing that tells
// two repositories called "api" apart, which is the case the tab bar exists
// for. The security boundary is that a path from the network is checked
// against YAGIT_ROOT before it reaches the disk, and that is in repo.Open,
// where it can be enforced rather than merely hoped for.
type repositoryView struct {
	ID       string    `json:"id"`
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Bare     bool      `json:"bare"`
	OpenedAt time.Time `json:"opened_at"`

	// Watched says the daemon is following this repository's git directories,
	// so that a commit made in the user's own terminal reaches the screen.
	//
	// False is the interesting value and the reason this field exists: the
	// watch can be refused — more ref directories than it will follow, or no
	// watches left on the machine — and until this was carried, the answer for
	// a repository that would never refresh again was identical to the answer
	// for one that refreshes perfectly.
	Watched bool `json:"watched"`

	// WatchFailure is what went wrong, in the operating system's own words,
	// and empty whenever Watched is true. Shown rather than summarised: "more
	// than 512 ref directories" is something a person can act on, and "could
	// not watch this repository" is not.
	WatchFailure string `json:"watch_failure,omitempty"`
}

// Both take the unwatched record rather than reading it off a server, so that
// a view stays a pure function of what it is handed — and so that a test can
// build one for a repository that is not being watched.
func repositoryViews(repos []*repo.Repo, unwatched *unwatched) []repositoryView {
	views := make([]repositoryView, 0, len(repos))
	for _, opened := range repos {
		views = append(views, repositoryViewOf(opened, unwatched))
	}
	return views
}

func repositoryViewOf(opened *repo.Repo, unwatched *unwatched) repositoryView {
	view := repositoryView{
		ID:       opened.ID,
		Path:     opened.Path,
		Name:     opened.Name,
		Bare:     opened.Bare,
		OpenedAt: opened.OpenedAt,
		Watched:  true,
	}
	// Nil is accepted so that a caller without one — a test, or a daemon
	// started with no watcher at all — still gets a view rather than a panic.
	if unwatched != nil {
		if reason, failed := unwatched.lookup(opened.ID); failed {
			view.Watched, view.WatchFailure = false, reason
		}
	}
	return view
}
