// Package assets holds the built frontend, embedded in the binary.
//
// This is what makes yagit a single executable: no assets directory to
// install alongside it, no path to configure.
//
// In development this package is unused: the daemon proxies to the Vite
// server, which serves the sources as-is with hot reload.
package assets

import (
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// dist is filled by `./do build`, which runs Vite before compiling.
//
// The `all:` prefix includes files starting with a dot, so the directory
// can hold a .gitkeep: without it the package would not compile on a
// freshly cloned repository, where the frontend has not been built yet.
//
//go:embed all:dist
var dist embed.FS

// ErrFrontendNotBuilt reports a binary compiled without a frontend.
var ErrFrontendNotBuilt = errors.New(
	"no frontend embedded in this binary; build it with ./do build")

// Handler serves the built frontend.
//
// Any unknown route returns index.html: this is a single-page application,
// and routing happens in the browser. /api requests never reach here; they
// are handled upstream.
func Handler(logger *slog.Logger) (http.Handler, error) {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, err
	}
	return handlerFor(root, logger)
}

// handlerFor is Handler with the filesystem passed in rather than embedded.
//
// The split exists so this can be tested. What the embedded copy contains
// depends on whether `./do build` has run, so a test against it would assert
// one thing on a developer's machine and another in CI — which is not a test,
// it is a coin toss. Against a synthetic filesystem the questions have
// answers: what happens to an unknown route, to a directory, to a path trying
// to climb out.
func handlerFor(root fs.FS, logger *slog.Logger) (http.Handler, error) {
	indexPage, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return nil, ErrFrontendNotBuilt
	}

	fileServer := http.FileServerFS(root)

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if exists(root, request.URL.Path) {
			fileServer.ServeHTTP(writer, request)
			return
		}

		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := writer.Write(indexPage); err != nil {
			// The client left mid-write; there is nobody left to answer,
			// but that does not make the failure go away.
			logger.Warn("index page write interrupted", "error", err)
		}
	}), nil
}

func exists(root fs.FS, requestPath string) bool {
	cleaned := strings.TrimPrefix(path.Clean("/"+requestPath), "/")
	if cleaned == "" {
		return false
	}

	info, err := fs.Stat(root, cleaned)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
