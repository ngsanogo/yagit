package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Submodules: another repository pinned inside this one at one commit.
//
// The list is assembled from three machine-readable readings rather than from
// `git submodule status`, and that is the decision this file rests on. That
// command prints `<char><sha> <path> (<describe>)` — a format written for a
// terminal, where a path holding a space is already ambiguous and a path
// holding a newline is a second entry. What is read instead:
//
//  1. `git ls-files -s -z`, filtered to mode 160000. That is git's own record
//     of which paths are submodules and at which commit, NUL-separated, and it
//     is the authority: a gitlink can exist with no entry in .gitmodules.
//  2. `.gitmodules`, through `git config -f … -z`, for the URL each declares.
//  3. `git config --get submodule.<name>.url` in the superproject, which is
//     git's own definition of "initialised" — `git submodule init` is what
//     writes it — and exits 1 when nobody has, which is an answer and not a
//     failure.
//
// Where the checkout stands is then one `rev-parse` per initialised submodule.
// A handful of subprocesses for a handful of directories, and every one of
// them exact.

// ErrNoSubmodulePath: nothing was named.
var ErrNoSubmodulePath = errors.New("no submodule path given")

// ErrEmptySubmoduleURL: nothing was given to add from.
var ErrEmptySubmoduleURL = errors.New("no URL given for the submodule")

// gitlinkMode is the file mode git records for a submodule in the index. It is
// the only thing that tells one from a file or a directory there.
const gitlinkMode = "160000"

// Submodule is one repository pinned inside another.
type Submodule struct {
	// Name is the section name in .gitmodules, which is usually the path but
	// need not be — and it is the name every `git config submodule.<name>.…`
	// key is written under.
	Name string `json:"name"`

	// Path is where it sits in the superproject's work tree.
	Path string `json:"path"`

	// URL is what .gitmodules declares, redacted the way a remote list is: it
	// is a URL that may carry a token, and this one is drawn on a screen.
	URL string `json:"url"`

	// Recorded is the commit the superproject pins — the gitlink in the index.
	Recorded string `json:"recorded"`

	// HEAD is the commit checked out inside it, empty when there is no
	// checkout there yet.
	HEAD string `json:"head"`

	// Initialised says `git submodule init` has run: the URL is in the
	// superproject's own config, so an update knows where to clone from.
	Initialised bool `json:"initialised"`

	// Present says there is a checkout on disk at Path.
	Present bool `json:"present"`

	// Moved says the checkout is at a commit other than the one the
	// superproject records — the state `git submodule status` marks with `+`,
	// and the one that becomes a staged change in the superproject.
	Moved bool `json:"moved"`

	// Declared says .gitmodules has a section for it. A gitlink without one is
	// a real state — somebody committed a submodule and not the file that
	// describes it — and it is why the list is built from the index rather
	// than from .gitmodules.
	Declared bool `json:"declared"`
}

// Submodules lists what this repository pins, in index order.
func (r *Runner) Submodules(ctx context.Context, dir string) ([]Submodule, error) {
	gitlinks, err := r.gitlinks(ctx, dir)
	if err != nil {
		return nil, err
	}
	if len(gitlinks) == 0 {
		return []Submodule{}, nil
	}

	declared, err := r.declaredSubmodules(ctx, dir)
	if err != nil {
		return nil, err
	}

	initialised, err := r.initialisedSubmodules(ctx, dir)
	if err != nil {
		return nil, err
	}

	submodules := make([]Submodule, 0, len(gitlinks))
	for _, link := range gitlinks {
		submodule := Submodule{Path: link.path, Recorded: link.sha, Name: link.path}
		if named, ok := declared[link.path]; ok {
			submodule.Name = named.name
			submodule.URL = RedactURL(named.url)
			submodule.Declared = true
		}

		submodule.Initialised = initialised[submodule.Name]

		if head, ok := r.submoduleHEAD(ctx, joinPath(dir, link.path)); ok {
			submodule.HEAD = head
			submodule.Present = true
			submodule.Moved = head != link.sha
		}

		submodules = append(submodules, submodule)
	}
	return submodules, nil
}

// submoduleHEAD is the commit checked out inside a submodule, and whether
// there is a checkout there at all.
//
// A plain `rev-parse HEAD` in that directory is not enough, and the way it
// fails is the worst kind: a submodule that has never been updated is an EMPTY
// DIRECTORY inside the superproject's work tree, so git's upward search finds
// the SUPERPROJECT and answers with its HEAD. The panel would then show a
// submodule checked out at a commit from another repository entirely, and mark
// it "moved" for good measure.
//
// `--show-superproject-working-tree` is the question asked directly: it prints
// the superproject's work tree when the directory is a submodule checkout of
// its own, and nothing when the repository found is the superproject itself.
// Both answers come from one command, in that order.
func (r *Runner) submoduleHEAD(ctx context.Context, path string) (string, bool) {
	output, err := r.Run(ctx, path, "rev-parse", "--show-superproject-working-tree", "HEAD")
	if err != nil {
		// The directory is not there, or holds no repository at all. Not a
		// failure to report: a submodule nobody has fetched is the ordinary
		// state of a fresh clone.
		return "", false
	}

	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")
	if len(lines) != 2 || lines[0] == "" {
		// No superproject above it means the repository git found IS the
		// superproject, and this directory is empty.
		return "", false
	}
	return lines[1], true
}

