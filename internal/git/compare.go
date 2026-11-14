package git

import (
	"context"
	"fmt"
	"strings"
)

// Comparing HEAD against another reference: how far each side has run.
//
// Ahead and behind are the two halves of `rev-list --left-right --count
// HEAD...ref`. They answer the history question "how has this tip drifted
// from that one" without deciding what to do about it — merge and rebase
// each have their own plan for that, and the interface offers them from the
// counts rather than inventing a third operation.

// CompareResult is how HEAD stands against another reference.
type CompareResult struct {
	// Head is the branch HEAD is on, empty when detached. Display only: the
	// count always walks from the commit HEAD points at.
	Head string `json:"head"`

	// Against is the reference the client named, as CheckRefName accepted it.
	Against string `json:"against"`

	// AgainstShort is Against with the refs/{heads,remotes,tags}/ prefix
	// dropped, for a sentence that should not say "refs/heads/main".
	AgainstShort string `json:"against_short"`

	// Ahead is how many commits HEAD holds that Against does not.
	Ahead int `json:"ahead"`

	// Behind is how many commits Against holds that HEAD does not.
	Behind int `json:"behind"`

	// HeadSHA and AgainstSHA are the tips the count walked, so a confirmation
	// that later offers merge or rebase can name the same objects.
	HeadSHA    string `json:"head_sha"`
	AgainstSHA string `json:"against_sha"`
}

// Compare counts how far HEAD and against have diverged.
//
// against is a full reference name (refs/heads/…, refs/remotes/…,
// refs/tags/…). Short names are refused rather than resolved through git's
// search order — the same rule LocalBranchRef exists for: a repository with
// both a branch and a tag called `release` would otherwise count against the
// tag while the interface named the branch.
//
// Unborn HEAD (no commits yet) fails at rev-parse the same way every other
// history reading does; the interface does not offer Compare until there is
// a history to compare.
func (r *Runner) Compare(ctx context.Context, dir, against string) (CompareResult, error) {
	against = strings.TrimSpace(against)
	if err := CheckRefName(against); err != nil {
		return CompareResult{}, err
	}
	// Full names only. CheckRefName accepts "main" — that is a legal ref
	// component — and git would then resolve it through its search order,
	// reaching tags before heads. The interface always sends what ForEachRef
	// listed, so a short name here is a client bug rather than a choice.
	if !strings.HasPrefix(against, "refs/") {
		return CompareResult{}, fmt.Errorf(
			"%w: %q is not a full reference name", ErrBadRefName, against)
	}

	head, err := r.ReadHEAD(ctx, dir)
	if err != nil {
		return CompareResult{}, err
	}
	if head.SHA == "" {
		return CompareResult{}, ErrNoCommits
	}

	resolved, err := r.Run(ctx, dir, "rev-parse", "--verify", against+"^{commit}")
	if err != nil {
		return CompareResult{}, err
	}
	againstSHA := strings.TrimSpace(string(resolved))

	output, err := r.Run(ctx, dir, "rev-list", "--left-right", "--count",
		fmt.Sprintf("%s...%s", head.SHA, against))
	if err != nil {
		return CompareResult{}, err
	}
	ahead, behind, err := parseLeftRightCount(string(output))
	if err != nil {
		return CompareResult{}, err
	}

	headName := ""
	if !head.Detached {
		headName = head.Name
	}

	return CompareResult{
		Head:         headName,
		Against:      against,
		AgainstShort: shortRefName(against),
		Ahead:        ahead,
		Behind:       behind,
		HeadSHA:      head.SHA,
		AgainstSHA:   againstSHA,
	}, nil
}

func shortRefName(name string) string {
	switch {
	case strings.HasPrefix(name, headsPrefix):
		return strings.TrimPrefix(name, headsPrefix)
	case strings.HasPrefix(name, remotesPrefix):
		return strings.TrimPrefix(name, remotesPrefix)
	case strings.HasPrefix(name, tagsPrefix):
		return strings.TrimPrefix(name, tagsPrefix)
	default:
		return name
	}
}
