package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/ngsanogo/yagit/internal/edit"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// The working directory over HTTP: what differs, what each difference looks
// like, and the four operations that change it.
//
// Every operation takes the same shape — one or more paths, optionally
// narrowed to a set of lines within one of them — because they are the same
// operation pointed in different directions. See selection below.

// fileView is one path git reports as differing, as the interface reads it.
//
// It carries both raw status codes AND the two booleans derived from them.
// That is not redundancy: the codes are the fact, and a file can be staged and
// unstaged at once — `git add`, then edit again — which no single verb can
// express. The booleans are the derivation, made once here rather than in
// every place that lists files.
type fileView struct {
	Path    string        `json:"path"`
	OldPath string        `json:"old_path,omitempty"`
	Kind    git.EntryKind `json:"kind"`

	Index    git.Code `json:"index"`
	WorkTree git.Code `json:"work_tree"`

	Staged   bool `json:"staged"`
	Unstaged bool `json:"unstaged"`

	// Conflict is how a path is unmerged, in words, or empty when it is not.
	// "DU" is a fact about two index stages; "deleted by us" is a sentence
	// somebody can act on.
	Conflict git.Conflict `json:"conflict,omitempty"`

	Score     int                 `json:"score,omitempty"`
	Submodule *git.SubmoduleState `json:"submodule,omitempty"`
}

