import type { UseQueryResult } from '@tanstack/react-query';

import type { RefsPayload, Remote, Repository } from '../api/types';
import { Panel } from '../components/Panel';
import { Centered, QueryErrorState } from '../components/PanelState';
import { Spinner } from '../components/Spinner';
import type { DialogSlot } from './dialogSlot';
import type { HistoryOperations } from './historyOperations';
import type { HistoryProposals } from './historyProposals';
import type { Inspected } from './HistoryView';
import { LFSPanel } from './LFSPanel';
import { ReflogPanel } from './ReflogPanel';
import { RefSidebar } from './RefSidebar';
import { RepositoryAdditions } from './RepositoryAdditions';
import { stashPushRefusal } from './stash';
import { StashPanel } from './StashPanel';
import { SubmodulePanel } from './SubmodulePanel';
import { WorktreePanel } from './WorktreePanel';

/**
 * The column beside the history: the references, then what the repository
 * holds that is not a commit — its stashes, its reflog, its worktrees, its
 * submodules, the files it routes through LFS.
 */
export function HistorySidebar({
  repository,
  refs,
  canCheckOut,
  changed,
  remotes,
  ops,
  slot,
  proposals,
  inspected,
  onInspect,
  onGoTo,
}: {
  repository: Repository;
  refs: UseQueryResult<RefsPayload>;
  canCheckOut: boolean;
  /** How many paths differ in the work tree right now. */
  changed: number;
  remotes: Remote[];
  ops: HistoryOperations;
  slot: DialogSlot;
  proposals: HistoryProposals;
  inspected: Inspected | undefined;
  onInspect: (what: Inspected) => void;
  onGoTo: (sha: string) => void;
}) {
  const { dialog, open: openDialog } = slot;

  // Not a detached-HEAD refusal, unlike the commit pane's: `git stash` works
  // perfectly well without a branch and records "(no branch)". What it cannot
  // do is save a work tree that matches HEAD.
  const stashRefusal = stashPushRefusal(changed);

  return (
    <>
      {/* One scroller for the whole column, and every panel in it sized to
          what it holds. The invariant is the one this column has always had —
          a repository with a thousand tags must scroll something other than
          the page, and yagit's own already overflowed a 900-pixel window by
          five hundred — but the element that takes the overflow is the column
          rather than each panel in turn.

          What that replaces is a column in which the references were both the
          only child that could grow and the only one that could shrink:
          `flex-1` against four capped siblings. They took every spare pixel on
          a tall window — five hundred of them, half of it empty bordered
          surface, for five branches — and were the first thing crushed on a
          short one, down past their own header while a panel listing two
          filename patterns kept its full height. Sized to content they take
          neither, and the cap on the references panel itself is what keeps the
          thousand tags scrolling inside it instead of pushing the stash off
          the bottom of the column. */}
      {/* Twenty-one rem, and it was eighteen, then twenty. The extra is what
          the labels in these panel headers need: every one of them that opens
          a dialog carries an ellipsis, two of them sit beside a panel title,
          and the title now carries a glyph of its own. "References" was the
          part that gave way each time — at twenty rem the header measured 294
          pixels of room, the actions took 190 of it, and the title needed 102
          of the 92 that were left, so the one word naming the panel was drawn
          as "Referenc…". The column is also where a branch name is read, and
          those truncate first everywhere else too. What it costs is 48 pixels
          of the history beside it, which is a column the commit rows stopped
          needing when they stopped stacking. */}
      <div className="flex w-84 shrink-0 flex-col gap-3 overflow-y-auto">
        {refs.isPending && (
          <Panel title="References" className="shrink-0">
            <Centered compact>
              <Spinner label="Reading the references" />
            </Centered>
          </Panel>
        )}

        {/* Only where there is nothing to fall back on. A query that has
            answered once keeps its data when a later read fails, so `isError`
            and `data` are both true after a refetch that git refused — and
            drawing both branches put two regions called "References" on the
            screen at once, which is one landmark too many for anybody moving
            between them by name.

            The list wins that tie, for the reason CommitList states about a
            failed page: a refetch that failed is no reason to take a list off
            the screen that is still on it. The refusal is not lost with it —
            the command, its exit code and its stderr are in the command log,
            which is where every git failure lands whether or not a panel is
            free to draw it. */}
        {refs.isError && refs.data === undefined && (
          <Panel title="References" className="shrink-0">
            <QueryErrorState
              title="Could not read the references"
              error={refs.error}
              compact
              retry={refs}
            />
          </Panel>
        )}

        {refs.data !== undefined && (
          <RefSidebar
            refs={refs.data.refs}
            head={refs.data.head}
            onGoTo={onGoTo}
            onCheckOut={canCheckOut ? ops.checkOut.mutate : undefined}
            checkingOut={ops.checkOut.isPending ? ops.checkOut.variables.ref : undefined}
            onRenameBranch={(branch) => openDialog({ kind: 'rename', branch })}
            onSetUpstream={remotes.length > 0 ? proposals.proposeSetUpstream : undefined}
            onUnsetUpstream={proposals.proposeUnsetUpstream}
            onDeleteBranch={proposals.proposeBranchDelete}
            onMergeBranch={canCheckOut ? proposals.proposeBranchMerge : undefined}
            onRebaseBranch={canCheckOut ? proposals.proposeBranchRebase : undefined}
            onNewBranch={() => openDialog({ kind: 'create' })}
            onNewTag={() => openDialog({ kind: 'create-tag' })}
            onDeleteTag={proposals.proposeTagDelete}
            onPushTag={proposals.proposeTagPush}
            {...(ops.remotes.isFetched && remotes.length === 0
              ? { pushTagUnavailableReason: 'This repository has no remote configured.' }
              : {})}
            // No HEAD is a repository with no commit yet, and `git tag` on one
            // answers "fatal: Failed to resolve 'HEAD' as a valid ref". The
            // button stays and says why, the way every other refusal on this
            // screen does. New branch beside it is NOT refused: git points an
            // unborn HEAD at a new branch quite happily.
            {...(refs.data.head === undefined
              ? { newTagUnavailableReason: 'There is no commit to tag yet.' }
              : {})}
          />
        )}

        {/* Under the references, in the column of things the repository holds
            rather than of things that happened. A bare repository has neither
            a work tree to save nor one to restore into, so it has no panel.

            A direct child of the column, and sized to its own content like
            every panel in it. What used to overflow the page was a column that
            could not scroll, and making the panels give up rows was the
            workaround for that; the column scrolls now, so a panel that keeps
            its rows costs nothing and hides nothing. */}
        {canCheckOut && (
          <StashPanel
            stashes={ops.stash.stashes.data}
            loading={ops.stash.stashes.isPending}
            {...(ops.stash.stashes.error === null ? {} : { error: ops.stash.stashes.error })}
            retry={ops.stash.stashes}
            {...(inspected?.kind === 'stash' ? { selected: inspected.index } : {})}
            onSelect={(chosen) => onInspect({ kind: 'stash', index: chosen.index })}
            onStash={() => proposals.proposeStash()}
            stashing={ops.stash.planPush.isPending && dialog?.kind !== 'stash-push'}
            {...(stashRefusal === undefined ? {} : { stashRefusal })}
            onApply={proposals.proposeStashApply}
            onDrop={proposals.proposeStashDrop}
          />
        )}

        {canCheckOut && (
          <ReflogPanel
            entries={ops.reflog.entries.data?.entries}
            loading={ops.reflog.entries.isPending}
            {...(ops.reflog.entries.error === null ? {} : { error: ops.reflog.entries.error })}
            retry={ops.reflog.entries}
            onRestore={(entry) => proposals.proposeReflogRestore(entry)}
            restoring={
              ops.reflog.planRestore.isPending
                ? ops.reflog.planRestore.variables?.selector
                : undefined
            }
          />
        )}

        {/* Beside the stash, and for the same reason: a worktree is something
            the repository holds. Offered for a bare repository too — it has no
            checkout of its own, and adding one is exactly how somebody gets a
            work tree to look at. */}
        <WorktreePanel
          worktrees={ops.worktrees.list.data?.worktrees}
          loading={ops.worktrees.list.isPending}
          {...(ops.worktrees.list.error === null ? {} : { error: ops.worktrees.list.error })}
          retry={ops.worktrees.list}
          onAdd={() => openDialog({ kind: 'add-worktree', plan: undefined })}
          adding={ops.worktrees.plan.isPending && dialog?.kind !== 'add-worktree'}
          onRemove={proposals.proposeWorktreeRemove}
          onPrune={() => ops.worktrees.prune.mutate()}
          pruning={ops.worktrees.prune.isPending}
        />

        {/* Under the worktrees, and drawn only where there is one: most
            repositories pin nothing, and an empty panel would be one more
            thing to scroll this column past for a sentence saying so. */}
        <SubmodulePanel
          submodules={ops.submodules.list.data?.submodules}
          loading={ops.submodules.list.isPending}
          {...(ops.submodules.list.error === null ? {} : { error: ops.submodules.list.error })}
          retry={ops.submodules.list}
          onUpdate={(path) => ops.submodules.update.mutate(path)}
          updating={ops.submodules.update.isPending}
          onSync={() => ops.submodules.sync.mutate('')}
          onRemove={proposals.proposeSubmoduleRemove}
        />

        {/* Not for a bare repository: `git lfs track` writes .gitattributes
            into a work tree, and one that has none has nowhere to put it. */}
        {!repository.bare && (
          <LFSPanel
            support={ops.lfs.support.data}
            loading={ops.lfs.support.isPending}
            {...(ops.lfs.support.error === null ? {} : { error: ops.lfs.support.error })}
            retry={ops.lfs.support}
            onUntrack={proposals.proposeUntrackLFS}
          />
        )}

        {/* Last, under whichever of those two were drawn, because it is what
            comes after a list rather than a list of its own. It holds the only
            way to start either feature — see RepositoryAdditions for why that
            is not on the panels. */}
        {!repository.bare && (
          <RepositoryAdditions
            lfs={ops.lfs.support.data}
            lfsLoading={ops.lfs.support.isPending}
            lfsPending={
              ops.lfs.plan.isPending &&
              ops.lfs.plan.variables?.action === 'track' &&
              dialog?.kind !== 'track-lfs'
            }
            submodulePending={ops.submodules.plan.isPending && dialog?.kind !== 'add-submodule'}
            onAddSubmodule={() => openDialog({ kind: 'add-submodule', plan: undefined })}
            onTrackLFS={() => openDialog({ kind: 'track-lfs', plan: undefined })}
          />
        )}
      </div>
    </>
  );
}
