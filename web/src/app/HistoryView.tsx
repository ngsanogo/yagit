import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';

import { api } from '../api/client';
import type { HistoryScope, Repository } from '../api/types';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { Centered, QueryErrorState } from '../components/PanelState';
import { SegmentedControl } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { BlamePanel } from './Blame';
import { CommitDetails, commitQuery } from './CommitDetails';
import { CommitList, type CommitListHandle } from './CommitList';
import { useWorkbenchDialog } from './dialogSlot';
import { FileHistoryPanel } from './FileHistory';
import { HistoryDialogs } from './HistoryDialogs';
import { useHistoryOperations } from './historyOperations';
import { useHistoryProposals } from './historyProposals';
import { HISTORY_SCOPES, initialSelectedRefs } from './historyScope';
import { LFSPanel } from './LFSPanel';
import { LineHistoryPanel } from './LineHistory';
import { RefPicker } from './RefPicker';
import { RefSidebar } from './RefSidebar';
import { RepositoryAdditions } from './RepositoryAdditions';
import { readStoredScope, writeStoredScope } from './sessionStore';
import { stashPushRefusal } from './stash';
import { StashDetails } from './StashDetails';
import { StashPanel } from './StashPanel';
import { SubmodulePanel } from './SubmodulePanel';
import { checkOutRequestForCommit } from './useCheckOut';
import { useHistoryOverview } from './useHistory';
import { WorktreePanel } from './WorktreePanel';

/**
 * What the panel under the history is showing.
 *
 * One state and not five, because the panel is one slot: a commit and a stash
 * cannot both be open in it, and independent values would let them try. They
 * are different kinds of thing addressed different ways — a commit by its
 * object name, a stash by its POSITION in the stack — which is exactly why the
 * union carries the address rather than a bare string.
 */
export type Inspected =
  | { kind: 'commit'; sha: string }
  | { kind: 'stash'; index: number }
  | { kind: 'file-history'; path: string; revision: string }
  | { kind: 'blame'; path: string; revision: string }
  | { kind: 'line-history'; path: string; revision: string; line: number };

/**
 * The history: the graph and its commits, the references beside them, and what
 * the repository holds that is not a commit.
 *
 * Three things and no more. The operations this screen offers are named once
 * in historyOperations, what a click does with them is written once in
 * historyProposals, and the confirmations they open are drawn in
 * HistoryDialogs. What is left here is the layout and the state that decides
 * it — which refs the walk covers, and what is open in the panel below.
 */
