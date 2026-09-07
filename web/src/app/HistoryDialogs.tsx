import type { Head, HistoryScope, Ref, Remote, Repository } from '../api/types';
import { Dialog } from '../components/Dialog';
import { shortenSha } from '../lib/format';
import { parentDirectoryOf } from '../lib/path';
import {
  CherryPickDialog,
  CreateBranchDialog,
  DeleteBranchDialog,
  MergeBranchDialog,
  RebaseBranchDialog,
  RenameBranchDialog,
  ResetDialog,
  RevertDialog,
  SetUpstreamDialog,
  UnsetUpstreamDialog,
} from './BranchDialogs';
import type { DialogSlot } from './dialogSlot';
import { describeScope } from './historyScope';
import type { HistoryOperations } from './historyOperations';
import type { HistoryProposals } from './historyProposals';
import { TrackLFSDialog, UntrackLFSDialog } from './LFSDialogs';
import { RebasePlanDialog } from './RebasePlanDialog';
import { SearchHistory } from './SearchHistory';
import { StashApplyDialog, StashDropDialog, StashPushDialog } from './StashDialogs';
import { AddSubmoduleDialog, RemoveSubmoduleDialog } from './SubmoduleDialogs';
import { CreateTagDialog, DeleteTagDialog, PushTagDialog } from './TagDialogs';
import { trackingBranchesOnRemote } from './upstream';
import { AddWorktreeDialog, RemoveWorktreeDialog } from './WorktreeDialogs';

/**
 * Every question the history screen can put, and nothing else.
 *
 * One file because it is one job: turn the kind in the dialog slot into the box
 * that asks it. Split across the panels that offer each operation, the rule the
 * slot exists to enforce — at most one dialog, ever — would be spread over
 * eight files that each believe it.
 *
 * Nothing here decides anything. A dialog that has to ask the daemon something
 * calls a proposal, which claims the slot and reopens this same host on the
 * answer; a dialog whose answer is yes calls the operation and closes. What is
 * written out below is only the wiring between the two, and it is deliberately
 * dull: the interesting half is in historyProposals.ts, where it is written
 * once.
 */
export interface HistoryDialogsProps {
  slot: DialogSlot;
  ops: HistoryOperations;
  proposals: HistoryProposals;
  repository: Repository;
  /** What the sidebar read. The publish dialogs offer these as destinations. */
  refs: readonly Ref[];
  head: Head | undefined;
  remotes: readonly Remote[];
  /** The walk on screen: what a search covers, and how to follow a hit out of it. */
  walk: { scope: HistoryScope; refs: readonly string[]; goTo: (sha: string) => void };
  /** The commit a new tag would point at. Undefined means HEAD. */
  target: string | undefined;
}

