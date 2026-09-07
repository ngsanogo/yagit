package main

import (
	"os"
	"testing"
	"time"
)

// TestFingerprintFollowsContentsAndNotTimestamps covers the reason this is a
// hash rather than a stat.
//
// `git checkout` and `git pull` rewrite the mtime of every file they touch,
// identical contents included. A fingerprint made of timestamps would call the
// frontend stale after every branch switch and charge a full `npm ci` for it —
// and would restart a perfectly healthy background stack while it was at it.
func TestFingerprintFollowsContentsAndNotTimestamps(t *testing.T) {
	p := newProject(t)
	lockfile := p.path("web", "package-lock.json")
	writeFile(t, lockfile, `{"one": true}`)

	before, err := p.fingerprint(frontendStamp)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(lockfile, future, future); err != nil {
		t.Fatalf("moving the mtime: %v", err)
	}

	touched, err := p.fingerprint(frontendStamp)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if touched != before {
		t.Error("a file whose contents did not change must keep its fingerprint")
	}

	writeFile(t, lockfile, `{"two": true}`)
	changed, err := p.fingerprint(frontendStamp)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	if changed == before {
		t.Error("a changed lockfile must change the fingerprint")
	}
}

// TestFingerprintTellsMissingFromEmpty guards the one collision worth having a
// test for: a source that is absent and a source that is there but empty are
// different states of the checkout.
func TestFingerprintTellsMissingFromEmpty(t *testing.T) {
	p := newProject(t)

	missing, err := p.fingerprint(frontendStamp)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	writeFile(t, p.path("web", "package-lock.json"), "")
	empty, err := p.fingerprint(frontendStamp)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	if missing == empty {
		t.Error("an absent source and an empty one must not hash the same")
	}
}

func TestStampIsCurrentOnlyAfterItIsRecorded(t *testing.T) {
	p := newProject(t)
	writeFile(t, p.path("web", "package-lock.json"), `{"one": true}`)

	if p.stampIsCurrent(frontendStamp) {
		t.Error("work that was never recorded must not count as done")
	}

	if err := p.recordStamp(frontendStamp); err != nil {
		t.Fatalf("recordStamp: %v", err)
	}
	if !p.stampIsCurrent(frontendStamp) {
		t.Error("work recorded for these sources must count as done")
	}

	writeFile(t, p.path("web", "package-lock.json"), `{"two": true}`)
	if p.stampIsCurrent(frontendStamp) {
		t.Error("a source that moved must make the stamp stale")
	}
}

// TestStackStampWatchesBothLockfiles: a pull that moves a Go module needs the
// daemon rebuilt against it, and air watches .go files rather than go.sum.
func TestStackStampWatchesBothLockfiles(t *testing.T) {
	p := newProject(t)
	writeFile(t, p.path("web", "package-lock.json"), "{}")
	writeFile(t, p.path("go.sum"), "one\n")

	if err := p.recordStamp(stackStamp); err != nil {
		t.Fatalf("recordStamp: %v", err)
	}

	writeFile(t, p.path("go.sum"), "two\n")
	if p.stampIsCurrent(stackStamp) {
		t.Error("a moved go.sum must make the stack stamp stale")
	}
}
