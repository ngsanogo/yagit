package api

import (
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
)

// Tags over HTTP: create (annotated or lightweight), local delete, and push
// to a chosen remote.
//
// Push is its own pair of routes rather than the branch push path: that one
// writes refs/heads/… and must never resolve a short name into a tag.

type createTagRequest struct {
	// Name is the tag to create. It reaches git after `--`.
	Name string `json:"name"`

	// Message is required when Annotated is true (the default). Lightweight
	// tags carry none.
	Message string `json:"message"`

	// Target is any revision git accepts. Absent means HEAD.
	Target string `json:"target"`

	// Annotated defaults to true when omitted, so a body that only names a
	// message keeps creating what it always created. Inferring lightweight
	// from an empty message would turn a forgotten field into a different
	// kind of tag.
	Annotated *bool `json:"annotated"`
}

type deleteTagRequest struct {
	Name string `json:"name"`
}

type pushTagRequest struct {
	// Name is the local tag to send.
	Name string `json:"name"`

	// Remote is where it goes. Required — tags do not follow an upstream.
	Remote string `json:"remote"`
}

// tagPushPlan is what pushing a tag would run, before it runs.
type tagPushPlan struct {
	Command string `json:"command"`
	Remote  string `json:"remote"`
	Name    string `json:"name"`
	Ref     string `json:"ref"`
}

// handleCreateTag records a tag — annotated by default, lightweight when asked.
func (s *Server) handleCreateTag(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body createTagRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…", "message": "…", "annotated": true}`) {
		return
	}

	annotated := true
	if body.Annotated != nil {
		annotated = *body.Annotated
	}

	if err := s.runner.CreateTag(
		uninterrupted(request), opened.Path, body.Name, body.Message, body.Target, annotated,
	); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// handlePlanDeleteTag says what deleting would run, without running it.
func (s *Server) handlePlanDeleteTag(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body deleteTagRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoTagName)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.DeleteTagArgs(strings.TrimSpace(body.Name))),
	})
}

// handleDeleteTag removes a local tag.
func (s *Server) handleDeleteTag(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body deleteTagRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…"}`) {
		return
	}

	if err := s.runner.DeleteTag(uninterrupted(request), opened.Path, body.Name); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

// handlePlanPushTag says what pushing a tag would run, without running it.
func (s *Server) handlePlanPushTag(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body pushTagRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…", "remote": "…"}`) {
		return
	}

	name, remote := strings.TrimSpace(body.Name), strings.TrimSpace(body.Remote)
	if name == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoTagName)
		return
	}
	if remote == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrNoRemote)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, tagPushPlan{
		Command: git.CommandLine(git.PushTagArgs(remote, name)),
		Remote:  remote,
		Name:    name,
		Ref:     git.TagRef(name),
	})
}

// handlePushTag sends a local tag to a remote under the same name.
func (s *Server) handlePushTag(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body pushTagRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name": "…", "remote": "…"}`) {
		return
	}

	if err := s.runner.PushTag(uninterrupted(request), opened.Path, body.Remote, body.Name); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The local refs list is unchanged — the tag was already here. An empty
	// 204 would leave the client guessing; the refs answer is still the shape
	// every other tag route uses, and costs one for-each-ref.
	s.answerWithRefs(writer, request, opened)
}
