package assets

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// This package is what makes yagit one executable, and its whole job is to
// answer requests the daemon does not. Two of its behaviours are the kind that
// break quietly: an unknown route has to return the application rather than a
// 404, and a directory has to do the same rather than a listing of what is
// inside the binary.

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// builtFrontend is what Vite leaves behind: an entry page and hashed assets.
func builtFrontend() fstest.MapFS {
	return fstest.MapFS{
		"index.html":                &fstest.MapFile{Data: []byte("<!doctype html><title>yagit</title>")},
		"assets/index-CK3VP8Sy.css": &fstest.MapFile{Data: []byte(":root{color:red}")},
		"assets/index-Dep-ed4z.js":  &fstest.MapFile{Data: []byte("console.log(1)")},
	}
}

func serve(t *testing.T, root fstest.MapFS, target string) *httptest.ResponseRecorder {
	t.Helper()

	handler, err := handlerFor(root, discard())
	if err != nil {
		t.Fatalf("handlerFor: %v", err)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

// TestABinaryWithNoFrontendSaysSo covers the freshly cloned checkout, where
// dist/ holds nothing but the .gitkeep that lets //go:embed compile at all.
//
// Failing here is the point. A daemon that starts and serves a blank page is a
// bug report about the frontend; a daemon that refuses to start names the real
// problem at the only moment anyone is watching.
func TestABinaryWithNoFrontendSaysSo(t *testing.T) {
	_, err := handlerFor(fstest.MapFS{".gitkeep": &fstest.MapFile{}}, discard())

	if !errors.Is(err, ErrFrontendNotBuilt) {
		t.Fatalf("err = %v, want ErrFrontendNotBuilt", err)
	}
	// The message has to carry the fix. "not built" alone sends the reader to
	// the source to find out what builds it.
	if !strings.Contains(err.Error(), "./do build") {
		t.Errorf("the error must name the command that fixes it, got: %v", err)
	}
}

func TestABuiltAssetIsServedAsItself(t *testing.T) {
	response := serve(t, builtFrontend(), "/assets/index-Dep-ed4z.js")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if body := response.Body.String(); body != "console.log(1)" {
		t.Errorf("body = %q, want the asset itself", body)
	}
}

// TestAnUnknownRouteReturnsTheApplication is the single-page rule: routing
// happens in the browser, so a deep link the server has never heard of still
// has to load the application, which then reads the URL and shows the right
// screen.
func TestAnUnknownRouteReturnsTheApplication(t *testing.T) {
	for _, target := range []string{
		"/repositories/abc123",
		"/repositories/abc123/commits/deadbeef",
		"/settings",
	} {
		response := serve(t, builtFrontend(), target)

		if response.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", target, response.Code)
		}
		if !strings.Contains(response.Body.String(), "<title>yagit</title>") {
			t.Errorf("%s: did not return the application", target)
		}
		if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
			t.Errorf("%s: Content-Type = %q", target, contentType)
		}
	}
}

// TestADirectoryIsNotListed covers the reason `exists` refuses a directory
// rather than letting the file server answer.
//
// http.FileServerFS renders a directory as an index of what it contains. Here
// that would publish the name of every file compiled into the binary, to
// anyone who guesses /assets/ — and it is not even useful, since the client
// asking for a path is asking for a page.
func TestADirectoryIsNotListed(t *testing.T) {
	for _, target := range []string{"/assets", "/assets/", "/"} {
		response := serve(t, builtFrontend(), target)

		if strings.Contains(response.Body.String(), "index-Dep-ed4z.js") {
			t.Errorf("%s: the binary's contents were listed", target)
		}
		if !strings.Contains(response.Body.String(), "<title>yagit</title>") {
			t.Errorf("%s: want the application, got %q", target, response.Body.String())
		}
	}
}

// TestAPathCannotClimbOutOfTheBundle is the boundary this handler has.
//
// Nothing outside the embedded filesystem is reachable through an fs.FS in the
// first place — that is the guarantee of embedding rather than serving a
// directory. This asserts the guarantee holds through the cleaning that
// happens on the way in, so that a later change to `exists` cannot quietly
// turn a traversal into a file read.
func TestAPathCannotClimbOutOfTheBundle(t *testing.T) {
	root := builtFrontend()
	root["../outside-the-bundle"] = &fstest.MapFile{Data: []byte("SECRET")}

	for _, target := range []string{
		"/../outside-the-bundle",
		"/assets/../../outside-the-bundle",
		"/%2e%2e/outside-the-bundle",
	} {
		response := serve(t, root, target)

		if strings.Contains(response.Body.String(), "SECRET") {
			t.Errorf("%s: served something from outside the bundle", target)
		}
	}
}
