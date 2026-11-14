import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';

import { api } from '../api/client';
import type { HistoryScope, Operation, Repository } from '../api/types';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { HistoryIcon, SearchIcon } from '../components/Icons';
import { Panel } from '../components/Panel';
import { Centered, QueryErrorState } from '../components/PanelState';
import { SegmentedControl } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { commitQuery } from './CommitDetails';
import { CommitList, type CommitListHandle } from './CommitList';
import { useWorkbenchDialog } from './dialogSlot';
import { HistoryDialogs } from './HistoryDialogs';
import { InspectedCommit, InspectedPanel } from './HistoryInspector';
import { useHistoryOperations } from './historyOperations';
import { useHistoryProposals } from './historyProposals';
import { HISTORY_SCOPES, initialSelectedRefs } from './historyScope';
import { HistorySidebar } from './HistorySidebar';
import { RefPicker } from './RefPicker';
import { readStoredScope, writeStoredScope } from './sessionStore';
import { useHistoryOverview } from './useHistory';

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
 * The history list's height while something is open underneath.
 *
 * Capped, not flex-grown. Without a ceiling the list competed with the
 * inspect slot for every spare pixel, and the patch — the reason the click
 * happened — lost. A quarter of the column is enough to keep several rows
 * and the sense of place; the floor is a header plus a few commits so a
 * short window still shows something to click next. The inspect slot below
 * takes everything left, which is what makes an ordinary commit's message
 * and patch readable without scrolling.
 */
const HISTORY_WHILE_INSPECTING = 'min-h-44 max-h-[25%] shrink-0';

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
  operation,
}: {
  repository: Repository;
  inspected: Inspected | undefined;
  onInspect: (what: Inspected) => void;
  onClearSelection: () => void;
  /** How many paths differ in the work tree right now. */
  changed: number;
  /** What the work tree is in the middle of, empty when nothing. */
  operation: Operation;
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
  // for exactly that reason.
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
   * The first one costs the list most of its height, because that is when
   * the panel below opens and takes the column — so a row clicked in the
   * lower half of the screen is behind that panel by the time its patch
   * arrives, with aria-current on a row nobody can see and no way back to it
   * but the eye. Following it is the same courtesy goTo already does for a
   * reference, out of the same cached answer: the panel below asks for
   * exactly this query, so the row costs no request of its own.
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
          message and patch want the width the list does not use.

          `min-h-0` is load-bearing. The list's scroller paints a spacer as
          tall as the whole history — hundreds of thousands of pixels on a
          real repository — and without a floor of zero this column's
          automatic minimum becomes that spacer. The flex share then grows
          the inspect slot to match, so a short patch sits at the top of a
          pane you can scroll forever through empty space. The viewport is
          what must bound both halves; the list scrolls inside its own
          panel. */}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3">
        <Panel
          // Full height while nothing is open underneath; a capped strip
          // once something is, so the inspect slot below can be the main
          // object. See HISTORY_WHILE_INSPECTING.
          className={
            inspected === undefined
              ? 'min-h-0 min-w-0 flex-1'
              : `min-w-0 ${HISTORY_WHILE_INSPECTING}`
          }
          icon={<HistoryIcon />}
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
                leading={<SearchIcon />}
                onClick={() => openDialog({ kind: 'search' })}
              >
                Search…
              </Button>
              <Button size="sm" onClick={() => openDialog({ kind: 'compare' })}>
                Compare…
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
              icon={<HistoryIcon size={20} />}
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

        <InspectedPanel
          repositoryId={repository.id}
          inspected={inspected}
          onInspect={onInspect}
          onClose={onClearSelection}
        />

        {selected !== undefined && (
          <InspectedCommit
            repositoryId={repository.id}
            sha={selected}
            scope={walkScope}
            refs={chosenRefs}
            headSha={refs.data?.head?.sha}
            detached={detached}
            canCheckOut={canCheckOut}
            operation={operation}
            ops={ops}
            proposals={proposals}
            dialog={dialog}
            onInspect={onInspect}
            onClose={onClearSelection}
          />
        )}
      </div>

      <HistorySidebar
        repository={repository}
        refs={refs}
        canCheckOut={canCheckOut}
        changed={changed}
        remotes={configuredRemotes}
        ops={ops}
        slot={slot}
        proposals={proposals}
        inspected={inspected}
        onInspect={onInspect}
        onGoTo={goTo}
      />

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
