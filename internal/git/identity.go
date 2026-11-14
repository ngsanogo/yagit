package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The committer identity git will use, and the pull.rebase setting — read for
// the preferences screen. user.name and user.email are written globally;
// pull.rebase is shown only, because changing how pull behaves from a quiet
// preferences pane would surprise a pull button that already names three
// strategies.

// Identity is what git would put on the next commit, and how pull.rebase is set.
type Identity struct {
	Name  string `json:"name"`
	Email string `json:"email"`

	// PullRebase is the raw value of pull.rebase when set: "true", "false",
	// "merges", "interactive", or whatever the user wrote. Empty means unset.
	PullRebase string `json:"pull_rebase"`
}

// ErrEmptyIdentity: name or email was blank or held a newline.
var ErrEmptyIdentity = errors.New("identity is incomplete")

// ReadIdentity reads user.name, user.email and pull.rebase for this machine.
//
// Unset keys are empty strings rather than errors: `git config --get` exits 1
// with an empty stderr for a key nobody set, which is the ordinary case on a
// fresh profile and what the preferences form has to show as blank.
func (r *Runner) ReadIdentity(ctx context.Context) (Identity, error) {
	name, err := r.configGet(ctx, "user.name")
	if err != nil {
		return Identity{}, err
	}
	email, err := r.configGet(ctx, "user.email")
	if err != nil {
		return Identity{}, err
	}
	pullRebase, err := r.configGet(ctx, "pull.rebase")
	if err != nil {
		return Identity{}, err
	}
	return Identity{Name: name, Email: email, PullRebase: pullRebase}, nil
}

// SetIdentity writes user.name and user.email into the global config.
//
// Global rather than local: the preferences screen is about who THIS machine
// commits as, not about one repository's override. Empty values are refused —
// unsetting identity is a different operation and not what the form offers.
func (r *Runner) SetIdentity(ctx context.Context, name, email string) error {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		return fmt.Errorf("%w: user.name is empty", ErrEmptyIdentity)
	}
	if email == "" {
		return fmt.Errorf("%w: user.email is empty", ErrEmptyIdentity)
	}
	if strings.ContainsAny(name, "\n\r") || strings.ContainsAny(email, "\n\r") {
		return fmt.Errorf("%w: name and email must be a single line", ErrEmptyIdentity)
	}

	if _, err := r.Exec(ctx, Command{
		Args: []string{"config", "--global", "user.name", name},
	}); err != nil {
		return err
	}
	if _, err := r.Exec(ctx, Command{
		Args: []string{"config", "--global", "user.email", email},
	}); err != nil {
		return err
	}
	return nil
}

func (r *Runner) configGet(ctx context.Context, key string) (string, error) {
	output, err := r.Exec(ctx, Command{Args: []string{"config", "--get", key}})
	if err != nil {
		var failure *Error
		if errors.As(err, &failure) && failure.ExitCode == 1 && strings.TrimSpace(failure.Stderr) == "" {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
