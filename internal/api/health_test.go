package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// The health route is the one thing a user is asked for when a report cannot
// be reproduced, so what it carries is a contract rather than a convenience.

func readHealth(t *testing.T) map[string]any {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set("X-Yagit-Token", testToken)

	response := execute(testServer(t), request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("health is not JSON: %v (%s)", err, response.Body.String())
	}
	return payload
}

func TestHealthNamesTheMachineItIsRunningOn(t *testing.T) {
	payload := readHealth(t)

	if payload["status"] != "ok" {
		t.Errorf("status = %v, want ok", payload["status"])
	}

	// Half of what makes a report hard to reproduce is which platform it came
	// from, and the person reporting rarely thinks to say.
	if want := runtime.GOOS + "/" + runtime.GOARCH; payload["platform"] != want {
		t.Errorf("platform = %v, want %q", payload["platform"], want)
	}
}

func TestHealthNamesTheGitItDrives(t *testing.T) {
	payload := readHealth(t)

	// yagit is a front end over the git on the machine. A health answer that
	// leaves it out describes half the software that is running.
	version, reported := payload["git"].(string)
	failure, failed := payload["gitError"].(string)

	switch {
	case reported && version != "":
		// `2.39`, not `git version 2.39.5 (Apple Git-154)`: the vendor suffix
		// is noise, and the two numbers are what every floor is expressed in.
		if strings.Count(version, ".") != 1 {
			t.Errorf("git = %q, want major.minor", version)
		}
	case failed && failure != "":
		// A machine with no usable git is exactly the case this field exists
		// for, so it is a legitimate answer here rather than a failure.
		t.Logf("git could not be read, which the payload reports: %s", failure)
	default:
		t.Fatalf("health says nothing about git, neither a version nor a reason: %v", payload)
	}
}

func TestHealthNamesNothingPrivate(t *testing.T) {
	// The point of the route is that it can be pasted into a public bug report
	// by someone who did not read it first. A repository path, a branch name
	// or the token would all make that a mistake.
	body, err := json.Marshal(readHealth(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{testToken, "/home/", "C:\\Users"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("health leaks %q: %s", secret, body)
		}
	}
}