type statusPayload struct {
	Branch   string `json:"branch"`
	Detached bool   `json:"detached"`
	HeadSHA  string `json:"head_sha"`
	Unborn   bool   `json:"unborn"`

	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`

	// State is the operation the work tree is in the middle of, if any.
	//
	// It travels with the file list rather than on a route of its own because
	// it is the sentence that explains that list. Seven conflicted files with
	// no word about the merge that produced them is the panel asking the user
	// to remember what they were doing.
	State git.State `json:"state"`

	Files []fileView `json:"files"`
}

func statusPayloadOf(status git.Status, state git.State) statusPayload {
	// make rather than a declaration: a nil slice marshals to null, and a
	// panel handed null where it expected a list is a panel that crashes on a
	// clean repository — which is most of them, most of the time.
	files := make([]fileView, 0, len(status.Files))
	for _, file := range status.Files {
		files = append(files, fileView{
			Path:      file.Path,
			OldPath:   file.OldPath,
			Kind:      file.Kind,
			Index:     file.Index,
			WorkTree:  file.WorkTree,
			Staged:    file.Staged(),
			Unstaged:  file.Unstaged(),
			Conflict:  file.Conflict(),
			Score:     file.Score,
			Submodule: file.Submodule,
		})
	}

	return statusPayload{
		Branch:   status.Branch,
		Detached: status.Detached,
		HeadSHA:  status.HeadSHA,
		Unborn:   status.Unborn,
		Upstream: status.Upstream,
		Ahead:    status.Ahead,
		Behind:   status.Behind,
		State:    state,
		Files:    files,
	}
}

// readStatus reads the file list and the operation that produced it, together.
//
// One function because every caller wants both, and because the two must
// describe the same moment: a merge finished between the two reads would be
// reported as conflicted files belonging to nothing.
func (s *Server) readStatus(request *http.Request, opened *repo.Repo) (statusPayload, error) {
	status, err := s.runner.Status(request.Context(), opened.Path)
	if err != nil {
		return statusPayload{}, err
	}

	state, err := git.ReadState(opened.StateDir())
	if err != nil {
		return statusPayload{}, err
	}

	return statusPayloadOf(status, state), nil
}

// answerWithStatus reads the working directory and sends it.
//
// The answer every route that CHANGES the working directory gives, and the
// reason they all give it: what they moved is the file list and the operation
// above it, so answering with the state they left behind saves the interface a
// round trip and closes the window where it would draw the state from before.
// A route that failed here has done its work already — the 500 is about the
// reading, which is why it says nothing about the command.
func (s *Server) answerWithStatus(writer http.ResponseWriter, request *http.Request, opened *repo.Repo) {
	payload, err := s.readStatus(request, opened)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, payload)
}

func (s *Server) handleStatus(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	s.answerWithStatus(writer, request, opened)
}

// maxDisplayedLineRunes bounds one line of a diff on its way to the browser.
//
// The view already refuses to draw more than a couple of thousand LINES, and
// that cap counts lines and never their length — so a regenerated bundle, a
// minified stylesheet or a one-line data file arrives as two lines of three
// megabytes, under the cap, and the tab lays out tens of thousands of visual
// rows of break-all text. The pane stops responding, choosing lines becomes
// impossible, and a screen reader is handed a three-megabyte accessible name.
//
// Two thousand runes is the same scale as the line cap beside it, and it is
// already far past anything a person reads across.
const maxDisplayedLineRunes = 2000

// boundedForDisplay is the copy of a diff that goes to the browser, with
// over-long lines cut and the cut counted.
//
// It lives here rather than in internal/git, and that is the whole point of
// it. The FileDiff that package reads is what `git apply` receives when
// somebody stages three lines out of a hunk, and a patch built from truncated
// text would write the truncation into the file. So the daemon keeps the whole
// line and only the ANSWER is shortened: a client sends back line indices,
// never text, and resolveLines re-reads the diff from git before building any
// patch.
//
// A copy, never a mutation of the argument: the caller's FileDiff is the one
// its fingerprint was computed over.
func boundedForDisplay(diff git.FileDiff) git.FileDiff {
	bounded := diff
	bounded.Hunks = make([]git.Hunk, len(diff.Hunks))

	for hunkIndex, hunk := range diff.Hunks {
		bounded.Hunks[hunkIndex] = hunk
		bounded.Hunks[hunkIndex].Lines = make([]git.DiffLine, len(hunk.Lines))

		for lineIndex, line := range hunk.Lines {
			if count := utf8.RuneCountInString(line.Text); count > maxDisplayedLineRunes {
				// By runes rather than by bytes: a dangling half of a UTF-8
				// sequence reaches the browser as a replacement character in
				// the middle of a word.
				kept := 0
				for offset := range line.Text {
					if kept == maxDisplayedLineRunes {
						line.Text = line.Text[:offset]
						break
					}
					kept++
				}
				line.Truncated = count - maxDisplayedLineRunes
			}
			bounded.Hunks[hunkIndex].Lines[lineIndex] = line
		}
	}
	return bounded
}

func (s *Server) handleDiff(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	query := request.URL.Query()

	filePath, err := checkedPath(query.Get("path"))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	side, err := requestedSide(query.Get("side"))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	// git's own listing is the only authority on what this route may look at
	// and on what the file used to be called, and it is read for the two
	// sides that ask. Not for the third: the diff is re-read on a timer while
	// a row stays selected, and a `git status` beside every one of those
	// would double what the log panel shows without changing an answer —
	// index and work tree share a name whatever happened to it.
	var index statusIndex
	if side != git.DiffUnstaged {
		status, err := s.runner.Status(request.Context(), opened.Path)
		if err != nil {
			writeError(writer, s.logger, http.StatusInternalServerError, err)
			return
		}
		index = indexStatus(status)
	}

	if side == git.DiffUntracked && index.kindOf(filePath) != git.EntryUntracked {
		// The untracked diff is `git diff --no-index`, which deliberately
		// ignores the index and the work-tree registry and simply opens the
		// file it is given. Left ungated it reads anything the daemon can
		// reach — `.git/config` and the remote URL in it, or a path through a
		// symlink the repository checked out, which is the YAGIT_ROOT
		// boundary SECURITY.md says only the open call can cross. Only a path
		// git itself called untracked has an untracked diff to show.
		writeError(writer, s.logger, http.StatusNotFound, fmt.Errorf(
			"git does not report %q as untracked, so it has no untracked diff", filePath))
		return
	}

	diff, err := s.runner.Diff(request.Context(), opened.Path, git.DiffRequest{
		Path:    filePath,
		OldPath: index.oldPathOf(filePath),
		Side:    side,
	})
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, boundedForDisplay(diff))
}

func requestedSide(raw string) (git.DiffSide, error) {
	switch git.DiffSide(raw) {
	case git.DiffStaged, git.DiffUnstaged, git.DiffUntracked:
		return git.DiffSide(raw), nil
	case "":
		return "", errors.New(
			"side is required: staged, unstaged or untracked")
	default:
		return "", fmt.Errorf(
			"side=%q is none of staged, unstaged, untracked", raw)
	}
}

// selection is the body every operation takes.
//
// One shape for all four, with the same rule: paths names what to act on, and
// Lines optionally narrows the action to part of ONE of them. That is not a
// union of two request types — it is one request with an optional refinement,
// which is what keeps "stage this file" and "stage these three lines of it"
// from being two APIs that can drift apart.
type selection struct {
	Paths []string `json:"paths"`

	Lines *lineSelection `json:"lines,omitempty"`
}

type lineSelection struct {
	// Diff is the fingerprint of the diff the selection was made against —
	// FileDiff.id, sent back unchanged.
	//
	// It is what makes partial staging safe. The daemon re-reads the diff
	// before building a patch from it, because the file may have moved on
	// since the interface drew it; if it has, the line numbers no longer
	// describe it and the request is refused rather than applied to whatever
	// is there now.
	Diff string `json:"diff"`

	// Indices are DiffLine.index values. Context lines among them are
	// ignored: context is not a change, and including it is not a choice
	// anyone can make.
	Indices []int `json:"indices"`
}

func (s *Server) readSelection(writer http.ResponseWriter, request *http.Request) (selection, bool) {
	var body selection

	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	// An unknown field is refused rather than ignored: a client that sends
	// "path" where "paths" was expected should learn it right away, not find
	// out afterwards that nothing was staged.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		// A body over the cap is not a malformed body, and saying it is sends
		// the reader looking for a typo. One integer per changed line puts a
		// single very large hunk over the limit, and the answer to that is to
		// stage the file rather than its lines — which the message says, and
		// the shape of the object cannot.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(writer, s.logger, http.StatusRequestEntityTooLarge, fmt.Errorf(
				"this selection is larger than the %d bytes a request may carry; stage or discard the whole file instead",
				maxRequestBody))
			return selection{}, false
		}
		writeError(writer, s.logger, http.StatusBadRequest,
			fmt.Errorf("unreadable request body, a {\"paths\": [\"…\"]} object is expected: %w", err))
		return selection{}, false
	}

	if len(body.Paths) == 0 {
		writeError(writer, s.logger, http.StatusBadRequest,
			errors.New("paths is empty; name at least one file"))
		return selection{}, false
	}

	for index, candidate := range body.Paths {
		checked, err := checkedPath(candidate)
		if err != nil {
			writeError(writer, s.logger, http.StatusBadRequest, err)
			return selection{}, false
		}
		body.Paths[index] = checked
	}

	if body.Lines != nil {
		if len(body.Paths) != 1 {
			// Two files' diffs have two independent numberings, so a single
			// list of indices means nothing across them. Refusing beats
			// applying it to the first and quietly ignoring the rest.
			writeError(writer, s.logger, http.StatusBadRequest,
				fmt.Errorf("lines narrows one file, but %d paths were named", len(body.Paths)))
			return selection{}, false
		}
		if body.Lines.Diff == "" {
			writeError(writer, s.logger, http.StatusBadRequest,
				errors.New("lines.diff is empty; send back the id of the diff the selection was made against"))
			return selection{}, false
		}
		if len(body.Lines.Indices) == 0 {
			writeError(writer, s.logger, http.StatusBadRequest,
				errors.New("lines.indices is empty; there would be nothing to do"))
			return selection{}, false
		}
	}

	return body, true
}

// checkedPath refuses a path that could reach outside the repository.
//
// git refuses these too — `git add -- ../../etc/passwd` fails with "outside
// repository" — and this check is here anyway. The boundary is worth
// enforcing where it can be enforced rather than left to a subcommand's
// argument handling: `git clean --force --` behaves differently from `git add
// --`, and the day a fifth operation is added is the day that difference
// matters.
func checkedPath(candidate string) (string, error) {
	if candidate == "" {
		return "", errors.New("path is empty")
	}
	// Backslashes reach here from a Windows client. git speaks forward
	// slashes in every path it reads and writes, including on Windows, so
	// converting is what makes the check see the components git will see.
	cleaned := path.Clean(strings.ReplaceAll(candidate, "\\", "/"))

	if path.IsAbs(cleaned) || hasVolumeName(cleaned) ||
		strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf(
			"%q leaves the repository; paths are relative to its root", candidate)
	}
	return cleaned, nil
}

