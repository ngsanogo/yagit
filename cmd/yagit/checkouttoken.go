package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/session"
)

// checkoutTokenFlag makes this program print the token a checkout's
// development stack runs on, minting it into the file the first time, and
// exit.
//
// `./do` keeps one token per checkout so that a rebuild, or a `./do down` and
// `./do up`, does not log the browser out. Minting it here rather than in a
// script of its own is what keeps the token's rules in one place: the same
// length and alphabet as a token the daemon mints for itself (internal/session),
// and the same owner-only lockdown on every platform, an ACL on Windows
// included (internal/protect). A second implementation had the first and not
// the second.
//
// Private like the editor flags of an interactive rebase: `./do token`, the
// development daemon's environment and the end-to-end suite call it, and
// nothing a person types.
const checkoutTokenFlag = "--print-checkout-token"

// renewTokenArgument replaces the stored token instead of reading it.
const renewTokenArgument = "--new"

// runAsCheckoutToken handles `--print-checkout-token <file> [--new]`.
//
// Recognised by the exact flag and the exact argument count, like
// git.RunAsEditor, so a daemon starting normally falls through untouched.
func runAsCheckoutToken(args []string, out io.Writer) (handled bool, err error) {
	switch {
	case len(args) == 3 && args[1] == checkoutTokenFlag:
		return true, printCheckoutToken(out, args[2], false)
	case len(args) == 4 && args[1] == checkoutTokenFlag && args[3] == renewTokenArgument:
		return true, printCheckoutToken(out, args[2], true)
	default:
		return false, nil
	}
}

func printCheckoutToken(out io.Writer, path string, renew bool) error {
	token, err := checkoutToken(path, renew)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, token)
	return err
}

// checkoutToken returns the token stored at path, minting one there when there
// is none, when what is there is not a token this program minted, or when
// renew asks for a new one.
//
// A token is written whole or not at all: into a file beside the destination,
// locked down, then moved into place. A first mint links rather than renames,
// so two of them racing — the daemon's environment and the test suite, both
// started on a fresh checkout — agree on whichever got there first instead of
// each running on a token the other overwrote.
func checkoutToken(path string, renew bool) (string, error) {
	// The directory is the checkout's runtime state, .yagit/: the token, and
	// the background stack's log of every git command and repository path. It
	// is locked down with the token, and on Windows what lands in it later
	// inherits the same owner-only entry.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s for the session token: %w", dir, err)
	}
	if err := protect.OwnerOnly(dir); err != nil {
		return "", err
	}

	if !renew {
		stored, err := os.ReadFile(path)
		switch {
		case err == nil:
			if token := strings.TrimSpace(string(stored)); session.Minted(token) {
				// Locked down again on every read: a file copied in by hand, or
				// one an older checkout left readable, is closed the next time
				// anything asks for it.
				if err := protect.OwnerOnly(path); err != nil {
					return "", err
				}
				return token, nil
			}
			// Anything else in the file — an edit, a truncated write, a token of
			// some other shape — is replaced rather than handed to a daemon that
			// would refuse it on every start.
			renew = true
		case errors.Is(err, fs.ErrNotExist):
		default:
			return "", fmt.Errorf("reading the session token at %s: %w", path, err)
		}
	}

	token, err := session.Mint()
	if err != nil {
		return "", err
	}

	staged, err := stageToken(dir, token)
	if err != nil {
		return "", err
	}
	// Whatever happens below, the staged copy goes: after a rename it no longer
	// exists, and after a link it is a second name nothing needs.
	defer func() {
		if err := os.Remove(staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "could not remove %s: %v\n", staged, err)
		}
	}()

	if renew {
		if err := os.Rename(staged, path); err != nil {
			return "", fmt.Errorf("replacing the session token at %s: %w", path, err)
		}
		return token, nil
	}

	err = os.Link(staged, path)
	switch {
	case err == nil:
		return token, nil
	case errors.Is(err, fs.ErrExist):
		// Another first mint got there between the read above and here. Its
		// token is the checkout's now.
		return checkoutToken(path, false)
	default:
		// A filesystem without hard links. Renamed instead, which gives up
		// only the race above.
		if err := os.Rename(staged, path); err != nil {
			return "", fmt.Errorf("writing the session token to %s: %w", path, err)
		}
		return token, nil
	}
}

// stageToken writes token into a new owner-only file in dir and returns its
// path.
func stageToken(dir, token string) (string, error) {
	file, err := os.CreateTemp(dir, ".session-token-*")
	if err != nil {
		return "", fmt.Errorf("creating the session token in %s: %w", dir, err)
	}
	staged := file.Name()
	_, writeErr := file.WriteString(token + "\n")
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", errors.Join(fmt.Errorf("writing the session token to %s: %w", staged, err),
			os.Remove(staged))
	}
	// CreateTemp already makes the file 0600 on Unix; on Windows the mode means
	// nothing and the ACL is what keeps the token to its owner.
	if err := protect.OwnerOnly(staged); err != nil {
		return "", errors.Join(err, os.Remove(staged))
	}
	return staged, nil
}
