package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Making a repository where there was none.
//
// The other half of clone.go: both put a new repository on disk under
// YAGIT_ROOT, and the boundary check they share belongs to the registry
// rather than here. What differs is that this one asks a question a clone
// never has to — what the first branch is called — and that the answer is a
// setting on the user's machine.

// ErrEmptyInitPath: nowhere was given to make the repository.
var ErrEmptyInitPath = errors.New("no path given to make a repository in")

// ErrBadInitialBranch: the name offered for the first branch is not one.
var ErrBadInitialBranch = errors.New("that is not a usable branch name")

// DefaultInitialBranch is what yagit proposes when the machine has no
// `init.defaultBranch`.
//
// Not what git would do, and deliberately: git falls back to `master` while
// printing a hint that says it is going to stop. yagit has to put a name in a
// field either way, and the field is editable — so the proposal is the one the
// hint recommends, and the command on the confirmation shows exactly which
// name is about to be used.
const DefaultInitialBranch = "main"

// InitArgs is the command Init runs. Exported for the reason CloneArgs is: the
// line the user is shown and the line git receives have one definition between
// them.
//
// `--initial-branch` is always present, and that is ADR 0021 applied to the
// one command whose result depends on a config key more than any other. Left
// off, `git init` reads `init.defaultBranch` — so the same button makes `main`
// on one machine and `master` on the next, and the line in the log panel would
// name neither. Pinned, the line says which branch exists afterwards.
//
// The path lands after `--` so a directory named `-q` cannot become an option.
func InitArgs(path, branch string) []string {
	return []string{"init", "--initial-branch=" + branch, "--", path}
}

// Init makes an empty repository at path, with branch as its first branch.
//
// The destination's parent must already exist and path itself must not; those
// checks belong to the registry, which owns the root boundary.
func (r *Runner) Init(ctx context.Context, path, branch string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return ErrEmptyInitPath
	}
	branch = strings.TrimSpace(branch)
	if err := CheckInitialBranch(branch); err != nil {
		return err
	}

	_, err := r.Exec(ctx, Command{
		// Dir is empty on purpose: the destination is an argument, and a
		// working directory would only matter for a relative path — which the
		// registry has already refused.
		Args: InitArgs(path, branch),
	})
	return err
}

// DefaultBranchName is the name a new repository's first branch would get on
// this machine: `init.defaultBranch` where it is set, DefaultInitialBranch
// where it is not.
//
// Read rather than assumed, so somebody whose machine says `trunk` is offered
// `trunk`. `git config --get` exits 1 with no output for a key nobody set,
// which is not a failure to report — it is the answer, and it means the
// fallback.
func (r *Runner) DefaultBranchName(ctx context.Context) string {
	output, err := r.Exec(ctx, Command{Args: []string{"config", "--get", "init.defaultBranch"}})
	if err != nil {
		return DefaultInitialBranch
	}
	name := strings.TrimSpace(string(output))
	if err := CheckInitialBranch(name); err != nil {
		return DefaultInitialBranch
	}
	return name
}

// CheckInitialBranch refuses a name `git init` would not take.
//
// git checks it too, and answers "invalid branch name" AFTER creating the
// directory — leaving a path that now exists, so the second attempt fails with
// "destination already exists" and the user is told about a problem they do
// not have. Checked first, the directory is still absent when the refusal
// arrives.
//
// The rules here are the subset that matters: git's own check is
// `check-ref-format --branch`, which is a subprocess for a question about a
// string somebody is still typing.
func CheckInitialBranch(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: none was given", ErrBadInitialBranch)
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("%w: %q starts with a dash, which git reads as an option", ErrBadInitialBranch, name)
	case strings.HasPrefix(name, "/"), strings.HasSuffix(name, "/"), strings.Contains(name, "//"):
		return fmt.Errorf("%w: %q has an empty path component", ErrBadInitialBranch, name)
	case strings.HasSuffix(name, "."), strings.Contains(name, ".."), strings.Contains(name, "@{"):
		return fmt.Errorf("%w: %q holds a sequence git reserves", ErrBadInitialBranch, name)
	case name == "@":
		return fmt.Errorf("%w: %q is git's own shorthand for HEAD", ErrBadInitialBranch, name)
	case strings.ContainsAny(name, " \t\n~^:?*[\\\x7f"):
		return fmt.Errorf("%w: %q holds a character no branch name may", ErrBadInitialBranch, name)
	}
	for _, r := range name {
		if r < ' ' {
			return fmt.Errorf("%w: %q holds a control character", ErrBadInitialBranch, name)
		}
	}

	// Per COMPONENT, not per name, which is the whole reason this is a loop
	// and not two more HasPrefix lines. git applies both rules to every slash
	// separated part — `.hidden` and `feature/.hidden` are refused alike, and
	// so are `x.lock` and `x.lock/y` — and a check that only looked at the
	// ends of the whole string let `.hidden` through to git, which refuses it
	// AFTER making the directory. That is the exact failure this function
	// exists to prevent.
	for _, component := range strings.Split(name, "/") {
		switch {
		case strings.HasPrefix(component, "."):
			return fmt.Errorf("%w: %q has a part starting with a dot, which git reserves",
				ErrBadInitialBranch, name)
		case strings.HasSuffix(component, ".lock"):
			return fmt.Errorf("%w: %q has a part ending in .lock, which git uses for its own files",
				ErrBadInitialBranch, name)
		}
	}
	return nil
}