// hasVolumeName says the path names a Windows drive, which is absolute to
// Windows and relative to everything in this function.
//
// path.IsAbs above knows only a leading `/`, so `C:/Users/…` and the
// drive-relative `C:evil` both walk straight through it — and on a Windows
// daemon git resolves them as absolute paths. Checked on every platform
// rather than behind a GOOS test: the daemon that reads the path is not
// necessarily the one that wrote it, and a boundary that holds on one
// operating system is not a boundary.
func hasVolumeName(candidate string) bool {
	if len(candidate) < 2 || candidate[1] != ':' {
		return false
	}
	letter := candidate[0]
	return ('a' <= letter && letter <= 'z') || ('A' <= letter && letter <= 'Z')
}

// errUnresolvedConflict: the file git calls unmerged still has a marker in it.
var errUnresolvedConflict = errors.New("this file still has a conflict marker in it")

// refuseUnresolved stops `git add` from ending a conflict that is not over.
//
// `git add` on a file full of `<<<<<<<` succeeds, and git commits it without a
// word — the one mistake the editing pane exists to prevent. The pane's own
// button is off while a marker is in the buffer, and that guard is worth
// nothing on its own: the list beside it offers the same operation, in bulk, on
// files nobody has opened. A rule only one caller honours is not a rule, so it
// is made here, where every caller passes.
//
// Only unmerged paths are read. A `<<<<<<<` in an ordinary file is text — this
// project's own conflict parser documents the markers in a comment — and
// refusing to stage that would be refusing to commit the documentation.
func (s *Server) refuseUnresolved(context worktreeContext) error {
	location := editLocation(context.opened)

	for _, filePath := range context.selection.Paths {
		if context.status.kindOf(filePath) != git.EntryUnmerged {
			continue
		}

		file, err := edit.Read(location, filePath)
		if err != nil {
			// Four files this cannot read, and not one of them a marker
			// somebody left behind: a conflict with nothing on disk (both
			// sides deleted it), a directory, bytes that are not text, and a
			// file past the pane's cap. `git add` is a real answer for each,
			// so what is skipped is the refusal rather than the operation.
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, edit.ErrNotRegular) ||
				errors.Is(err, edit.ErrBinary) || errors.Is(err, edit.ErrTooLarge) {
				continue
			}
			return err
		}

		if edit.HasConflictMarkers(file.Text) {
			return fmt.Errorf(
				"%w: %s — resolve it and save first, or stage it outside yagit",
				errUnresolvedConflict, filePath)
		}
	}
	return nil
}

