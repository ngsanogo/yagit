package api

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/ngsanogo/yagit/internal/edit"
	"github.com/ngsanogo/yagit/internal/git"
)

// Image previews for a binary FileDiff.
//
// When git marks a path binary and the name is an image, the interface draws
// the two sides rather than the "Binary file" notice. The bytes come from
// here — work tree, index, HEAD, or a revision — with the same path checks
// every other file route holds, and a size cap so a huge asset never lands
// in the daemon's memory.

// handleBlob answers with the raw bytes of one path at one place.
//
// Query: path (repository-relative), at (worktree | index | head | <revision>).
// Only image extensions are served: this route exists for a side-by-side
// preview, not as a general file download.
func (s *Server) handleBlob(writer http.ResponseWriter, request *http.Request) {
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

	contentType, ok := imageContentType(filePath)
	if !ok {
		writeError(writer, s.logger, http.StatusBadRequest, fmt.Errorf(
			"%q is not an image this preview serves (png, jpg, gif, webp, svg)", filePath))
		return
	}

	at := strings.TrimSpace(query.Get("at"))
	if at == "" {
		writeError(writer, s.logger, http.StatusBadRequest, errors.New(
			"at is required: worktree, index, head, or a revision"))
		return
	}

	var body []byte
	if strings.EqualFold(at, "worktree") {
		body, err = edit.ReadBytes(editLocation(opened), filePath, git.MaxBlobBytes)
		if err != nil {
			writeError(writer, s.logger, statusForBlobError(err), err)
			return
		}
	} else {
		body, err = s.runner.ShowBlob(request.Context(), opened.Path, at, filePath)
		if err != nil {
			writeError(writer, s.logger, statusForBlobError(err), err)
			return
		}
	}

	writer.Header().Set("Content-Type", contentType)
	// A preview is re-read when the diff fingerprint moves; caching by URL
	// alone would keep a stale work-tree image after the next edit.
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write(body); err != nil {
		s.logger.Warn("blob response interrupted",
			"repository", opened.ID, "path", filePath, "error", err)
	}
}

// imageContentType says whether path is an image this route will serve, and
// which Content-Type the browser needs. Extension only: git's binary flag is
// the other half of the decision, made by the caller before asking.
func imageContentType(filePath string) (string, bool) {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".svg":
		// Served only into <img>, where browsers do not run script. object /
		// embed / inline SVG would be a different surface and are not used.
		return "image/svg+xml", true
	default:
		return "", false
	}
}

// statusForBlobError maps a refusal from edit or git into an HTTP code.
func statusForBlobError(err error) int {
	switch {
	case errors.Is(err, edit.ErrTooLarge), errors.Is(err, git.ErrDiffTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, edit.ErrOutsideWorkTree), errors.Is(err, edit.ErrInsideGitDir):
		return http.StatusForbidden
	case errors.Is(err, edit.ErrNotRegular):
		return http.StatusConflict
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, git.ErrBadRevision), errors.Is(err, git.ErrNoPaths):
		return http.StatusBadRequest
	default:
		return statusForOperationError(err)
	}
}
