package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// A stamp records that some work was done for a particular version of the
// files that decide it: the frontend install for a lockfile, the running stack
// for the dependencies it was started with.
//
// The sources' CONTENTS decide, never their timestamps. `git checkout` and
// `git pull` rewrite an mtime without changing a byte — so a stamp that
// followed the mtime would reinstall node_modules, and restart a healthy
// stack, on every branch switch. Hashing two files costs a millisecond.
type stamp struct {
	// file holds the fingerprint, relative to the checkout.
	file string

	// sources are the files whose contents the work depends on.
	sources []string
}

var (
	// frontendStamp: web/node_modules was installed from this lockfile.
	frontendStamp = stamp{
		file:    stateDirectory + "/frontend.stamp",
		sources: []string{"web/package-lock.json"},
	}

	// stackStamp: the background stack is running the dependencies it was
	// started with.
	//
	// go.sum as well as the lockfile: a pull that moves a Go module needs the
	// daemon rebuilt against it, and air watches .go files rather than the
	// module graph. mise.lock is deliberately absent — the shim runs `mise
	// install` on every invocation, so its mtime moves constantly and its
	// contents change nothing about a stack that is already up.
	stackStamp = stamp{
		file:    stateDirectory + "/stack.stamp",
		sources: []string{"web/package-lock.json", "go.sum"},
	}
)

// fingerprint hashes the stamp's sources.
//
// A missing source is a fingerprint, not an error: a checkout with no go.sum
// is a checkout with no Go dependencies. It is recorded as missing rather than
// as nothing, so that adding an empty file is still a change.
func (p *project) fingerprint(s stamp) (string, error) {
	sum := sha256.New()

	for _, relative := range s.sources {
		contents, err := os.ReadFile(p.path(relative))
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("reading %s: %w", relative, err)
		}

		state := "present"
		if err != nil {
			state = "missing"
		}

		// The name, the state and the length go into the hash ahead of the
		// bytes, so that no two different source sets can hash the same — an
		// absent file and an empty one included.
		header := fmt.Sprintf("%s %s %d\n", relative, state, len(contents))
		sum.Write([]byte(header))
		sum.Write(contents)
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// stampIsCurrent reports whether the work this stamp records was done for the
// sources as they are now.
//
// A stamp it cannot compute or read is reported as stale, which sends the
// caller to do the work again — the safe direction, and the one where npm or
// git explains what is wrong with the checkout far better than a guess here
// would.
func (p *project) stampIsCurrent(s stamp) bool {
	current, err := p.fingerprint(s)
	if err != nil {
		warn("checking %s: %s", s.file, err)
		return false
	}

	saved, err := os.ReadFile(p.path(s.file))
	if err != nil {
		if !os.IsNotExist(err) {
			warn("reading %s: %s", s.file, err)
		}
		return false
	}

	return strings.TrimSpace(string(saved)) == current
}

// recordStamp writes the fingerprint of the sources as they are now. It is
// called after the work succeeded, never before: a stamp written first would
// claim an install that a failed `npm ci` left half-finished.
func (p *project) recordStamp(s stamp) error {
	fingerprint, err := p.fingerprint(s)
	if err != nil {
		return err
	}
	return p.writeStateFile(s.file, []byte(fingerprint+"\n"))
}