// handleStage puts changes into the index.
func (s *Server) handleStage(writer http.ResponseWriter, request *http.Request) {
	s.applyToWorkTree(writer, request, func(context worktreeContext) error {
		if err := s.refuseUnresolved(context); err != nil {
			return err
		}

		if context.selection.Lines == nil {
			return s.runner.Stage(uninterrupted(request), context.dir(), context.selection.Paths)
		}

		diff, selected, err := s.resolveLines(request, context, context.unstagedSide())
		if err != nil {
			return err
		}
		if git.CoversEveryChange(diff, selected) {
			// Every line is the whole file, and the whole file is `git add`.
			// Not a shortcut: a patch cannot carry a mode change, a binary
			// file or a deletion, and `git add` carries all three.
			return s.runner.Stage(uninterrupted(request), context.dir(), context.selection.Paths)
		}
		return s.runner.StageLines(uninterrupted(request), context.dir(), diff, selected)
	})
}

// handleUnstage takes changes back out of the index.
func (s *Server) handleUnstage(writer http.ResponseWriter, request *http.Request) {
	s.applyToWorkTree(writer, request, func(context worktreeContext) error {
		if context.selection.Lines == nil {
			return s.runner.Unstage(uninterrupted(request), context.dir(),
				context.selection.Paths, context.status.unborn)
		}

		diff, selected, err := s.resolveLines(request, context, git.DiffStaged)
		if err != nil {
			return err
		}
		if git.CoversEveryChange(diff, selected) {
			return s.runner.Unstage(uninterrupted(request), context.dir(),
				context.selection.Paths, context.status.unborn)
		}
		return s.runner.UnstageLines(uninterrupted(request), context.dir(), diff, selected)
	})
}

// handleDiscard throws work away.
//
// The only operation here that destroys something no git command can bring
// back: what it removes was never committed and is in no reflog. The
// interface names the files and asks before calling it; this end refuses
// nothing extra, because a confirmation the daemon cannot see is not a
// confirmation it can enforce.
func (s *Server) handleDiscard(writer http.ResponseWriter, request *http.Request) {
	s.applyToWorkTree(writer, request, func(context worktreeContext) error {
		if context.selection.Lines != nil {
			diff, selected, err := s.resolveLines(request, context, context.unstagedSide())
			if err != nil {
				return err
			}
			return s.runner.DiscardLines(uninterrupted(request), context.dir(), diff, selected)
		}

		tracked, untracked := context.discardSplit()

		if len(tracked) > 0 {
			if err := s.runner.DiscardTracked(uninterrupted(request), context.dir(), tracked); err != nil {
				return err
			}
		}
		if len(untracked) > 0 {
			return s.runner.DiscardUntracked(uninterrupted(request), context.dir(), untracked)
		}
		return nil
	})
}

type discardPlanPayload struct {
	// Commands are the exact lines that would run, in order, rendered the way
	// they would be typed.
	Commands []string `json:"commands"`
}

