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
 * The height the panel under the list is given, in one place because all five
 * of them are the same slot and a disagreement between them would be a panel
 * that jumps when the kind of thing open in it changes.
 *
 * Two fifths of the column, with a floor. The ratio on its own reached zero:
 * a commit's header — subject, author, parents, the files badge — is around a
 * hundred and thirty pixels that cannot shrink, so on a short window the part
 * that disappeared was the patch, which is the whole of what reading a commit
 * means. The floor is that header plus a few lines of diff. The list above is
 * `flex-1` over a virtualised scroller, so it is the half that can afford to
 * give the pixels up.
 */
const INSPECT_PANEL = 'h-2/5 min-h-64 shrink-0';

/**
 * The magnifier on the search button.
 *
 * Here rather than in ThemeGlyphs, which is the theme control's own set and
 * named for it; one glyph used in one place is not a shared module yet. It
 * exists because of what sits beside it — a three-segment switch drawn in the
 * same size, weight and colour as a ghost button, which left the door to the
 * search dialog reading as a fourth, unselected setting. A glyph is the one
 * mark a segment of a switch never carries.
 */
function SearchGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <circle cx="6" cy="6" r="4.25" stroke="currentColor" strokeWidth="1.3" />
      <path d="M9.1 9.1 12.6 12.6" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    </svg>
  );
}

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
  const select = (sha: string) => onInspect({ kind: 'commit', sha });

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
    select(sha);
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

  /**
   * A click on a row of the list.
   *
   * The first one costs the list two fifths of its height, because that is
   * when the panel below opens — so a row clicked in the lower half of the
   * screen is behind that panel by the time its patch arrives, with
   * aria-current on a row nobody can see and no way back to it but the eye.
   * Following it is the same courtesy goTo already does for a reference, out
   * of the same cached answer: the panel below asks for exactly this query, so
   * the row costs no request of its own.
   *
   * Only the opening click. With the panel already showing something the list
   * keeps its height and the row keeps its place, and scrolling anyway would
   * move the history under a reader who could see the row perfectly well.
   */
  const selectRow = (sha: string) => {
    if (inspected === undefined) {
      goTo(sha);
      return;
    }
    // And it cancels a jump still in flight, for the reason `wanted` exists at
    // all: the answer to the click before this one can land after it, and a
    // scroll from an older click would take the list away from the row the
    // reader has just chosen.
    wanted.current = undefined;
    select(sha);
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
            // Two things, spaced as two: the door to a dialog, then the
            // controls that decide what the graph is drawn from. Search used
            // to sit inside the same gap as the switch's own parts, in the
            // same ghost type, which made a button that opens a dialog read
            // as a setting that is currently off.
            <div className="flex items-center gap-3">
              <Button
                size="sm"
                variant="ghost"
                leading={<SearchGlyph />}
                onClick={() => openDialog({ kind: 'search' })}
              >
                Search…
              </Button>
              <div className="flex items-center gap-2">
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
              onSelect={selectRow}
            />
          )}
        </Panel>

        {inspected?.kind === 'stash' && (
          <div className={INSPECT_PANEL}>
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
          <div className={INSPECT_PANEL}>
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
          <div className={INSPECT_PANEL}>
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
          <div className={INSPECT_PANEL}>
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
          <div className={INSPECT_PANEL}>
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
      {/* Twenty rem, and it was eighteen. The extra two are what the labels
          in these panel headers now need: every one of them that opens a
          dialog carries an ellipsis, and two of them sit beside a panel title
          in 288 pixels. "References" was the part that gave way — the header
          measured 262 pixels of room, the actions took 186 of it, and the
          title needed 84 of the 64 that were left, so the one word naming the
          panel was drawn as "REFERE…". The column is also where a branch name
          is read, and those truncate first everywhere else too. What it costs
          is 32 pixels of the history beside it, which is a column the commit
          rows stopped needing when they stopped stacking. */}
      <div className="flex w-80 shrink-0 flex-col gap-3 overflow-y-auto">
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
