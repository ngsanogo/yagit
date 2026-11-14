package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/session"
)

// storedToken reads what the file holds, as `./do` and the suite would.
func storedToken(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Errorf("%s holds %q, want one line ending in a newline", path, raw)
	}
	return strings.TrimSpace(string(raw))
}

func TestACheckoutTokenIsMintedOnceAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".yagit", "session-token")

	first, err := checkoutToken(path, false)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !session.Minted(first) {
		t.Fatalf("minted %q, which is not what session.Mint produces", first)
	}
	if got := storedToken(t, path); got != first {
		t.Fatalf("file holds %q, answered %q", got, first)
	}
	// The file grants everything the daemon can do, on every platform, and
	// the directory beside it holds the stack's log of every repository path.
	if err := protect.Check(path); err != nil {
		t.Fatalf("the token file is not locked down: %v", err)
	}
	if err := protect.Check(filepath.Dir(path)); err != nil {
		t.Fatalf("the token's directory is not locked down: %v", err)
	}

	// Kept: a rebuild or a restart asks again and must get the same answer,
	// or the browser is logged out while somebody is writing code.
	again, err := checkoutToken(path, false)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if again != first {
		t.Fatalf("second read = %q, want the stored %q", again, first)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries, want the token alone: %v", len(entries), entries)
	}
}

func TestRenewingReplacesTheCheckoutToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-token")

	old, err := checkoutToken(path, false)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := checkoutToken(path, true)
	if err != nil {
		t.Fatalf("renewing: %v", err)
	}
	if renewed == old || !session.Minted(renewed) {
		t.Fatalf("renewed %q from %q, want a different minted token", renewed, old)
	}
	if got := storedToken(t, path); got != renewed {
		t.Fatalf("file holds %q after renewing, want %q", got, renewed)
	}
	if err := protect.Check(path); err != nil {
		t.Fatalf("the renewed file is not locked down: %v", err)
	}
}

// What is in the file but is not a token this program minted is replaced, not
// handed down: the daemon would refuse a wrapped token on every start, and
// accept a short one it should not.
func TestAStoredTokenThatIsNotMintedIsReplaced(t *testing.T) {
	minted, err := session.Mint()
	if err != nil {
		t.Fatal(err)
	}

	for name, content := range map[string]string{
		"a word":              "hunter2\n",
		"a token cut in two":  minted[:20] + "\n" + minted[20:] + "\n",
		"an empty file":       "",
		"a token with a tail": minted + "=\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session-token")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			token, err := checkoutToken(path, false)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if !session.Minted(token) {
				t.Fatalf("answered %q, which is not a minted token", token)
			}
			if got := storedToken(t, path); got != token {
				t.Fatalf("file holds %q, answered %q", got, token)
			}
		})
	}
}

// Two first reads racing on a fresh checkout — the daemon's environment and
// the end-to-end suite — must agree, or one of them runs on a token the other
// overwrote and every request it makes is refused.
func TestFirstMintsThatRaceAgreeOnOneToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".yagit", "session-token")

	const racers = 16
	tokens := make([]string, racers)
	errs := make([]error, racers)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := range racers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			tokens[i], errs[i] = checkoutToken(path, false)
		}()
	}
	start.Done()
	done.Wait()

	stored := storedToken(t, path)
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if tokens[i] != stored {
			t.Errorf("racer %d answered %q, the file holds %q", i, tokens[i], stored)
		}
	}
}

func TestTheCheckoutTokenModeIsRecognisedByItsExactShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-token")

	var out bytes.Buffer
	handled, err := runAsCheckoutToken([]string{"yagit", checkoutTokenFlag, path}, &out)
	if !handled || err != nil {
		t.Fatalf("handled = %v, err = %v; want the token printed", handled, err)
	}
	printed := strings.TrimSpace(out.String())
	if printed != storedToken(t, path) {
		t.Fatalf("printed %q, the file holds %q", printed, storedToken(t, path))
	}

	out.Reset()
	handled, err = runAsCheckoutToken([]string{"yagit", checkoutTokenFlag, path, "--new"}, &out)
	if !handled || err != nil {
		t.Fatalf("--new: handled = %v, err = %v", handled, err)
	}
	if renewed := strings.TrimSpace(out.String()); renewed == printed {
		t.Fatalf("--new printed the old token %q", renewed)
	}

	// Anything else is a daemon starting, and its flags are flag.Parse's to
	// refuse: the mode never guesses at a shape it was not given.
	for _, args := range [][]string{
		{"yagit"},
		{"yagit", "-root", path},
		{"yagit", checkoutTokenFlag},
		{"yagit", checkoutTokenFlag, path, "--renew"},
		{"yagit", checkoutTokenFlag, path, "--new", "extra"},
		{"yagit", "-addr", "127.0.0.1:7420", checkoutTokenFlag, path},
	} {
		if handled, _ := runAsCheckoutToken(args, &out); handled {
			t.Errorf("%q was handled as the checkout token mode", args)
		}
	}
}
