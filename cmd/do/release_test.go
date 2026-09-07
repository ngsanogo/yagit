package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseBinaryPathRequiresABuild(t *testing.T) {
	project := newProject(t)

	_, err := project.releaseBinaryPath()
	if err == nil {
		t.Fatal("expected an error when dist/ has no binary")
	}
	if !strings.Contains(err.Error(), "run ./do build first") {
		t.Errorf("error = %q, want it to name ./do build", err)
	}
}

func TestReleaseBinaryPathFindsTheLocalTarget(t *testing.T) {
	project := newProject(t)

	name := "yagit-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	dist := project.path("dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatalf("creating dist/: %v", err)
	}

	binary := filepath.Join(dist, name)
	writeFile(t, binary, "#!/bin/sh\n")

	got, err := project.releaseBinaryPath()
	if err != nil {
		t.Fatalf("releaseBinaryPath: %v", err)
	}
	if got != binary {
		t.Errorf("releaseBinaryPath() = %q, want %q", got, binary)
	}
}

func TestProbeAtUsesTheBaseURLItIsGiven(t *testing.T) {
	var seen string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		seen = request.URL.Path
		if request.Header.Get("X-Yagit-Token") != "probe-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	project := newProject(t)
	if !project.probeAt(server.URL, "probe-token", "/api/health", 0) {
		t.Fatal("probeAt must succeed against the server it was aimed at")
	}
	if seen != "/api/health" {
		t.Errorf("path probed = %q, want /api/health", seen)
	}
}

func TestProbeReleaseFrontendNeedsTheEmbeddedApplication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Yagit-Token") != "probe-token" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("<!doctype html><title>Not yagit</title>"))
	}))
	defer server.Close()

	project := newProject(t)
	if project.probeReleaseFrontend(server.URL, "probe-token", 0) {
		t.Fatal("a page without the yagit title must not count as the embedded frontend")
	}
}

func TestFreePortReturnsSomethingListening(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if port <= 0 {
		t.Fatalf("freePort() = %d, want a positive port", port)
	}
}