// gitlink is one mode-160000 entry of the index.
type gitlink struct {
	path string
	sha  string
}

// gitlinks reads the submodule entries of the index.
//
// `-z` because the path is the field that can hold anything, and `--stage`
// because the mode is the only thing that tells a submodule from a directory.
func (r *Runner) gitlinks(ctx context.Context, dir string) ([]gitlink, error) {
	output, err := r.Run(ctx, dir, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}

	links := make([]gitlink, 0, 4)
	for _, record := range strings.Split(string(output), fieldSeparator) {
		if record == "" {
			continue
		}
		// `<mode> <sha> <stage>\t<path>`. Split on the tab first: everything
		// after it is the path, whatever it holds.
		meta, path, found := strings.Cut(record, "\t")
		if !found {
			return nil, fmt.Errorf("ls-files --stage: no tab in %q", record)
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("ls-files --stage: expected mode, object and stage in %q", meta)
		}
		if fields[0] != gitlinkMode {
			continue
		}
		links = append(links, gitlink{path: path, sha: fields[1]})
	}
	return links, nil
}

// declaredSubmodule is one section of .gitmodules.
type declaredSubmodule struct {
	name string
	url  string
}

// declaredSubmodules reads .gitmodules, keyed by the path each section names.
//
// Keyed by path rather than by name because the index is keyed by path, and a
// section's name need not be its path. A repository with no .gitmodules is not
// an error: `git config -f` on a missing file exits 1, and "there are none" is
// what that means here.
func (r *Runner) declaredSubmodules(ctx context.Context, dir string) (map[string]declaredSubmodule, error) {
	output, err := r.Exec(ctx, Command{
		Dir:  dir,
		Args: []string{"config", "--file", ".gitmodules", "-z", "--get-regexp", `^submodule\..*\.(path|url)$`},
		// 1 is "no matching key", which for a repository whose .gitmodules is
		// missing or empty is the answer rather than a failure.
		SuccessCodes: []int{1},
	})
	if err != nil {
		return nil, err
	}

	// Two passes, because a section's path can be read after its url: collect
	// by section name first, then key the result by the path each names.
	byName := make(map[string]map[string]string)
	for _, record := range strings.Split(string(output), fieldSeparator) {
		if record == "" {
			continue
		}
		// `git config -z` writes `key\nvalue` per record. A value holding a
		// newline is therefore ambiguous here — and a submodule path or URL
		// holding one is not something git can act on either.
		key, value, found := strings.Cut(record, "\n")
		if !found {
			continue
		}
		name, field, ok := cutSubmoduleKey(key)
		if !ok {
			continue
		}
		if byName[name] == nil {
			byName[name] = make(map[string]string, 2)
		}
		byName[name][field] = value
	}

	byPath := make(map[string]declaredSubmodule, len(byName))
	for name, fields := range byName {
		path := fields["path"]
		if path == "" {
			// A section with no path describes nothing git can find.
			continue
		}
		byPath[path] = declaredSubmodule{name: name, url: fields["url"]}
	}
	return byPath, nil
}

// cutSubmoduleKey splits `submodule.<name>.<field>` into its two halves.
//
// The name is whatever sits between, dots included: git allows a subsection
// name to hold them, and cutting at the FIRST dot after the prefix would split
// `submodule.a.b.url` into the wrong pair.
func cutSubmoduleKey(key string) (name, field string, ok bool) {
	const prefix = "submodule."
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	at := strings.LastIndex(rest, ".")
	if at <= 0 {
		return "", "", false
	}
	return rest[:at], rest[at+1:], true
}

// initialisedSubmodules names the submodules the superproject has a URL for,
// which is git's own definition of initialised: `git submodule init` is what
// writes `submodule.<name>.url` into the config that is not under version
// control.
//
// One reading for the whole list rather than one `git config --get` per row.
// A repository pinning thirty submodules is thirty subprocesses for thirty
// lines of one file, and the answer is the same file every time — the same
// reason declaredSubmodules reads .gitmodules once with --get-regexp.
//
// Exit 1 is "no matching key", which for a repository nobody has initialised
// is the answer and not a failure; every other failure travels, unlike the
// per-key read this replaced, which could not tell "unset" from "git is not
// on this machine".
func (r *Runner) initialisedSubmodules(ctx context.Context, dir string) (map[string]bool, error) {
	output, err := r.Exec(ctx, Command{
		Dir:          dir,
		Args:         []string{"config", "-z", "--get-regexp", `^submodule\..*\.url$`},
		SuccessCodes: []int{1},
	})
	if err != nil {
		return nil, err
	}

	names := make(map[string]bool, 4)
	for _, record := range strings.Split(string(output), fieldSeparator) {
		if record == "" {
			continue
		}
		// `git config -z` writes `key\nvalue` per record, as in
		// declaredSubmodules.
		key, value, found := strings.Cut(record, "\n")
		if !found || strings.TrimSpace(value) == "" {
			continue
		}
		name, field, ok := cutSubmoduleKey(key)
		if !ok || field != "url" {
			continue
		}
		names[name] = true
	}
	return names, nil
}