// handleDiscardPlan answers with the commands a discard would run, and runs
// none of them.
//
// It exists because of a rule: a destructive operation shows the exact command
// and names what will be lost. The interface cannot know that command. The
// paths reach git as `:(literal)` pathspecs — without which a file named `*`
// turns "discard this one file" into `git clean --force -- *` — and the split
// between `git restore` and `git clean` follows a status read here, on the
// server, at the moment the question is asked. A dialog composing its own line
// was showing one git never received, which is precisely the failure the rule
// exists to prevent.
//
// The status is read again when the discard itself arrives, so a file that
// became tracked in between is still handled by the right command. The plan is
// what the user is shown, never what the operation trusts.
func (s *Server) handleDiscardPlan(writer http.ResponseWriter, request *http.Request) {
	context, ok := s.readWorkTreeRequest(writer, request)
	if !ok {
		return
	}

	planned := context.discardCommands()
	commands := make([]string, 0, len(planned))
	for _, args := range planned {
		commands = append(commands, git.CommandLine(args))
	}

	writeJSON(writer, s.logger, http.StatusOK, discardPlanPayload{Commands: commands})
}

// worktreeContext is what every operation needs before it can run: where the
// repository is, what the user asked for, and what git says is there.
type worktreeContext struct {
	opened    *repo.Repo
	selection selection
	status    statusIndex
}

// dir is where git runs for this repository.
func (c worktreeContext) dir() string { return c.opened.Path }

func (c worktreeContext) onePath() string { return c.selection.Paths[0] }

// unstagedSide says which diff a path's unstaged change lives in.
//
// Which one depends on whether git has ever seen the file, and asking the
// status rather than the client keeps the interface from having to know. It
// lives here rather than in each handler because staging and discarding read
// the same side: while discard chose for itself it always read the tracked
// one, so every attempt to discard lines of a new file compared a fingerprint
// against a diff that could not match — a 409 blaming the user's editor for a
// file nothing had touched, which no retry could ever clear.
func (c worktreeContext) unstagedSide() git.DiffSide {
	if c.status.kindOf(c.onePath()) == git.EntryUntracked {
		return git.DiffUntracked
	}
	return git.DiffUnstaged
}

// discardSplit says which of the named paths each of the two commands takes.
//
// Two different commands, and the split has to be made from the status rather
// than guessed: `git restore` cannot remove a file git has never seen, and
// `git clean` would refuse a tracked one.
//
// One function because two callers need the same answer — the one that runs
// the commands and the one that shows them first — and a confirmation that
// sorted the files differently would name the wrong command for half of them.
func (c worktreeContext) discardSplit() (tracked, untracked []string) {
	for _, filePath := range c.selection.Paths {
		if c.status.kindOf(filePath) == git.EntryUntracked {
			untracked = append(untracked, filePath)
			continue
		}
		tracked = append(tracked, filePath)
	}
	return tracked, untracked
}

// discardCommands is what handleDiscard will run, in the order it runs them.
func (c worktreeContext) discardCommands() [][]string {
	if c.selection.Lines != nil {
		return [][]string{git.ApplyPatchArgs(git.ApplyToWorkTree, true)}
	}

	tracked, untracked := c.discardSplit()

	commands := make([][]string, 0, 2)
	if len(tracked) > 0 {
		commands = append(commands, git.DiscardTrackedArgs(tracked))
	}
	if len(untracked) > 0 {
		// Plural: `git clean` takes no pathspec file, so a long list becomes
		// several commands, and the plan shows every one of them.
		commands = append(commands, git.DiscardUntrackedBatches(untracked)...)
	}
	return commands
}

// errNotAFile: the path names a directory git will not look inside.
var errNotAFile = errors.New("this path is a directory git does not descend into")

// checkFiles refuses a selection naming something no operation here can act
// on.
//
// `git status --untracked-files=all` expands untracked directories into their
// files, with one exception: a directory holding its own `.git`. That arrives
// as a single entry, and `git clean --force` on it exits 0 having removed
// nothing, while `git restore` exits 1 with a pathspec error. Saying so is
// better than either — and removing somebody else's repository, which is what
// the flags that would make it work do, is not a thing to do on one click.
func (c worktreeContext) checkFiles() error {
	for _, filePath := range c.selection.Paths {
		if c.status.isDirectory(filePath) {
			return fmt.Errorf(
				"%w: %s is a repository of its own, so remove it outside yagit",
				errNotAFile, filePath)
		}
	}
	return nil
}

// statusIndex answers what the operations ask of the status: what git called
// this path, what it used to be called, and whether the branch has a commit
// yet.
type statusIndex struct {
	entries map[string]statusEntry
	unborn  bool
}

