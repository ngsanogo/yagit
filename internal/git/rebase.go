package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Rebasing the branch HEAD is on onto another local branch.
//
// The upstream name reaches git after `--`, for the same reason every
// reference in branch.go and merge.go does: a string somebody typed must never
// be read as an option.
//
// What the command DOES is never left to configuration, and a rebase reads
// more settings than a merge did. `rebase.backend` chooses between two
// different implementations with different conflict behaviour;
// `rebase.autoSquash` turns a commit whose subject begins `fixup!` into an
// instruction to amend another one; `rebase.autoStash` stashes a dirty work
// tree and puts it back, so a refusal the confirmation promised becomes a
// silent stash; `rebase.rebaseMerges` decides whether merge commits are
// recreated or flattened away; and `rebase.updateRefs` force-updates every
// OTHER branch pointing into the replayed range. Each is pinned by a flag in
// RebaseArgs. docs/adr/0021 is the rule; docs/adr/0023 is what following it
// cost here.
//
// What would be replayed is read before the command runs (PreviewRebase), and
// the outcome travels back with the confirmation so a rebase that has become a
// different operation while the dialog was open is refused rather than run
// under the old description — the lease of docs/adr/0022.

// RebaseOutcome is what rebasing onto another branch would do.
//
// Three, and they are the same three a merge has, because the same fact
// decides both: how the two branches stand to each other. One already contains
// the other and there is nothing to do; the branch holds nothing of its own
// and simply moves; or both have moved and commits have to be written
// somewhere else. What differs is which one is expensive — for a merge it is
// the last that records a commit, and for a rebase it is the last that
// rewrites history.
type RebaseOutcome string

const (
	// RebaseUpToDate: the upstream is already contained in the branch AND
	// nothing on the way to it is a merge, so git answers "Current branch is
	// up to date" and writes nothing.
	//
	// Both halves are load-bearing, and git's own name for the second is
	// linear history: it takes the shortcut only when the walk from HEAD down
	// to the upstream passes no merge commit. A branch that merged the
	// upstream in twice is ahead of it and not linear, and git flattens it
	// rather than doing nothing — see RebaseReplay, and
	// TestPreviewRebaseReplaysAnAheadBranchThatHoldsAMerge.
	//
	// This is one of the two outcomes a one-directional count reads backwards.
	// `git rev-list --count onto..from` is non-zero here — the branch is
	// AHEAD, usually by everything its author has been working on — and
	// reading that as "commits to replay" promises a rewrite git refuses to
	// perform.
	RebaseUpToDate RebaseOutcome = "up-to-date"

	// RebaseFastForward: the branch holds no commit of its own, so nothing is
	// replayed and the branch is moved onto the upstream. No commit is
	// written, so no hook runs and nothing is signed — but the work tree is
	// checked out again underneath it, which is the half worth saying.
	//
	// The other outcome the one-directional count reads backwards: `onto..from`
	// is zero here, which was reported as "rebasing changes nothing" while git
	// moved the branch and rewrote every file under it.
	RebaseFastForward RebaseOutcome = "fast-forward"

	// RebaseReplay: the branch's own commits are written again on top of the
	// upstream, under new hashes, with the user's hooks running and a conflict
	// as the ordinary way for it to stop halfway.
	//
	// Two arrangements reach it, and the second is the one worth naming. Both
	// branches holding commits the other does not is the obvious one. The
	// other is a branch strictly AHEAD of the upstream that holds a merge
	// commit: git cannot take its up-to-date shortcut across a merge, so it
	// replays the range as a straight line and the merge commits are gone.
	// That is the ordinary shape of a branch somebody merged main into twice,
	// and reading it as up-to-date promised that nothing would happen while
	// git rewrote the branch.
	RebaseReplay RebaseOutcome = "rebase"
)

// errUnknownRebaseOutcome names the three, because every refusal here is
// somebody being told which words the field takes.
var errUnknownRebaseOutcome = errors.New(
	"the rebase outcome must be one of up-to-date, fast-forward, rebase")

// ParseRebaseOutcome reads an outcome sent by a client.
//
// An unknown one is refused rather than read as a default, for the reason
// ParseMergeOutcome refuses one: the three differ by whether history is
// rewritten, and running the wrong one would look entirely correct to a client
// that asked for another.
func ParseRebaseOutcome(raw string) (RebaseOutcome, error) {
	switch RebaseOutcome(raw) {
	case RebaseUpToDate, RebaseFastForward, RebaseReplay:
		return RebaseOutcome(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownRebaseOutcome)
	default:
		return "", fmt.Errorf("%w: %q is not one of them", errUnknownRebaseOutcome, raw)
	}
}

