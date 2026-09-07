package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/ngsanogo/yagit/internal/edit"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Editing a file, and the two ways out of a conflict.
//
// These routes exist because of one moment: a merge stops, a file has conflict
// markers in it, and every other screen in yagit can describe that file
// without being able to fix it. Sending the user to another editor to delete
// seven characters is the point at which a git client stops being one.
//
// Editing is not a git operation and does not pretend to be. Saving writes the
// file and stops there — it does not stage, because what is on disk and what
// is in the index are two different things everywhere else in this interface
// and this is no place to start blurring them.

// editLocation is where a repository's files may be read and written.
//
// Built here, from the registry's own answers, so the boundary is the same one
// every other route enforces: repo.Get has already re-checked the work tree
// and both git directories against YAGIT_ROOT by the time this is called.
func editLocation(opened *repo.Repo) edit.Location {
	return edit.Location{WorkTree: opened.Path, GitDirs: opened.GitDirs()}
}

// handleReadFile answers with a work-tree file's text.
//
// The file on DISK, not a blob from the index or from a commit. That is the
// one the user is looking at in their editor, the one a merge left conflict
// markers in, and the only one saving can put back.
func (s *Server) handleReadFile(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	filePath, err := checkedPath(request.URL.Query().Get("path"))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	file, err := edit.Read(editLocation(opened), filePath)
	if err != nil {
		writeError(writer, s.logger, statusForEditError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, file)
}

// maxSaveBody caps a save, and it is derived from the read cap rather than
// chosen.
//
// Two independent numbers is what this replaces: the pane opened anything
// under edit.MaxFileBytes and the body cap beside it was 64 KiB, so every file
// between the two rendered in the editor, took a resolution, and answered the
// save with a 413. The draft only ever existed in the browser, so the work
// went with it.
//
// Six times the file, because the body is the file as a JSON string and
// escaping is not free: a byte like 0x01 passes this package's text test and
// crosses the wire as `\u0001`, six characters. The rest is room for the path
// and the fingerprint. Anything the read route will hand out, this route can
// therefore take back.
const maxSaveBody = 6*edit.MaxFileBytes + 4<<10

type saveRequest struct {
	Path string `json:"path"`
	Text string `json:"text"`

	// Base is the fingerprint of the content the edit started from —
	// edit.File.Fingerprint, sent back unchanged.
	//
	// Required, and the same guard as a line selection's diff id: a save is
	// only correct for the content it was started from. During a merge — which
	// is when this pane is used most — a `git checkout --theirs` in another
	// window rewrites the file under the editor, and a save that did not check
	// would put the user's half-finished edit over the top of it.
	Base string `json:"base"`
}

// savePayload is the saved file AND the status that followed it.
//
// Two objects in one answer, because saving changes both and a client that had
// to ask for the second would draw one frame of the state it just left — the
// same reasoning as the four staging routes, which answer with the status for
// exactly this reason. The file comes back because its fingerprint has moved:
// without it the next save of the same buffer is refused as stale.
type savePayload struct {
	File   edit.File     `json:"file"`
	Status statusPayload `json:"status"`
}

func (s *Server) handleSaveFile(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body saveRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxSaveBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(writer, s.logger, http.StatusRequestEntityTooLarge, fmt.Errorf(
				"this file is larger than the %d bytes a request may carry; edit it outside yagit",
				maxSaveBody))
			return
		}
		writeError(writer, s.logger, http.StatusBadRequest, fmt.Errorf(
			"unreadable request body, a {\"path\": \"…\", \"text\": \"…\", \"base\": \"…\"} object is expected: %w", err))
		return
	}

	filePath, err := checkedPath(body.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	saved, err := edit.Save(editLocation(opened), filePath, body.Text, body.Base)
	if err != nil {
		writeError(writer, s.logger, statusForEditError(err), err)
		return
	}

	after, err := s.readStatus(request, opened)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, savePayload{File: saved, Status: after})
}

type resolveRequest struct {
	Paths []string `json:"paths"`

	// Side is "ours" or "theirs", in git's own sense of the words. See
	// git.ConflictSide — during a rebase they mean the opposite of what most
	// people expect, and the notes beside the buttons follow the operation
	// rather than renaming them here.
	Side git.ConflictSide `json:"side"`
}

// errNotConflicted: the path is not unmerged, so there is no side to take.
var errNotConflicted = errors.New("this path is not conflicted")

