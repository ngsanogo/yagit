import { useRef } from 'react';

import type {
  Ref,
  Remote,
  ResetMode,
  Stash,
  StashApplyMode,
  StashHandle,
  Submodule,
  WorktreeRequest,
} from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import type { DialogSlot } from './dialogSlot';
import type { HistoryOperations } from './historyOperations';
import { initialSteps } from './interactiveRebase';
import { stashLabel, stashRef } from './stash';
import { alreadyOnBranchToast } from './useCherryPick';
import { parseUpstreamRef } from './upstream';

/**
 * Turning a click on the history screen into a question the daemon has already
 * answered.
 *
 * Every one of these reads a plan first and opens the confirmation on what came
 * back, which is [ADR 0021] and [ADR 0022]: the command a dialog shows is the
 * command that will run, read from the repository at the moment of the click.
 * `slot.propose` is that shape, so what is left here is only what differs —
 * which mutation to ask, what to ask it, and how to say what could not be read.
 *
 * They live beside the operations rather than inside the screen because two
 * places need the same ones. Removing a worktree is offered by its panel and
 * re-asked by the `--force` box on its own confirmation; publishing a tag is
 * offered by the sidebar and re-asked when the remote on the dialog changes. A
 * second copy of either would be a second chance for the two to disagree about
 * what the line on screen means.
 *
 * [ADR 0021]: ../../../docs/adr/0021-a-shown-command-is-not-a-setting.md
 * [ADR 0022]: ../../../docs/adr/0022-a-merge-is-checked-before-it-runs.md
 */