// RebaseArgs is the command Rebase runs to reach an outcome.
//
// Exported for the reason MergeArgs is: the line the user is shown and the
// line git receives have one definition between them.
//
// Five flags pin five settings git would otherwise read, and none of them is
// cosmetic:
//
//   - --merge pins `rebase.backend`. The two backends are two programs: the
//     apply backend rewinds and re-applies patches, resolves conflicts
//     differently, and does not implement --update-refs at all. It is the
//     default nowhere any more and it is still one line of config away.
//   - --no-autosquash pins `rebase.autoSquash`, which promotes a commit whose
//     subject begins `fixup!` from a commit into an instruction.
//   - --no-autostash pins `rebase.autoStash`. A dirty work tree is refused
//     rather than silently stashed and reapplied, because the confirmation
//     named commits and said nothing about uncommitted work.
//   - --no-rebase-merges pins `rebase.rebaseMerges`. Which of the two is right
//     is a matter of taste; which one HAPPENED must not be. The merge commits
//     that get flattened away are counted and named on the confirmation
//     instead — see PreviewRebase.
//   - --no-update-refs pins `rebase.updateRefs`, and it is the one that moves
//     what nobody was shown: with the setting on, a rebase force-updates every
//     other branch pointing into the replayed range. That is a stacked-branch
//     workflow git is right to offer and wrong to perform silently under a
//     dialog naming one branch. It needs git 2.38, the highest bar yagit sets
//     for any operation; `gitFloors` in internal/api/server.go carries the
//     whole list, and /api/health reports the ones this machine's git misses.
//
// Two more behaviours the confirmation depends on are deliberately NOT pinned,
// and the reason is the same for both: the flag that looks like it would pin
// them holds nothing up. Every flag above is drawn on the confirmation
// character-for-character, so one that defends no setting is a longer line
// bought for nothing — and worse, it tells the next reader that a setting was
// dealt with when it was not.
//
// `rebase.forkPoint` changes which commits are replayed — git walks the
// upstream's reflog for a better fork than the merge base, so a branch rebased
// once already replays fewer commits than PreviewRebase counted. It cannot
// happen here: the setting only ever turns fork-point OFF, and naming an
// upstream on the command line, which every rebase here does, is already
// --no-fork-point by git's own default. So the default is what is tested, in
// TestRebaseIgnoresForkPointWhereAnUpstreamIsNamed.
//
// Dropping a commit whose patch is already upstream has no setting behind it
// at all. `--reapply-cherry-picks` is a flag and only a flag: `git help -c`
// lists no `rebase.reapplyCherryPicks`, so a `--no-reapply-cherry-picks` here
// would restate git's default rather than pin it, and configuration could not
// have moved it in the first place. What the confirmation leans on is that
// default, so the default is what is tested, in
// TestRebaseDropsAlreadyAppliedCommitsByDefault.
//
// --no-ff is the lease, and only the replay has one. It is what makes the
// sentence on the confirmation true rather than nearly true: without it git
// fast-forwards over the commits at the bottom of the range that are unchanged
// against the new base, so they keep their hashes and stay on the branch,
// while the dialog promised new ones in their place. Verified in
// TestRebaseReplayWritesEveryCommitAgain. It also holds the promise up against
// the upstream being REWOUND between the last reading and this exec — a reset,
// a force-fetch, to something the branch already contains — which turns an
// approved replay into "Current branch is up to date" and nothing at all,
// under a toast reporting a rebase.
//
// The other two outcomes have no such flag. git has no `--ff-only` for rebase,
// so nothing in the argument list can refuse to move a branch that was
// described as going nowhere; what holds them up is the route re-reading the
// two branches immediately before this. That gap is named here rather than
// papered over, and --no-ff is deliberately NOT passed for them: on an
// up-to-date rebase it means "rebase forced", which rewrites every commit on
// the branch under new hashes — the exact thing that outcome promised would
// not happen.
//
// Interactive rebase is not offered here; `-i` needs an editor the daemon has
// no terminal to open.
func RebaseArgs(onto string, outcome RebaseOutcome) []string {
	args := []string{
		"rebase",
		"--merge",
		"--no-autosquash",
		"--no-autostash",
		"--no-rebase-merges",
		"--no-update-refs",
	}
	if outcome == RebaseReplay {
		args = append(args, "--no-ff")
	}

	return append(args, "--", LocalBranchRef(onto))
}