type statusEntry struct {
	// file is git's whole record for the path.
	//
	// It used to be the two fields the operations then needed, kind and
	// oldPath. Resolving a conflict needs a third — the pair of codes, which
	// is what says whether "ours" has any content to keep — and copying
	// fields across one at a time was going to keep happening. The zero value
	// still reads correctly for a path git did not list: an empty Kind is no
	// kind, which is what "not there" means everywhere below.
	file git.FileStatus

	// directory says git reported the path with a trailing slash, which it
	// does for the one thing `--untracked-files=all` will not descend into: a
	// nested checkout. Kept because none of the operations can act on one, and
	// the honest answer is a refusal rather than a command that exits 0 having
	// done nothing.
	directory bool
}

func (index statusIndex) kindOf(filePath string) git.EntryKind {
	return index.entries[filePath].file.Kind
}

func (index statusIndex) oldPathOf(filePath string) string {
	return index.entries[filePath].file.OldPath
}

// recordOf is git's whole listing for a path, or the zero value when git did
// not list it.
func (index statusIndex) recordOf(filePath string) git.FileStatus {
	return index.entries[filePath].file
}

func (index statusIndex) isDirectory(filePath string) bool {
	return index.entries[filePath].directory
}

// indexStatus keys the listing the way a request arrives.
//
// Both sides go through checkedPath, and they have to go through the same one:
// git reports a nested checkout as `nested/`, path.Clean turns the request for
// it into `nested`, and with two normalisations the lookup misses — the
// interface then offers `git clean --force` on a row the daemon runs `git
// restore` for. One normalisation is the whole fix.
func indexStatus(status git.Status) statusIndex {
	entries := make(map[string]statusEntry, len(status.Files))
	for _, file := range status.Files {
		key, err := checkedPath(file.Path)
		if err != nil {
			// git listed it, so it is inside the repository by construction;
			// a path this refuses is one no request could name either, and
			// leaving it out of the index is the same answer as never having
			// seen it.
			continue
		}
		entries[key] = statusEntry{
			file:      file,
			directory: strings.HasSuffix(file.Path, "/"),
		}
	}
	return statusIndex{entries: entries, unborn: status.Unborn}
}

// applyToWorkTree runs one operation and answers with the status that
// followed it.
//
// Answering with the new status rather than 204 is deliberate. Staging a file
// changes what every other row of the panel says about itself, and a client
// that had to ask again would draw one frame of the state it just left.
func (s *Server) applyToWorkTree(
	writer http.ResponseWriter,
	request *http.Request,
	operation func(worktreeContext) error,
) {
	context, ok := s.readWorkTreeRequest(writer, request)
	if !ok {
		return
	}

	if err := operation(context); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithStatus(writer, request, context.opened)
}

// readWorkTreeRequest resolves everything an operation needs before it can
// run, or answers the refusal itself and says so.
//
// Shared with the route that only reports what a discard would run: that route
// has to refuse exactly what the operation refuses — a bare repository, a path
// leaving the root, a nested checkout — or the interface would ask a question
// about a command the daemon was never going to accept.
func (s *Server) readWorkTreeRequest(
	writer http.ResponseWriter, request *http.Request,
) (worktreeContext, bool) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return worktreeContext{}, false
	}

	body, ok := s.readSelection(writer, request)
	if !ok {
		return worktreeContext{}, false
	}

	before, err := s.runner.Status(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return worktreeContext{}, false
	}

	context := worktreeContext{
		opened:    opened,
		selection: body,
		status:    indexStatus(before),
	}
	if err := context.checkFiles(); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return worktreeContext{}, false
	}

	return context, true
}

// errDiffMoved: the file changed between being drawn and being staged.
var errDiffMoved = errors.New("the file changed since this diff was read")

// resolveLines re-reads the diff and checks it is still the one the selection
// was made against.
//
// This is the guard that makes partial staging safe. A patch is only correct
// for the diff it was built from: if the file moved on in between — the user
// saved in their editor, a script rewrote it — the line numbers describe
// something else, and `git apply` would either refuse it or, worse, find
// somewhere it fits.
func (s *Server) resolveLines(
	request *http.Request, context worktreeContext, side git.DiffSide,
) (git.FileDiff, map[int]bool, error) {
	diff, err := s.runner.Diff(request.Context(), context.dir(), git.DiffRequest{
		Path:    context.onePath(),
		OldPath: context.status.oldPathOf(context.onePath()),
		Side:    side,
	})
	if err != nil {
		return git.FileDiff{}, nil, err
	}

	if diff.ID != context.selection.Lines.Diff {
		return git.FileDiff{}, nil, fmt.Errorf(
			"%w: read it again and choose the lines afresh", errDiffMoved)
	}

	selected := make(map[int]bool, len(context.selection.Lines.Indices))
	for _, index := range context.selection.Lines.Indices {
		selected[index] = true
	}
	return diff, selected, nil
}

