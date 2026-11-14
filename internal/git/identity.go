package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The committer identity in the user's global git configuration, and the
// pull.rebase setting beside it — read and written for the preferences
// screen. pull.rebase is shown only, because changing how pull behaves from a
// quiet preferences pane would surprise a pull button that already names
// three strategies.

// Identity is what the global configuration says about who commits, and how
// pull.rebase is set there.
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`

	// PullRebase is the raw value of pull.rebase when set: "true", "false",
	// "merges", "interactive", or whatever the user wrote. Empty means unset.
	PullRebase string `json:"pull_rebase"`
}

// ErrBadIdentity: a name or an email that was blank or spanned lines.
var ErrBadIdentity = errors.New("user.name and user.email must each be one non-empty line")

// globalScope is where these keys are read and written, spelled once so the
// two directions cannot disagree.
//
// Global, because the screen is about who THIS machine commits as rather than
// one repository's override — and read from the same place it writes, because
// the daemon has no repository to stand in here: a plain `git config --get`
// resolves against the daemon's working directory, which under `./do up` is a
// checkout with configuration of its own, and the form would show one value
// and save another. --includes, because --global alone stops following
// include.path, and an identity kept in an included file is common.
var globalScope = []string{"config", "--global", "--includes"}

// ReadIdentity reads user.name, user.email and pull.rebase from the global
// configuration.
//
// Unset keys are empty strings rather than errors: `git config --get` exits 1
// with an empty stderr for a key nobody set, which is the ordinary case on a
// fresh profile and what the preferences form has to show as blank.
func (r *Runner) ReadIdentity(ctx context.Context) (Identity, error) {
	name, err := r.globalValue(ctx, "user.name")
	if err != nil {
		return Identity{}, err
	}
	email, err := r.globalValue(ctx, "user.email")
	if err != nil {
		return Identity{}, err
	}
	pullRebase, err := r.globalValue(ctx, "pull.rebase")
	if err != nil {
		return Identity{}, err
	}
	return Identity{Name: name, Email: email, PullRebase: pullRebase}, nil
}

// setIdentityArgs are the two commands a save runs.
func setIdentityArgs(name, email string) [][]string {
	return [][]string{
		append(append([]string{}, globalScope[:2]...), "user.name", name),
		append(append([]string{}, globalScope[:2]...), "user.email", email),
	}
}

// SetIdentity writes user.name and user.email into the global configuration.
//
// Empty values are refused — unsetting identity is a different operation and
// not what the form offers — and so is a newline, which git would store and
// then write into the header of every commit.
func (r *Runner) SetIdentity(ctx context.Context, name, email string) error {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" || email == "" || strings.ContainsAny(name+email, "\n\r") {
		return ErrBadIdentity
	}

	for _, args := range setIdentityArgs(name, email) {
		if _, err := r.Exec(ctx, Command{Args: args}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) globalValue(ctx context.Context, key string) (string, error) {
	args := append(append([]string{}, globalScope...), "--get", key)
	output, err := r.Exec(ctx, Command{Args: args})
	if err != nil {
		var failure *Error
		if errors.As(err, &failure) && failure.ExitCode == 1 && strings.TrimSpace(failure.Stderr) == "" {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", key, err)
	}
	return strings.TrimSpace(string(output)), nil
}