export function HistoryView({
  repository,
  inspected,
  onInspect,
  onClearSelection,
  changed,
}: {
  repository: Repository;
  inspected: Inspected | undefined;
  onInspect: (what: Inspected) => void;
  onClearSelection: () => void;
  /** How many paths differ in the work tree right now. */
  changed: number;
}) {
  // The commit half of the selection, which is what the list highlights and
  // what the panel below reads. A stash is not in the walk, so a stash being
  // inspected leaves the history with nothing marked — which is correct: no
  // row on it is the thing on screen.
  const selected = inspected?.kind === 'commit' ? inspected.sha : undefined;
  const onSelect = (sha: string) => onInspect({ kind: 'commit', sha });

  // The graph is drawn from the current branch until somebody asks for more.
  // Every ref is the picture that does not exist on a repository with enough
  // tags — 280 columns on git's own — and it is a choice rather than a default
  // for exactly that reason (docs/adr/0016).
  const [scope, setScope] = useState<HistoryScope>(
    () => readStoredScope(repository.path) ?? 'head',
  );

  // The refs the third scope walks. Held whichever scope is showing, so
  // leaving "Selected" and coming back returns to the same picture rather than
  // to a choice that has to be made again; the two other scopes ignore it, and
  // the request never carries it under them.
  const [selectedRefs, setSelectedRefs] = useState<readonly string[]>([]);

  // Two independent queries rather than one call for both: the references
  // change far more often than the history does, and asking for them together
  // would refetch a page of commits to learn that a branch moved.
  //
  // The references are read first because the history now depends on them: the
  // walk a chosen set describes is only the refs that are still there.
  const refs = useQuery({
    queryKey: ['refs', repository.id],
    queryFn: () => api.refs(repository.id),
  });

  // A branch can be deleted while the graph is drawn from it, and a walk from
  // a reference that is gone is a walk git refuses. Only what is still listed
  // is asked for.
  const known = refs.data?.refs ?? [];
  const stillThere = selectedRefs.filter((name) => known.some((ref) => ref.name === name));
  const chosenRefs = scope === 'refs' ? stillThere : [];

  // A repository with no reference at all falls back to what is checked out.
  // The daemon refuses `scope=refs` with none — an empty picture is not an
  // empty repository — and a refusal is not what somebody who picked a branch
  // that has since been deleted should be looking at.
  const walkScope: HistoryScope = scope === 'refs' && chosenRefs.length === 0 ? 'head' : scope;

  // The history is read in pages, and this is only its first one — enough to
  // know how long it is and how wide its graph is. The list asks for the pages
  // the scrollbar is actually over, out of the same cache.
  const history = useHistoryOverview(repository.id, walkScope, chosenRefs);

  // Switching to the picker opens it on something rather than on nothing: the
  // branch HEAD is on, so the first click is a narrowing or a widening rather
  // than a repair. See initialSelectedRefs.
  //
  // Asked of what is HELD and still listed, not of chosenRefs — that one is
  // empty under every other scope by construction, so testing it would seed
  // the choice afresh on every return and throw away the set the comment on
  // selectedRefs promises to keep.
  const chooseScope = (next: HistoryScope) => {
    if (next === 'refs' && stillThere.length === 0) {
      setSelectedRefs(initialSelectedRefs(known, refs.data?.head?.name));
    }
    setScope(next);
    writeStoredScope(repository.path, next);
  };

  const list = useRef<CommitListHandle>(null);
  const queryClient = useQueryClient();

  // One slot for every confirmation on this screen — branch, cherry-pick,
  // revert, reset and the three stash questions alike — so two modals never
  // stack. See useWorkbenchDialog.
  const slot = useWorkbenchDialog();
  const { dialog, open: openDialog } = slot;

  const ops = useHistoryOperations(repository.id, repository.bare);
  const configuredRemotes = ops.remotes.data ?? [];
  const proposals = useHistoryProposals(slot, ops, {
    refs: known,
    remotes: configuredRemotes,
    remotesFetched: ops.remotes.isFetched,
  });

  // A bare repository has no work tree to check anything out into. The daemon
  // refuses the request; not offering it is the difference between a screen
  // that shows what applies and one that shows an error nobody caused.
  const canCheckOut = !repository.bare;

  // A detached HEAD is a state and not a shape, so the buttons stay and are
  // told why — the same line the branch menu draws for merge and rebase. A
  // bare repository is the other case, and it loses the button entirely.
  const detached = refs.data?.head?.detached === true;
  const whileDetached = (what: string) => (detached ? `HEAD is detached, so ${what}` : undefined);

  // Not a detached-HEAD refusal, unlike those: `git stash` works perfectly well
  // without a branch and records "(no branch)". What it cannot do is save a
  // work tree that matches HEAD.
  const stashRefusal = stashPushRefusal(changed);

  // The commit last asked for, read back when the daemon answers. Two clicks
  // resolve in whatever order git finishes them: a reference already in the
  // cache answers while an uncached one is still walking the log, and the
  // slower answer would then scroll the history away from the commit the panel
  // and the highlight are both showing.
  const wanted = useRef<string | undefined>(undefined);

  /**
   * Follows a reference: selects the commit it names, and takes the list to
   * the row it sits on.
   *
   * The row comes from the daemon, through the same query the panel below
   * reads, so one click costs one request. It is a row in THIS scope's walk —
   * a branch nobody has merged has none under the default one, and the daemon
   * says so in words the panel shows rather than scrolling somewhere wrong.
   */
  const goTo = (sha: string) => {
    wanted.current = sha;
    onSelect(sha);
    queryClient
      .fetchQuery(commitQuery(repository.id, sha, walkScope, chosenRefs))
      .then((located) => {
        if (wanted.current === sha) {
          list.current?.scrollToRow(located.row);
        }
      })
      .catch(() => {
        // Not swallowed: this is the panel's own query, and the panel renders
        // the failure whole — message, command, exit code, stderr — in the
        // place the commit was going to appear. Reporting it here as well
        // would put the same news on screen twice.
      });
  };

  return (
    <div className="flex min-h-0 flex-1 gap-3 p-3">
      {/* The list above, the chosen commit below it. Stacked rather than in a
          third column: the references stay visible either way, and a commit's
          message and patch want the width the list does not use. */}
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        <Panel
          className="min-w-0 flex-1"
          title={`History${history.isPending || history.error ? '' : ` — ${history.total}`}`}
          actions={
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" onClick={() => openDialog({ kind: 'search' })}>
                Search…
              </Button>
              <SegmentedControl
                label="Refs the graph is drawn from"
                segments={HISTORY_SCOPES}
                value={scope}
                onChange={chooseScope}
              />
              {/* Beside the switch rather than inside it: which references
                  were picked is a second question, asked only of the scope
                  that reads them. */}
              {scope === 'refs' && (
                <RefPicker refs={known} selected={chosenRefs} onChange={setSelectedRefs} />
              )}
            </div>
          }
          flush
        >
          {history.isPending && (
            <Centered>
              <Spinner label="Reading the history" />
            </Centered>
          )}

          {history.error && (
            <Centered>
              <QueryErrorState title="Could not read the history" error={history.error} />
            </Centered>
          )}

          {!history.isPending && !history.error && history.total === 0 && (
            <EmptyState
              title="No commits yet"
              description="This repository has no history. Make a commit and it will appear here."
            />
          )}

          {!history.isPending && !history.error && history.total > 0 && (
            <CommitList
              ref={list}
              repositoryId={repository.id}
              total={history.total}
              columns={history.width}
              pageSize={history.pageSize}
              scope={walkScope}
              refs={chosenRefs}
              selected={selected}
              onSelect={onSelect}
            />
          )}
        </Panel>

        {inspected?.kind === 'stash' && (
          <div className="h-2/5 min-h-0 shrink-0">
            <StashDetails
              // Keyed by the position, so choosing another stash starts from
              // nothing rather than showing the previous one's patch under the
              // new one's header while it loads.
              key={inspected.index}
              repositoryId={repository.id}
              index={inspected.index}
              onClose={onClearSelection}
            />
          </div>
        )}

        {inspected?.kind === 'file-history' && (
          <div className="h-2/5 min-h-0 shrink-0">
            <FileHistoryPanel
              key={`${inspected.path}@${inspected.revision}`}
              repositoryId={repository.id}
              path={inspected.path}
              revision={inspected.revision}
              onClose={onClearSelection}
              onSelectCommit={(sha) => onInspect({ kind: 'commit', sha })}
            />
          </div>
        )}

        {inspected?.kind === 'blame' && (
          <div className="h-2/5 min-h-0 shrink-0">
            <BlamePanel
              key={`${inspected.path}@${inspected.revision}`}
              repositoryId={repository.id}
              path={inspected.path}
              revision={inspected.revision}
              onClose={onClearSelection}
              onSelectCommit={(sha) => onInspect({ kind: 'commit', sha })}
              onLineHistory={(line) =>
                onInspect({
                  kind: 'line-history',
                  path: inspected.path,
                  revision: inspected.revision,
                  line,
                })
              }
            />
          </div>
        )}

        {inspected?.kind === 'line-history' && (
          <div className="h-2/5 min-h-0 shrink-0">
            <LineHistoryPanel
              key={`${inspected.path}:${inspected.line}@${inspected.revision}`}
              repositoryId={repository.id}
              path={inspected.path}
              line={inspected.line}
              revision={inspected.revision}
              onClose={onClearSelection}
              onSelectCommit={(sha) => onInspect({ kind: 'commit', sha })}
            />
          </div>
        )}

        {selected !== undefined && (
          <div className="h-2/5 min-h-0 shrink-0">
            <CommitDetails
              // Keyed by the commit, so choosing another one starts from
              // nothing rather than showing the previous commit's patch under
              // the new one's message while it loads.
              key={selected}
              repositoryId={repository.id}
              sha={selected}
              scope={walkScope}
              refs={chosenRefs}
              onClose={onClearSelection}
              onCheckOut={
                canCheckOut
                  ? () => ops.checkOut.mutate(checkOutRequestForCommit(selected))
                  : undefined
              }
              checkingOut={ops.checkOut.isPending && ops.checkOut.variables.ref === selected}
              onCherryPick={canCheckOut ? () => proposals.proposeCherryPick(selected) : undefined}
              cherryPicking={
                ops.cherryPick.plan.isPending && ops.cherryPick.plan.variables === selected
              }
              cherryPickRefusal={whileDetached('there is no branch to cherry-pick onto')}
              onRevert={canCheckOut ? () => proposals.proposeRevert(selected) : undefined}
              reverting={ops.revert.plan.isPending && ops.revert.plan.variables === selected}
              revertRefusal={whileDetached('there is no branch to revert on')}
              onReset={canCheckOut ? () => proposals.proposeReset(selected) : undefined}
              resetting={
                ops.reset.plan.isPending &&
                ops.reset.plan.variables?.commit === selected &&
                dialog?.kind !== 'reset'
              }
              resetRefusal={whileDetached('there is no branch to reset')}
              onRewrite={canCheckOut ? () => proposals.proposeRewrite(selected) : undefined}
              rewriting={ops.rewrite.plan.isPending && ops.rewrite.plan.variables === selected}
              rewriteRefusal={whileDetached('there is no branch whose commits could be rewritten')}
              onFileHistory={(path) =>
                onInspect({ kind: 'file-history', path, revision: selected })
              }
              onBlame={(path) => onInspect({ kind: 'blame', path, revision: selected })}
            />
          </div>
        )}
      </div>

      {/* A flex column, and it has to be one. The panel inside asks for
          `flex-1` and `min-h-0`, and the list inside that for `h-full
          overflow-auto` — none of which means anything in a plain block, where
          the height is whatever the content comes to. The references then grew
          the page instead of scrolling: yagit's own repository already
          overflowed a 900-pixel window by five hundred, and the repository
          this application exists to draw has a thousand tags. */}
      <div className="flex w-72 shrink-0 flex-col gap-3">
        {refs.isPending && (
          <Panel title="References" className="h-full">
            <Centered compact>
              <Spinner label="Reading the references" />
            </Centered>
          </Panel>
        )}

        {refs.isError && (
          <Panel title="References" className="h-full">
            <QueryErrorState title="Could not read the references" error={refs.error} compact />
          </Panel>
        )}

        {refs.data !== undefined && (
          <RefSidebar
            refs={refs.data.refs}
            head={refs.data.head}
            onGoTo={goTo}
            onCheckOut={canCheckOut ? ops.checkOut.mutate : undefined}
            checkingOut={ops.checkOut.isPending ? ops.checkOut.variables.ref : undefined}
            onRenameBranch={(branch) => openDialog({ kind: 'rename', branch })}
            onSetUpstream={configuredRemotes.length > 0 ? proposals.proposeSetUpstream : undefined}
            onUnsetUpstream={proposals.proposeUnsetUpstream}
            onDeleteBranch={proposals.proposeBranchDelete}
            onMergeBranch={canCheckOut ? proposals.proposeBranchMerge : undefined}
            onRebaseBranch={canCheckOut ? proposals.proposeBranchRebase : undefined}
            onNewBranch={() => openDialog({ kind: 'create' })}
            onNewTag={() => openDialog({ kind: 'create-tag' })}
            onDeleteTag={proposals.proposeTagDelete}
            onPushTag={proposals.proposeTagPush}
            {...(ops.remotes.isFetched && configuredRemotes.length === 0
              ? { pushTagUnavailableReason: 'This repository has no remote configured.' }
              : {})}
          />
        )}

        {/* Under the references, in the column of things the repository holds
            rather than of things that happened. A bare repository has neither
            a work tree to save nor one to restore into, so it has no panel.

            A direct child of the column, so the flex algorithm can shrink it:
            wrapped in a shrink-0 box it was the page that overflowed instead
            of the panel. */}
        {canCheckOut && (
          <StashPanel
            stashes={ops.stash.stashes.data}
            loading={ops.stash.stashes.isPending}
            {...(ops.stash.stashes.error === null ? {} : { error: ops.stash.stashes.error })}
            {...(inspected?.kind === 'stash' ? { selected: inspected.index } : {})}
            onSelect={(chosen) => onInspect({ kind: 'stash', index: chosen.index })}
            onStash={() => proposals.proposeStash()}
            stashing={ops.stash.planPush.isPending && dialog?.kind !== 'stash-push'}
            {...(stashRefusal === undefined ? {} : { stashRefusal })}
            onApply={proposals.proposeStashApply}
            onDrop={proposals.proposeStashDrop}
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
          onAdd={() => openDialog({ kind: 'add-worktree', plan: undefined })}
          adding={ops.worktrees.plan.isPending && dialog?.kind !== 'add-worktree'}
          onRemove={proposals.proposeWorktreeRemove}
          onPrune={() => ops.worktrees.prune.mutate()}
          pruning={ops.worktrees.prune.isPending}
        />

        {/* Under the worktrees, and drawn only where there is one: most
            repositories pin nothing, and an empty panel would cost the
            references height for a sentence saying so. */}
        <SubmodulePanel
          submodules={ops.submodules.list.data?.submodules}
          loading={ops.submodules.list.isPending}
          {...(ops.submodules.list.error === null ? {} : { error: ops.submodules.list.error })}
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

      <HistoryDialogs
        slot={slot}
        ops={ops}
        proposals={proposals}
        repository={repository}
        refs={known}
        head={refs.data?.head}
        remotes={configuredRemotes}
        walk={{ scope: walkScope, refs: chosenRefs, goTo }}
        target={selected}
      />
    </div>
  );
}