type commitRequest struct {
	Message string `json:"message"`
	Amend   bool   `json:"amend"`
}

type commitPayload struct {
	SHA string `json:"sha"`
}

// handleCreateCommit records the index. Named for what it makes, beside
// handleCreateSession — handleCommit is the one that READS a commit, in
// history.go, and two handlers a letter apart is how a route ends up wired to
// the wrong one.
func (s *Server) handleCreateCommit(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body commitRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, s.logger, http.StatusBadRequest,
			fmt.Errorf("unreadable request body, a {\"message\": \"…\"} object is expected: %w", err))
		return
	}

	sha, err := s.runner.Commit(uninterrupted(request), opened.Path, git.CommitOptions{
		Message: body.Message,
		Amend:   body.Amend,
	})
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusCreated, commitPayload{SHA: sha})
}

// errBareRepository: there is no working directory to speak of.
var errBareRepository = errors.New("this repository has no work tree")

// workTree resolves the repository a route names and refuses a bare one.
//
// A bare repository has no file to stage and no file to discard. Every route
// in this file would otherwise hand git a command it answers with something
// obscure — `git status` in a bare repository fails on the missing work tree —
// and the interface would report a git failure where the honest answer is
// that the question does not apply.
func (s *Server) workTree(request *http.Request) (*repo.Repo, error) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		return nil, err
	}
	if opened.Bare {
		return nil, fmt.Errorf("%w: %s is bare, so there is nothing to stage or commit",
			errBareRepository, opened.Name)
	}
	return opened, nil
}

func statusForWorkTreeError(err error) int {
	if errors.Is(err, errBareRepository) {
		// The request is well formed and the repository exists; what is asked
		// for cannot apply to it.
		return http.StatusConflict
	}
	return statusForLookupError(err)
}