// handleResolve takes one side of a conflict whole.
//
// The third way out of a conflict, beside editing the file and staging the
// result. It is two commands — `git checkout --ours`, then `git add` — and
// both are needed: the first writes the file and only the second collapses the
// index stages that make git call it unmerged. Stopping after the first leaves
// a work tree that looks finished and a repository that still refuses to
// commit.
//
// The path that has no version on the chosen side is the interesting one.
// "Deleted by us" kept ours is not a checkout at all — `git checkout --ours`
// fails there with "does not have our version" — it is `git rm`, because our
// side of that conflict is the file not existing. Which of the two runs is
// read off git's own status codes rather than guessed.
func (s *Server) handleResolve(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body resolveRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, fmt.Errorf(
			"unreadable request body, a {\"paths\": [\"…\"], \"side\": \"ours\"} object is expected: %w", err))
		return
	}

	if body.Side != git.SideOurs && body.Side != git.SideTheirs {
		writeError(writer, s.logger, http.StatusBadRequest, fmt.Errorf(
			"side=%q is neither ours nor theirs", body.Side))
		return
	}
	if len(body.Paths) == 0 {
		writeError(writer, s.logger, http.StatusBadRequest,
			errors.New("paths is empty; name at least one conflicted file"))
		return
	}

	paths := make([]string, 0, len(body.Paths))
	for _, candidate := range body.Paths {
		checked, err := checkedPath(candidate)
		if err != nil {
			writeError(writer, s.logger, http.StatusBadRequest, err)
			return
		}
		paths = append(paths, checked)
	}

	before, err := s.runner.Status(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}
	index := indexStatus(before)

	// The two groups are worked out before anything runs, so that a selection
	// mixing them is one pass of each command rather than a command per file
	// — and so that a path that is not conflicted at all is refused before the
	// first of them has changed anything.
	var keep, remove []string
	for _, filePath := range paths {
		record := index.recordOf(filePath)
		if record.Kind != git.EntryUnmerged {
			writeError(writer, s.logger, http.StatusConflict, fmt.Errorf(
				"%w: git does not report %s as unmerged", errNotConflicted, filePath))
			return
		}
		if record.HasSide(body.Side) {
			keep = append(keep, filePath)
			continue
		}
		remove = append(remove, filePath)
	}

	if len(keep) > 0 {
		if err := s.runner.KeepSide(uninterrupted(request), opened.Path, keep, body.Side); err != nil {
			writeError(writer, s.logger, statusForOperationError(err), err)
			return
		}
	}
	if len(remove) > 0 {
		if err := s.runner.RemoveConflicted(uninterrupted(request), opened.Path, remove); err != nil {
			writeError(writer, s.logger, statusForOperationError(err), err)
			return
		}
	}

	s.answerWithStatus(writer, request, opened)
}

type preparedMessagePayload struct {
	Text string `json:"text"`

	// Source names the file the text came from, so the interface can say
	// whose words these are. Empty when git prepared nothing, which is the
	// ordinary case and not a failure.
	Source git.MessageSource `json:"source"`

	// Signing says `commit.gpgsign` is on, so the box can say so BEFORE the
	// button is pressed. Signing is the step that fails once everything else
	// has succeeded — a locked key, an agent that is not running — and a box
	// that never mentioned it turns that into "the button did not work".
	//
	// It rides on this route rather than on the status for the reason the
	// message does: the status is re-read every two seconds, and this is
	// asked when the question changes.
	Signing bool `json:"signing"`

	// SigningUnreadable is why the question above could not be answered,
	// empty when it was.
	//
	// A field rather than a failed request, and that is the whole point of it.
	// The message is what this route exists for — MERGE_MSG after a stopped
	// merge is the one thing on this screen a person cannot get back — and
	// `commit.gpgsign` spelled in a way git will not parse is no reason to
	// throw it away. So the failure travels beside the answer instead of
	// replacing it, and the box says it could not tell.
	//
	// Not swallowed, which the rule in this project forbids: it is on screen
	// and it is in the log.
	SigningUnreadable string `json:"signing_unreadable,omitempty"`
}

// handlePreparedMessage answers with the message git would start this commit
// from: what an amend would replace, or what a merge left behind.
//
// A route of its own rather than a field on the status, and the reason is the
// log panel. The status is re-read every two seconds; answering this runs a
// git command, and putting it on the status would print a line into the panel
// every two seconds for the whole of a merge — drowning the commands the user
// actually caused in ones they did not. This is asked when the question
// changes: an operation starts, or the amend box is ticked.
func (s *Server) handlePreparedMessage(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	// Amend is a different question with a different answer: the message of
	// the commit being replaced, not the one a merge left behind. It is the
	// client's to ask because it is a checkbox on the client's screen.
	amend := request.URL.Query().Get("amend") == "true"

	text, source, err := s.runner.PreparedMessage(
		request.Context(), opened.Path, opened.StateDir(), amend)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// A second read, and a cheap one: `git config --get`, no object database
	// touched. It answers the same question the message does — what the next
	// commit is going to be — and splitting it into its own route would be a
	// second request the box has to make before it can be drawn.
	//
	// Its failure does not fail the route, and that is a decision about which
	// of the two answers matters. The message above has already been read: it
	// is git's own MERGE_MSG or the commit an amend replaces, and during a
	// stopped merge it is not recoverable from anywhere else on this screen.
	// A `commit.gpgsign` git cannot parse must not take it down with it. The
	// git layer is right to let that error travel — WillSignCommits cannot
	// know what is being asked of it — and this is where it is caught.
	payload := preparedMessagePayload{Text: text, Source: source}

	// The text carries the command, its exit code and git's own stderr, which
	// is what makes it worth putting on screen rather than "something failed".
	if signing, err := s.runner.WillSignCommits(request.Context(), opened.Path); err != nil {
		payload.SigningUnreadable = err.Error()
		s.logger.Warn("cannot tell whether the next commit will be signed",
			"repository", opened.ID, "error", err)
	} else {
		payload.Signing = signing
	}

	writeJSON(writer, s.logger, http.StatusOK, payload)
}

// statusForEditError turns a refusal from the edit package into an HTTP code.
//
// Every case here is one the interface says something different about. A file
// that is too large earns an offer to stage it whole; one that moved earns
// "read it again"; one inside the git directory earns a flat no.
func statusForEditError(err error) int {
	switch {
	case errors.Is(err, edit.ErrFileMoved):
		// The request was correct when it was made and the disk moved under
		// it. Neither side is at fault, which is what 409 says.
		return http.StatusConflict
	case errors.Is(err, edit.ErrOutsideWorkTree), errors.Is(err, edit.ErrInsideGitDir):
		return http.StatusForbidden
	case errors.Is(err, edit.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, edit.ErrBinary), errors.Is(err, edit.ErrNotRegular):
		// The path exists and the request is well formed; what is asked for
		// does not apply to it.
		return http.StatusConflict
	case errors.Is(err, fs.ErrNotExist):
		// A file the status listed and the disk no longer has: staged from
		// another window, or removed by a checkout between the click and the
		// read.
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