export function useHistoryProposals(
  slot: DialogSlot,
  ops: HistoryOperations,
  repository: { refs: readonly Ref[]; remotes: readonly Remote[]; remotesFetched: boolean },
) {
  const toast = useToast();
  const { propose, open } = slot;

  // Which set-upstream plan is still the live one. Typing faster than the
  // daemon answers is ordinary, and only the last request may write the
  // command on the dialog. Not slot.claim's counter: that one settles the
  // SLOT, and every keystroke here is aimed at a dialog already in it.
  const upstreamGeneration = useRef(0);

  /** The remote a publish starts on, or a toast saying why there is none. */
  function firstRemote(failureTitle: string): Remote | undefined {
    const first = repository.remotes[0];
    if (first === undefined) {
      toast.push({
        tone: 'danger',
        title: failureTitle,
        detail: repository.remotesFetched
          ? 'This repository has no remote configured.'
          : 'The remotes are still being read. Try again in a moment.',
      });
      return undefined;
    }
    return first;
  }

  function upstreamOf(branch: string) {
    const reference = repository.refs.find(
      (entry) => entry.kind === 'branch' && entry.short_name === branch,
    );
    return parseUpstreamRef(reference?.upstream);
  }

  /**
   * Re-asks the daemon when either field of the publish dialog changes.
   *
   * An open rather than a claim: the dialog is already up and the user is
   * typing in it, so there is no slot to win — what there is to settle is
   * which keystroke the command on screen belongs to, and that is the
   * generation below. An answer for an earlier keystroke is dropped.
   */
  function replanSetUpstream(branch: string, remote: string, upstream: string) {
    const trimmed = upstream.trim();
    const hasUpstream = upstreamOf(branch) !== undefined;
    const generation = ++upstreamGeneration.current;

    open({ kind: 'set-upstream', branch, hasUpstream, remote, upstream, plan: undefined });
    if (trimmed === '') {
      return;
    }

    ops.upstream.planSet.mutate(
      { branch, remote, upstream: trimmed },
      {
        onSuccess: (plan) => {
          if (generation !== upstreamGeneration.current) {
            return;
          }
          // The daemon answers about what it was asked. A plan describing a
          // different destination than the fields now hold is an answer to a
          // question nobody is looking at any more.
          if (plan.remote !== remote || plan.upstream !== trimmed) {
            return;
          }
          open({ kind: 'set-upstream', branch, hasUpstream, remote, upstream: trimmed, plan });
        },
        onError: (error) => {
          if (generation !== upstreamGeneration.current) {
            return;
          }
          toast.push({
            tone: 'danger',
            title: `Could not read what setting upstream for ${branch} would run`,
            detail: errorDescription(error),
          });
        },
      },
    );
  }

  /**
   * Opens the publish dialog on the branch's current upstream, if it has one.
   *
   * Opens first and asks second, unlike every other proposal here, because
   * this dialog has fields: there is nothing to plan until somebody has said
   * where the branch should point, and a branch that already has an upstream
   * is the one case where that is already known.
   */
  function proposeSetUpstream(branch: string) {
    const first = firstRemote(`Could not set upstream for ${branch}`);
    if (first === undefined) {
      return;
    }
    const parsed = upstreamOf(branch);
    const remote = parsed?.remote ?? first.name;
    const upstream = parsed?.branch ?? '';

    slot.claim()({
      kind: 'set-upstream',
      branch,
      hasUpstream: parsed !== undefined,
      remote,
      upstream,
      plan: undefined,
    });
    if (upstream !== '') {
      replanSetUpstream(branch, remote, upstream);
    }
  }

  function proposeUnsetUpstream(branch: string) {
    propose(
      ops.upstream.planUnset,
      branch,
      `what unsetting upstream for ${branch} would run`,
      ({ command }) => ({ kind: 'unset-upstream', pending: { branch, command } }),
    );
  }

  /**
   * Puts the delete question, once the daemon has said what answering yes
   * would run.
   *
   * Force is asked for up front. `-d` refuses an unmerged branch, and somebody
   * who meets that refusal, reopens the dialog and confirms again has been
   * asked the same question twice; what they need is to be told what is at
   * stake once, which is what the confirmation names.
   */
  function proposeBranchDelete(name: string) {
    propose(
      ops.branches.planDelete,
      { name, force: true },
      `what deleting ${name} would run`,
      ({ command }) => ({ kind: 'delete', pending: { name, force: true, command } }),
    );
  }

  /**
   * Puts the merge question, once the daemon has said what it would do.
   *
   * The plan carries more than a command: which branch this is going into, and
   * whether it is a pointer moving or a commit being recorded. All of it is
   * read on the daemon at this moment, so nothing on the dialog is assembled
   * from a reference list the browser may have been holding since before the
   * click — and the plan is what goes back when the button is pressed.
   */
  function proposeBranchMerge(branch: string, mergeCommit = false) {
    propose(
      ops.branches.planMerge,
      { branch, mergeCommit },
      `what merging ${branch} would do`,
      (plan) => ({ kind: 'merge', plan, preferMergeCommit: mergeCommit }),
    );
  }

  function proposeBranchRebase(onto: string) {
    propose(ops.branches.planRebase, onto, `what rebasing onto ${onto} would do`, (plan) => ({
      kind: 'rebase',
      plan,
    }));
  }

  function proposeTagDelete(name: string) {
    propose(ops.tags.planDelete, name, `what deleting ${name} would run`, ({ command }) => ({
      kind: 'delete-tag',
      pending: { name, command },
    }));
  }

  /**
   * Opens the push-tag dialog on a destination the daemon has described.
   *
   * Starts on the first remote in git's list — usually origin — the same way
   * publishing a branch does. Changing the remote asks again, so the line on
   * screen always names the remote beside it.
   */
  function proposeTagPush(tag: string, remote?: string) {
    const chosen = remote ?? firstRemote(`Could not push ${tag}`)?.name;
    if (chosen === undefined) {
      return;
    }
    // Opened without a plan first: the remote is a choice on this dialog, so
    // the box has to be on screen for the choice that re-asks to be made at
    // all.
    open({ kind: 'push-tag', tag, remote: chosen, plan: undefined });
    propose(
      ops.tags.planPush,
      { name: tag, remote: chosen },
      `what pushing ${tag} would run`,
      (plan) => ({ kind: 'push-tag', tag, remote: chosen, plan }),
    );
  }

  /**
   * Puts the cherry-pick question, once the daemon has said what it would do.
   *
   * Up-to-date is the whole answer: there is no command that succeeds as a
   * no-op, so the plan toasts and the dialog never opens.
   */
  function proposeCherryPick(sha: string) {
    propose(ops.cherryPick.plan, sha, `what cherry-picking ${shortenSha(sha)} would do`, (plan) => {
      if (plan.outcome === 'up-to-date') {
        toast.push(alreadyOnBranchToast(plan));
        return undefined;
      }
      return { kind: 'cherry-pick', plan };
    });
  }

  /**
   * Puts the revert question, once the daemon has said what it would do.
   *
   * Merges, roots and commits not on the branch are refused by the plan, and
   * the refusal is what the toast carries: there is no command that could
   * succeed, so no dialog opens.
   */
  function proposeRevert(sha: string) {
    propose(ops.revert.plan, sha, `what reverting ${shortenSha(sha)} would do`, (plan) => ({
      kind: 'revert',
      plan,
    }));
  }

  /**
   * Puts the reset question, and asks again whenever the mode changes.
   *
   * Mixed is what it opens on — git's own default when no flag is given — and
   * the segments on the dialog come back through here, so the command on
   * screen is never a mode other than the one selected.
   */
  function proposeReset(commit: string, mode: ResetMode = 'mixed') {
    propose(
      ops.reset.plan,
      { commit, mode },
      `what a ${mode} reset to ${shortenSha(commit)} would do`,
      (plan) => ({ kind: 'reset', plan }),
    );
  }

  /**
   * Opens the rebase plan, once the daemon has said which commits it may cover.
   *
   * The rows start as the history exactly as it stands — every commit kept, in
   * the order the branch already holds them — because that is what an
   * interactive rebase starts from in a terminal too, and a dialog proposing an
   * edit nobody made would be answering a question it asked itself.
   */
  function proposeRewrite(sha: string) {
    propose(
      ops.rewrite.plan,
      sha,
      `which commits after ${shortenSha(sha)} could be rewritten`,
      (plan) => ({ kind: 'rebase-plan', plan, steps: initialSteps(plan.commits) }),
    );
  }

  /**
   * Puts the stash question, and asks again when the untracked box is ticked.
   *
   * Unticked to start with, which is git's own default: `git stash push` saves
   * tracked changes. A work tree holding nothing else is described rather than
   * refused, and the dialog opens with its button disabled and the box beside
   * it — because that box is the only thing that turns it into a stash git
   * would make.
   */
  function proposeStash(untracked = false) {
    propose(
      ops.stash.planPush,
      untracked,
      untracked ? 'what including untracked files would save' : 'what stashing would save',
      (plan) => ({ kind: 'stash-push', plan }),
    );
  }

  /**
   * Puts the put-back question, and asks again when the mode changes.
   *
   * Pop is what it opens on: it is what somebody reaching for a stash usually
   * means, and the segment beside it re-asks so the command on screen stays
   * the one that will run.
   */
  function proposeStashApply(stash: StashHandle, mode: StashApplyMode = 'pop') {
    propose(
      ops.stash.planApply,
      { stash, mode },
      `what a ${mode} of ${stashRef(stash.index)} would do`,
      (plan) => ({ kind: 'stash-apply', plan }),
    );
  }

  function proposeStashDrop(stash: Stash) {
    propose(ops.stash.planDrop, stash, `what dropping ${stashLabel(stash)} would take`, (plan) => ({
      kind: 'stash-drop',
      plan,
    }));
  }

  /**
   * Opens the add-worktree confirmation on the command the daemon wrote.
   *
   * The dialog is already up — it has fields — so this is the second half of
   * it rather than the way in.
   */
  function proposeWorktreeAdd(request: WorktreeRequest) {
    propose(
      ops.worktrees.plan,
      request,
      `what checking out into ${request.path} would run`,
      (plan) => ({ kind: 'add-worktree', plan }),
    );
  }

  /**
   * Opens the remove-worktree confirmation, and asks again when `--force` is
   * toggled, so the line on the dialog is always the line that would run.
   */
  function proposeWorktreeRemove(worktree: { path: string }, force = false) {
    propose(
      ops.worktrees.planRemove,
      { path: worktree.path, force },
      `what removing ${worktree.path} would run`,
      ({ command }) => ({
        kind: 'remove-worktree',
        pending: { path: worktree.path, force, command },
      }),
    );
  }

  function proposeSubmoduleAdd(url: string, path: string) {
    propose(ops.submodules.plan, { url, path }, `what adding ${path} would run`, ({ command }) => ({
      kind: 'add-submodule',
      plan: command,
    }));
  }

  /**
   * Opens the remove-submodule confirmation on the pair of commands the daemon
   * wrote. Toggling `--force` asks again, for the same reason a worktree does.
   */
  function proposeSubmoduleRemove(submodule: Submodule, force = false) {
    propose(
      ops.submodules.planRemove,
      { path: submodule.path, force },
      `what removing ${submodule.path} would run`,
      ({ commands }) => {
        const [first, ...rest] = commands;
        // A plan with no command is a plan with nothing to confirm. The daemon
        // does not send one; if it ever did, an empty dialog would be worse
        // than none.
        if (first === undefined) {
          return undefined;
        }
        return {
          kind: 'remove-submodule',
          pending: { submodule, force, commands: [first, ...rest] },
        };
      },
    );
  }

  function proposeTrackLFS(pattern: string) {
    propose(
      ops.lfs.plan,
      { action: 'track', pattern },
      `what tracking ${pattern} would run`,
      (command) => ({ kind: 'track-lfs', plan: command }),
    );
  }

  function proposeUntrackLFS(pattern: string) {
    propose(
      ops.lfs.plan,
      { action: 'untrack', pattern },
      `what untracking ${pattern} would run`,
      (command) => ({ kind: 'untrack-lfs', pattern, command }),
    );
  }

  return {
    proposeSetUpstream,
    replanSetUpstream,
    proposeUnsetUpstream,
    proposeBranchDelete,
    proposeBranchMerge,
    proposeBranchRebase,
    proposeTagDelete,
    proposeTagPush,
    proposeCherryPick,
    proposeRevert,
    proposeReset,
    proposeRewrite,
    proposeStash,
    proposeStashApply,
    proposeStashDrop,
    proposeWorktreeAdd,
    proposeWorktreeRemove,
    proposeSubmoduleAdd,
    proposeSubmoduleRemove,
    proposeTrackLFS,
    proposeUntrackLFS,
  };
}

export type HistoryProposals = ReturnType<typeof useHistoryProposals>;