export function HistoryDialogs({
  slot,
  ops,
  proposals,
  repository,
  refs,
  head,
  remotes,
  walk,
  target,
}: HistoryDialogsProps) {
  const { dialog, open, close } = slot;
  if (dialog === undefined) {
    return null;
  }

  switch (dialog.kind) {
    case 'search':
      return (
        <Dialog
          open
          onClose={close}
          title="Search the history"
          description={`Over ${describeScope(walk.scope, walk.refs.length)}. Choosing a commit takes the graph to it.`}
          size="wide"
        >
          <SearchHistory
            repositoryId={repository.id}
            scope={walk.scope}
            refs={walk.refs}
            onChoose={(sha) => {
              close();
              walk.goTo(sha);
            }}
          />
        </Dialog>
      );

    case 'create':
      return (
        <CreateBranchDialog
          busy={ops.branches.create.isPending}
          startLabel={startPointLabel(head)}
          onCancel={close}
          onCreate={(name, switchTo) =>
            ops.branches.create.mutate({ name, start: '', switchTo }, { onSuccess: close })
          }
        />
      );

    // Keyed on the branch, which is what seeds the field: a second rename
    // opens on the second branch's name rather than on what was typed for the
    // first.
    case 'rename':
      return (
        <RenameBranchDialog
          key={dialog.branch}
          branch={dialog.branch}
          busy={ops.branches.rename.isPending}
          onCancel={close}
          onRename={(to) =>
            ops.branches.rename.mutate({ from: dialog.branch, to }, { onSuccess: close })
          }
        />
      );

    case 'delete':
      return (
        <DeleteBranchDialog
          pending={dialog.pending}
          busy={ops.branches.remove.isPending}
          onCancel={close}
          onConfirm={() =>
            ops.branches.remove.mutate(
              { name: dialog.pending.name, force: dialog.pending.force },
              // Settled rather than succeeded: a refused delete has said
              // everything it has to say in its toast, and leaving the dialog
              // up would ask the same question again over a repository that
              // has already answered it.
              { onSettled: close },
            )
          }
        />
      );

    case 'set-upstream':
      return (
        <SetUpstreamDialog
          key={`${dialog.branch}@${dialog.remote}`}
          branch={dialog.branch}
          remotes={remotes}
          trackingChoices={trackingBranchesOnRemote(refs, dialog.remote)}
          remote={dialog.remote}
          upstream={dialog.upstream}
          plan={dialog.plan}
          hasUpstream={dialog.hasUpstream}
          busy={ops.upstream.set.isPending}
          planning={ops.upstream.planSet.isPending}
          onRemoteChange={(remote) =>
            proposals.replanSetUpstream(dialog.branch, remote, dialog.upstream)
          }
          onUpstreamChange={(upstream) =>
            proposals.replanSetUpstream(dialog.branch, dialog.remote, upstream)
          }
          onPickTracking={(upstream) =>
            proposals.replanSetUpstream(dialog.branch, dialog.remote, upstream)
          }
          onCancel={close}
          onSave={() =>
            ops.upstream.set.mutate(
              { branch: dialog.branch, remote: dialog.remote, upstream: dialog.upstream.trim() },
              { onSuccess: close },
            )
          }
        />
      );

    case 'unset-upstream':
      return (
        <UnsetUpstreamDialog
          pending={dialog.pending}
          busy={ops.upstream.unset.isPending}
          onCancel={close}
          onConfirm={() => ops.upstream.unset.mutate(dialog.pending.branch, { onSuccess: close })}
        />
      );

    case 'merge':
      return (
        <MergeBranchDialog
          plan={dialog.plan}
          busy={ops.branches.merge.isPending || ops.branches.planMerge.isPending}
          preferringMergeCommit={dialog.preferMergeCommit}
          // Re-asked, the way the publish dialog re-asks when the remote
          // changes: the command on screen must never describe a preference
          // other than the one selected (ADR 0021).
          onPreferMergeCommit={(prefer) => proposals.proposeBranchMerge(dialog.plan.branch, prefer)}
          onCancel={close}
          // The plan goes back whole. The daemon checks the branch it names
          // against where HEAD actually is, reads the two branches again, and
          // builds the command from what it finds — so a repository that moved
          // while this was open is refused rather than merged.
          onConfirm={() => ops.branches.merge.mutate(dialog.plan, { onSettled: close })}
        />
      );

    case 'rebase':
      return (
        <RebaseBranchDialog
          plan={dialog.plan}
          busy={ops.branches.rebase.isPending}
          onCancel={close}
          onConfirm={() => ops.branches.rebase.mutate(dialog.plan, { onSettled: close })}
        />
      );

    case 'rebase-plan':
      return (
        <RebasePlanDialog
          plan={dialog.plan}
          steps={dialog.steps}
          busy={ops.rewrite.run.isPending}
          onCancel={close}
          // An open and not a claim: nothing is in flight, this is the user
          // typing. The slot is where the rows live so that a re-render of the
          // workbench — a poll answering, an event arriving — cannot lose a
          // plan somebody is halfway through writing.
          onSteps={(steps) => open({ ...dialog, steps })}
          onConfirm={() =>
            ops.rewrite.run.mutate(
              { base: dialog.plan.base, from: dialog.plan.from, steps: dialog.steps },
              { onSettled: close },
            )
          }
        />
      );

    case 'create-tag':
      return (
        <CreateTagDialog
          busy={ops.tags.create.isPending}
          targetLabel={target === undefined ? startPointLabel(head) : shortenSha(target)}
          onCancel={close}
          onCreate={(name, message, annotated) =>
            ops.tags.create.mutate(
              { name, message, annotated, target: target ?? '' },
              { onSuccess: close },
            )
          }
        />
      );

    case 'delete-tag':
      return (
        <DeleteTagDialog
          pending={dialog.pending}
          busy={ops.tags.remove.isPending}
          onCancel={close}
          onConfirm={() => ops.tags.remove.mutate(dialog.pending.name, { onSettled: close })}
        />
      );

    case 'push-tag':
      return (
        <PushTagDialog
          tag={dialog.tag}
          remotes={remotes}
          chosen={dialog.remote}
          plan={dialog.plan}
          busy={ops.tags.push.isPending}
          onChoose={(remote) => proposals.proposeTagPush(dialog.tag, remote)}
          onCancel={close}
          onPush={() =>
            ops.tags.push.mutate({ name: dialog.tag, remote: dialog.remote }, { onSettled: close })
          }
        />
      );

    case 'cherry-pick':
      return (
        <CherryPickDialog
          plan={dialog.plan}
          busy={ops.cherryPick.run.isPending}
          onCancel={close}
          onConfirm={() => ops.cherryPick.run.mutate(dialog.plan, { onSettled: close })}
        />
      );

    case 'revert':
      return (
        <RevertDialog
          plan={dialog.plan}
          busy={ops.revert.run.isPending}
          onCancel={close}
          onConfirm={() => ops.revert.run.mutate(dialog.plan, { onSettled: close })}
        />
      );

    case 'reset':
      return (
        <ResetDialog
          plan={dialog.plan}
          busy={ops.reset.run.isPending}
          planning={ops.reset.plan.isPending}
          onCancel={close}
          onConfirm={() => ops.reset.run.mutate(dialog.plan, { onSettled: close })}
          // A claim and not an open, which is what the proposal does: cancelling
          // closes the dialog while this request is still out, and an open would
          // put a destructive confirmation back under the user's hands seconds
          // later, focused, with the next Enter on the button that runs it. It
          // also settles two quick mode clicks by the last CLICK rather than by
          // the last answer.
          onModeChange={(mode) => {
            if (mode !== dialog.plan.mode) {
              proposals.proposeReset(dialog.plan.commit, mode);
            }
          }}
        />
      );

    case 'stash-push':
      return (
        <StashPushDialog
          plan={dialog.plan}
          busy={ops.stash.push.isPending}
          planning={ops.stash.planPush.isPending}
          onCancel={close}
          onStash={(message, untracked) =>
            ops.stash.push.mutate({ message, untracked }, { onSettled: close })
          }
          onUntrackedChange={(untracked) => {
            if (untracked !== dialog.plan.include_untracked) {
              proposals.proposeStash(untracked);
            }
          }}
        />
      );

    case 'stash-apply':
      return (
        <StashApplyDialog
          plan={dialog.plan}
          busy={ops.stash.apply.isPending}
          planning={ops.stash.planApply.isPending}
          onCancel={close}
          onConfirm={() => ops.stash.apply.mutate(dialog.plan, { onSettled: close })}
          onModeChange={(mode) => {
            if (mode !== dialog.plan.mode) {
              proposals.proposeStashApply(dialog.plan, mode);
            }
          }}
        />
      );

    case 'stash-drop':
      return (
        <StashDropDialog
          plan={dialog.plan}
          busy={ops.stash.drop.isPending}
          onCancel={close}
          onConfirm={() => ops.stash.drop.mutate(dialog.plan, { onSettled: close })}
        />
      );

    case 'add-worktree':
      return (
        <AddWorktreeDialog
          busy={ops.worktrees.plan.isPending || ops.worktrees.add.isPending}
          rootHint={parentDirectoryOf(repository.path)}
          plan={dialog.plan}
          onCancel={close}
          onPlan={proposals.proposeWorktreeAdd}
          onCreate={(request) => ops.worktrees.add.mutate(request, { onSuccess: close })}
        />
      );

    case 'remove-worktree':
      return (
        <RemoveWorktreeDialog
          pending={dialog.pending}
          busy={ops.worktrees.remove.isPending}
          onCancel={close}
          onForce={(force) => proposals.proposeWorktreeRemove({ path: dialog.pending.path }, force)}
          onRemove={(pending) =>
            ops.worktrees.remove.mutate(
              { path: pending.path, force: pending.force },
              { onSuccess: close },
            )
          }
        />
      );

    case 'add-submodule':
      return (
        <AddSubmoduleDialog
          busy={ops.submodules.plan.isPending || ops.submodules.add.isPending}
          plan={dialog.plan}
          onCancel={close}
          onPlan={proposals.proposeSubmoduleAdd}
          onAdd={(url, path) => ops.submodules.add.mutate({ url, path }, { onSuccess: close })}
        />
      );

    case 'remove-submodule':
      return (
        <RemoveSubmoduleDialog
          pending={dialog.pending}
          busy={ops.submodules.remove.isPending}
          onCancel={close}
          onForce={(force) => proposals.proposeSubmoduleRemove(dialog.pending.submodule, force)}
          onRemove={(pending) =>
            ops.submodules.remove.mutate(
              { path: pending.submodule.path, force: pending.force },
              { onSuccess: close },
            )
          }
        />
      );

    case 'track-lfs':
      return (
        <TrackLFSDialog
          busy={ops.lfs.plan.isPending || ops.lfs.run.isPending}
          plan={dialog.plan}
          onCancel={close}
          onPlan={proposals.proposeTrackLFS}
          onTrack={(pattern) =>
            ops.lfs.run.mutate({ action: 'track', pattern }, { onSuccess: close })
          }
        />
      );

    case 'untrack-lfs':
      return (
        <UntrackLFSDialog
          pattern={dialog.pattern}
          command={dialog.command}
          busy={ops.lfs.run.isPending}
          onCancel={close}
          onUntrack={(pattern) =>
            ops.lfs.run.mutate({ action: 'untrack', pattern }, { onSuccess: close })
          }
        />
      );
  }
}

/**
 * Where a new branch or tag would start, in words.
 *
 * The dialog names it rather than offering a field: it is always HEAD, which is
 * the only start point this screen can be sure of, and saying "it will start at
 * main" is the difference between a dialog that tells you what it is about to
 * do and one that assumes you know.
 */
export function startPointLabel(head: Head | undefined): string {
  if (head === undefined) {
    return 'the first commit';
  }
  return head.detached ? `the commit HEAD is on, ${shortenSha(head.sha)}` : head.name;
}