// Rebase replays the branch HEAD is on onto another.
//
// There is no `from` parameter, and its absence is the point: git rebases
// whatever HEAD points at, so a name passed here could only be compared
// against HEAD or ignored. Comparing it is the caller's job — a caller is the
// only thing that knows what it promised — and the rebase route does exactly
// that before reaching this.
//
// The outcome is taken as given. It chooses the flag that pins the operation
// (RebaseArgs), and it comes from PreviewRebase one line earlier on the route
// rather than from a client, so there is nothing left here to validate a
// second time.
//
// A conflict is not hidden: git stops, writes the markers into the work tree
// and exits non-zero, and that refusal travels whole — command, exit code,
// stderr. The interface names the state and offers to resolve it.
//
// Not Run, for the reason Merge is not: a rebase checks out a tree per commit
// and can run the user's pre-commit and commit-msg hooks on each one, waiting
// on a smartcard or a pinentry every time. Thirty seconds is the deadline for
// commands that return instantly, and killing this one halfway leaves a
// half-replayed sequence while the user is told their command timed out.
func (r *Runner) Rebase(ctx context.Context, dir, onto string, outcome RebaseOutcome) error {
	onto = strings.TrimSpace(onto)
	if onto == "" {
		return ErrNoBranchName
	}

	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    RebaseArgs(onto, outcome),
		Timeout: rewriteTimeout,
	})
	return err
}

// RebasePreview is what rebasing onto another branch would do, read from the
// two branches rather than assumed.
type RebasePreview struct {
	Outcome RebaseOutcome

	// Rewriting is how many ordinary commits the branch holds past the fork,
	// and it is zero on the two outcomes that write nothing.
	//
	// A statement about the branch rather than a prediction of git's replay
	// loop, and that is deliberate: every one of them stops being what the
	// branch points at, whether git writes it again under a new hash or drops
	// it as a patch already upstream ("warning: skipped previously applied
	// commit"). git's own count can only ever be smaller — it also drops
	// commits that turn out empty against the new base, which nothing can know
	// without doing the rebase — so this is the number that is true before the
	// command runs, and it errs towards warning about more rather than less.
	//
	Rewriting int

	// Flattening is how many merge commits are discarded instead.
	//
	// Counted because it is a loss and nothing else on the confirmation would
	// say so: --no-rebase-merges replays a straight line, so a merge commit
	// inside the range is not recreated anywhere. Rewriting and Flattening
	// partition what the branch holds past the fork, which is what makes both
	// numbers readable off one walk and a second.
	Flattening int

	// Behind is how many commits the upstream holds that the branch does not.
	// Zero is what makes a rebase up to date.
	Behind int
}