// statusForOperationError turns a refusal into an HTTP code.
//
// Only the cases the interface acts on differently are told apart. Everything
// else is a git failure, and a git failure travels whole — command, exit code,
// raw stderr — whatever number it is wrapped in.
func statusForOperationError(err error) int {
	switch {
	case errors.Is(err, errDiffMoved):
		// Not a bad request and not a server fault: the state on disk moved
		// under a request that was correct when it was made.
		return http.StatusConflict
	case errors.Is(err, git.ErrCombinedDiff):
		// Not a bad request: the path is real and the side is one of the
		// three. It is the repository that is in a state where the question
		// has no answer, which is what 409 is for — and what the interface
		// turns into an offer to resolve the conflict instead.
		return http.StatusConflict
	case errors.Is(err, errUnresolvedConflict):
		// The request is well formed and the path is real; the file is in a
		// state where staging it would record something nobody meant. The
		// same reading as the combined diff above, and the same code.
		return http.StatusConflict
	case errors.Is(err, git.ErrNoOperation),
		errors.Is(err, errOperationMoved),
		errors.Is(err, errHeadMoved),
		errors.Is(err, errPlanChanged),
		errors.Is(err, git.ErrPlanNotTheRange),
		errors.Is(err, errRepositoryBusy),
		errors.Is(err, git.ErrWriteInProgress):
		// Nothing is in progress, something else now is, HEAD has left the
		// branch the operation was aimed at, or the operation it described has
		// become a different one. All of them are the repository having moved
		// under a dialog drawn from a reading up to two seconds old, and all
		// of them are answered by reading it again — which is what the
		// interface does with a 409 here. Three of them are shared by every
		// operation on the branch HEAD is on; see branchop.go, which is what
		// keeps this arm from growing with each one. A rebase plan that is no
		// longer the commits after its base is the same fact said about a
		// list: somebody committed, or a commit left the branch, while the
		// plan was open.
		return http.StatusConflict
	case errors.Is(err, git.ErrUnrelatedHistories),
		errors.Is(err, git.ErrCherryPickMerge),
		errors.Is(err, git.ErrRevertMerge),
		errors.Is(err, git.ErrRevertRoot),
		errors.Is(err, git.ErrRevertNotOnBranch),
		errors.Is(err, git.ErrResetNotOnBranch),
		errors.Is(err, git.ErrRangeHoldsMerge),
		errors.Is(err, git.ErrRangeEmpty),
		errors.Is(err, git.ErrPlanTooLong),
		errors.Is(err, git.ErrNothingToUndo),
		errors.Is(err, git.ErrCannotUndoRoot),
		errors.Is(err, git.ErrUndoStale):
		// Two branches that were started separately, a merge commit offered
		// for cherry-pick or revert without a mainline, a root that has no
		// parent to invert against, a revert of a commit the branch never
		// held, a reset onto foreign history, or a range that cannot be
		// planned over — a merge commit inside it, nothing in it at all, or
		// more commits than one list can be read as. The request is well
		// formed and nothing moved; it is the repository that holds a shape
		// git will not act on without being told more, and no amount of
		// re-reading changes that — but it is still the state answering rather
		// than the client, which is what keeps it beside the others.
		return http.StatusConflict
	case errors.Is(err, git.ErrBranchIsBack),
		errors.Is(err, git.ErrDeletedWorkIsGone):
		// The undo yagit remembered has been overtaken: somebody made a
		// branch of that name again, or `git gc` collected the commits the
		// deleted one held. Beside the pair above rather than a 400, for the
		// same reason — the request was right when the dialog drew it, and it
		// is the repository that answers differently now. Nothing to re-read
		// into a working request, so the interface reports it and drops the
		// offer.
		return http.StatusConflict
	case errors.Is(err, git.ErrActionBlocked):
		// git would take the instruction and yagit will not give it: a rebase
		// with a `squash` still ahead of it needs an editor the daemon has
		// nowhere to show. The repository is what refuses, not the request, so
		// it reads as a conflict and the reason travels with it.
		return http.StatusConflict
	case errors.Is(err, git.ErrActionUnavailable):
		// The instruction is not one this operation takes: `git merge --skip`
		// does not exist. Told from the pair above because it is the request
		// that is wrong rather than the moment it arrived in.
		return http.StatusBadRequest
	case errors.Is(err, git.ErrNoStash),
		errors.Is(err, git.ErrStashMoved):
		// The stack is shorter than the list on screen said, or the position
		// the confirmation named has come to hold a different stash. Both are
		// the repository having moved under a dialog — a stash pushed or
		// dropped in another window renumbers every entry below it — and both
		// are answered by reading the list again, which is what the interface
		// does with a 409. Beside errPlanChanged above rather than folded into
		// it, because a stash's plan is checked against a POSITION rather than
		// against an object name; see the note at the top of stash.go.
		return http.StatusConflict
	case errors.Is(err, git.ErrDetachedHEAD),
		errors.Is(err, git.ErrNoUpstream),
		errors.Is(err, git.ErrNothingToStash),
		errors.Is(err, git.ErrNoCommits):
		// The request is well formed and the repository exists; it is in a
		// state where what was asked for has no meaning. A detached HEAD has
		// no branch to push, a branch that follows nothing has nowhere to pull
		// from, and a work tree that matches HEAD has nothing to stash — and
		// the interface answers each with an offer rather than with a message,
		// which is why they are told apart at all.
		return http.StatusConflict
	case errors.Is(err, git.ErrNoRemote),
		errors.Is(err, git.ErrNoRemoteURL),
		errors.Is(err, git.ErrNothingSelected),
		errors.Is(err, git.ErrNotLineAddressable),
		errors.Is(err, git.ErrNoPaths),
		errors.Is(err, git.ErrNoRef),
		errors.Is(err, git.ErrNoBranchName),
		errors.Is(err, git.ErrNoTagName),
		errors.Is(err, git.ErrNoTagMessage),
		errors.Is(err, git.ErrBadRevision),
		errors.Is(err, git.ErrStashMessageLine),
		errors.Is(err, errSameBranch),
		errors.Is(err, errNotAFile),
		errors.Is(err, git.ErrUnknownInstruction),
		errors.Is(err, git.ErrEmptyPlan),
		errors.Is(err, git.ErrCombineWithoutTarget),
		errors.Is(err, git.ErrEmptyMessage):
		// The body itself is the problem: a field left empty, a revision git
		// would read as an option, two fields that cannot both be true, or a
		// rebase plan that no reading of any repository could make sense of —
		// a verb yagit does not have, no steps at all, or a combine with
		// nothing above it to combine into.
		// Nothing was read from the repository to decide this, so reading it
		// again would answer the same way — which is what tells these from the
		// 409s above.
		return http.StatusBadRequest
	case errors.Is(err, git.ErrDiffTooLarge):
		// The request is correct and so is the daemon; what it asked for does
		// not fit in an answer. 413 is the code that says exactly that.
		return http.StatusRequestEntityTooLarge
	default:
		var gitError *git.Error
		if errors.As(err, &gitError) {
			// git refused. The command, its exit code and its stderr are on
			// the error and reach the interface intact; what the daemon adds
			// is that the fault is not the daemon's.
			return http.StatusUnprocessableEntity
		}
		return http.StatusInternalServerError
	}
}