// AddSubmoduleArgs is the command AddSubmodule runs.
//
// The URL and the path land after `--`, so a path named `-f` arrives as a
// path. No `--force`: it exists to overwrite a directory that is already
// there, and "add a submodule" is not a request to replace something.
func AddSubmoduleArgs(url, path string) []string {
	return []string{"submodule", "add", "--", url, path}
}

// AddSubmodule pins another repository inside this one and clones it.
func (r *Runner) AddSubmodule(ctx context.Context, dir, url, path string) error {
	url, path = strings.TrimSpace(url), strings.TrimSpace(path)
	if url == "" {
		return ErrEmptySubmoduleURL
	}
	if path == "" {
		return ErrNoSubmodulePath
	}
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    AddSubmoduleArgs(url, path),
		Timeout: networkTimeout,
	})
	return err
}

// UpdateSubmodulesArgs is the command UpdateSubmodules runs.
//
// `--init` writes the URLs into the superproject's config where they are
// missing, which is the state a fresh clone leaves; without it, update on a
// never-initialised submodule succeeds while doing nothing. `--recursive`
// carries both down through submodules of submodules, which is what somebody
// asking for "get the submodules" means.
//
// A path narrows it to one; no path means all of them.
func UpdateSubmodulesArgs(path string) []string {
	args := []string{"submodule", "update", "--init", "--recursive"}
	if path != "" {
		args = append(args, "--", path)
	}
	return args
}

// UpdateSubmodules checks out the commits the superproject records.
func (r *Runner) UpdateSubmodules(ctx context.Context, dir, path string) error {
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    UpdateSubmodulesArgs(strings.TrimSpace(path)),
		Timeout: networkTimeout,
	})
	return err
}

// SyncSubmodulesArgs is the command SyncSubmodules runs.
func SyncSubmodulesArgs(path string) []string {
	args := []string{"submodule", "sync", "--recursive"}
	if path != "" {
		args = append(args, "--", path)
	}
	return args
}

// SyncSubmodules copies the URLs from .gitmodules into the local config.
//
// The operation for a submodule whose upstream moved: .gitmodules is under
// version control and the config is not, so a pull brings a new URL that
// nothing is using until this runs.
func (r *Runner) SyncSubmodules(ctx context.Context, dir, path string) error {
	_, err := r.Run(ctx, dir, SyncSubmodulesArgs(strings.TrimSpace(path))...)
	return err
}

// RemoveSubmoduleArgs is the pair of commands RemoveSubmodule runs, in order.
//
// Two commands and not one, because git has no `submodule remove`. `deinit`
// removes the checkout and the config entry; `git rm` removes the gitlink and
// the .gitmodules section, which is the half that makes the removal something
// to commit.
//
// `--force` on the deinit is what gets past a checkout holding uncommitted
// work, and it is the caller's to decide and the user's to see.
//
// What neither command touches is `.git/modules/<name>`, where the submodule's
// object database stays. That is git's design — it is what makes a removal
// undoable by hand — and the confirmation says so rather than letting somebody
// discover it.
func RemoveSubmoduleArgs(path string, force bool) [][]string {
	deinit := []string{"submodule", "deinit"}
	if force {
		deinit = append(deinit, "--force")
	}
	deinit = append(deinit, "--", path)

	remove := []string{"rm"}
	if force {
		remove = append(remove, "--force")
	}
	remove = append(remove, "--", path)

	return [][]string{deinit, remove}
}

// RemoveSubmodule unpins a repository from this one.
func (r *Runner) RemoveSubmodule(ctx context.Context, dir, path string, force bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return ErrNoSubmodulePath
	}
	for _, args := range RemoveSubmoduleArgs(path, force) {
		if _, err := r.Exec(ctx, Command{
			Dir:     dir,
			Args:    args,
			Timeout: rewriteTimeout,
		}); err != nil {
			return err
		}
	}
	return nil
}

// joinPath is filepath.Join for a repository-relative path, without importing
// path/filepath into a file that never touches the filesystem: git is given
// the result as a working directory, and both separators reach it unchanged on
// the platform that uses them.
func joinPath(dir, relative string) string {
	if relative == "" {
		return dir
	}
	return strings.TrimRight(dir, "/\\") + "/" + relative
}