// PreviewRebase reads what rebasing `from` onto `onto` would do.
//
// Both directions, in one walk. `git rev-list --left-right --count a...b`
// counts each side of the symmetric difference, and it takes both sides to say
// what a rebase is: a single `onto..from` count cannot, and it reads the two
// one-sided arrangements as each other's opposite — see RebaseUpToDate and
// RebaseFastForward, which are the two it got backwards.
//
// Merge's rule with one more term, and the term is not a refinement: git takes
// its "Current branch is up to date" shortcut only across LINEAR history, so a
// branch that is ahead of the upstream and holds a merge commit gets flattened
// rather than left alone. The merge commits therefore decide the outcome as
// well as describing it, and the second walk cannot wait until the outcome is
// known. See rebaseOutcomeOf.
//
// The names are spelled refs/heads/… for the reason PreviewMerge spells them:
// a short name is resolved by git's own search order, which reaches refs/tags
// before refs/heads, and a repository holding both a branch and a tag called
// `dup` would count against the tag while the confirmation named the branch.
//
// Unrelated histories are not refused, and that is the one place this parts
// company with PreviewMerge. `git merge` stops at "refusing to merge unrelated
// histories"; `git rebase` replays the branch onto the other root without a
// word of complaint. Refusing here would be yagit inventing a rule git does
// not have, and the answer it gives instead — every commit the branch has ever
// held, about to be written again — is a louder warning than a refusal.
//
// What this does NOT read is the work tree. A rebase is also refused by local
// changes it would overwrite, and finding out which files those are means
// doing the rebase; git's own refusal names them and travels whole. The
// boundary is worth stating: this answers what the two BRANCHES make of each
// other, and everything it answers is a promise the run route keeps.
func (r *Runner) PreviewRebase(ctx context.Context, dir, from, onto string) (RebasePreview, error) {
	from, onto = strings.TrimSpace(from), strings.TrimSpace(onto)
	if from == "" || onto == "" {
		return RebasePreview{}, ErrNoBranchName
	}

	// A name no branch has fails here, with git's own "unknown revision" —
	// the honest answer to a plan asked about a branch deleted since the
	// sidebar drew it.
	output, err := r.Run(ctx, dir, "rev-list", "--left-right", "--count",
		fmt.Sprintf("%s...%s", LocalBranchRef(from), LocalBranchRef(onto)))
	if err != nil {
		return RebasePreview{}, err
	}

	ahead, behind, err := parseLeftRightCount(string(output))
	if err != nil {
		return RebasePreview{}, err
	}

	// Skipped only where the branch holds nothing past the fork, which is the
	// one arrangement whose merge count is knowable without asking: an empty
	// range holds no merge.
	merges := 0
	if ahead > 0 {
		if merges, err = r.countMerges(ctx, dir, from, onto); err != nil {
			return RebasePreview{}, err
		}
	}

	preview := RebasePreview{Outcome: rebaseOutcomeOf(ahead, behind, merges), Behind: behind}

	// The cost is carried only by the outcome that has one. An up-to-date
	// rebase writes nothing and a fast-forward moves a pointer, and a
	// confirmation showing either of them a number of commits to rewrite would
	// be naming a loss that does not happen.
	if preview.Outcome == RebaseReplay {
		preview.Flattening = merges

		// Clamped, because the two walks are two subprocesses and a commit can
		// land on either branch between them — a terminal, a second tab. The
		// subtraction is then across two readings of two different
		// repositories and can go negative, which would reach the confirmation
		// as "-1 commits". Clamped rather than refused: the plan is read again
		// before anything runs, and a dialog that failed to open because
		// somebody committed elsewhere is a worse answer than one whose count
		// is a moment old.
		preview.Rewriting = max(0, ahead-merges)
	}

	return preview, nil
}

// rebaseOutcomeOf reads the three counts as the three things a rebase can be.
//
// Behind first, and that order is half of the correction: a branch the upstream
// brings nothing to has nowhere to move, however far ahead of it the branch has
// run, and reading "ahead" as "work to replay" is what promised a rewrite git
// answers with "Current branch is up to date".
//
// The merge count is the other half, and it is why this is not merge's
// outcomeOf with different words. git reaches that answer through
// can_fast_forward AND is_linear_history: the upstream must be an ancestor of
// HEAD, and the walk between them must pass no merge commit. Where it does,
// git has no shortcut to take and replays the range as a straight line, so a
// branch that is ahead of its upstream and holds a merge is a rewrite — which
// is the ordinary shape of a branch somebody merged main into and then rebased.
// A merge and a rebase genuinely differ here, and the two functions differ with
// them.
func rebaseOutcomeOf(ahead, behind, merges int) RebaseOutcome {
	switch {
	case behind == 0 && merges == 0:
		return RebaseUpToDate
	case ahead == 0:
		return RebaseFastForward
	default:
		return RebaseReplay
	}
}

// countMerges counts the merge commits a rebase would flatten away.
//
// `refs/heads/onto..refs/heads/from` is what the branch holds past the fork —
// the same set the left half of PreviewRebase's walk counted — and `--merges`
// keeps the commits --no-rebase-merges will not recreate. Everything else in
// the range is what gets written again, so this one number answers for both:
// the caller subtracts rather than walking a second time.
//
// --count rather than a listing, because a branch that forked a thousand
// commits ago is a megabyte of hashes read only to be counted. The counting
// and the parse are countCommits's — this is the range and the filter.
func (r *Runner) countMerges(ctx context.Context, dir, from, onto string) (int, error) {
	return r.countCommits(ctx, dir,
		fmt.Sprintf("%s..%s", LocalBranchRef(onto), LocalBranchRef(from)), "--merges")
}
