package git

// What the tests ask of a status and the daemon never does: the interface
// reads the file list itself, and every operation that cares counts what it
// cares about (see DirtyTracked). Declared here, so the questions exist for
// the assertions that read best with them and nowhere else.

// Clean says nothing differs anywhere: no staged change, no unstaged change,
// no untracked file.
func (s Status) Clean() bool { return len(s.Files) == 0 }

// Conflicted says the repository is in the middle of a merge that did not
// resolve on its own.
func (s Status) Conflicted() bool {
	for _, file := range s.Files {
		if file.Kind == EntryUnmerged {
			return true
		}
	}
	return false
}
