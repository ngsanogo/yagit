import { useState } from 'react';

import type {
  ConflictSide,
  FileStatus,
  LineSelection,
  Repository,
  WorkingDirectory,
} from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { SegmentedControl } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { useToast } from '../components/ToastHost';
import { ApiError } from '../api/client';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { pluralize } from '../lib/format';
import { ChangeList, type Selection } from './ChangeList';
import { CommitBox } from './CommitBox';
import { DiffView, type DiffAction } from './DiffView';
import { FileEditor } from './FileEditor';
import { isOnDisk, sideOf, useFileDiff, useWorktreeOperations } from './useWorkingDirectory';

/**
 * The working directory: what differs, what it looks like, and the message
 * that will record it.
 *
 * The files on the left and the diff on the right, which is the only
 * arrangement that lets a file be chosen and read at once. Depth stays at two —
 * a repository tab, then this — and there is no third pane: the reference list
 * belongs to the history, where the graph is the main object.
 */

interface ChangesViewProps {
  repository: Repository;
  status: WorkingDirectory;
  /**
   * What the user has chosen and written so far, held one level up.
   *
   * Switching to the history unmounts this view, and a draft or a chosen file
   * kept here would not survive the round trip the switch invites.
   */
  selected: Selection | undefined;
  onSelect: (selected: Selection) => void;
  /**
   * The message so far, or nothing when the box has not been touched.
   *
   * `undefined` rather than `''`: an empty string is somebody who cleared the
   * box and meant it, and nothing at all is a box git may fill in — see
   * CommitBox.
   */
  draft: string | undefined;
  onDraftChange: (draft: string | undefined) => void;
}

export function ChangesView({
  repository,
  status,
  selected,
  onSelect,
  draft,
  onDraftChange,
}: ChangesViewProps) {
  const [pendingDiscard, setPendingDiscard] = useState<DiscardRequest>();

  /**
   * Which face of the chosen file the right pane shows.
   *
   * A preference and not a fact about the file: it survives moving between
   * rows, because somebody who opened the editor to fix one typo usually has a
   * second one to fix.
   */
  const [pane, setPane] = useState<'diff' | 'edit'>('diff');

  const toast = useToast();
  const operations = useWorktreeOperations(repository.id);

  const staged = status.files.filter((file) => file.staged);
  const unstaged = status.files.filter((file) => file.unstaged);

  // A third list, cut out of the unstaged one.
  //
  // A conflicted file is unstaged — it differs from the index, which holds
  // three versions of it — so it belongs there by the same rule as everything
  // else, and it does not belong there at all. It cannot be discarded (`git
  // restore` refuses an unmerged path), its diff is a shape nothing here
  // reads, and nothing else in the list can stop a commit. Putting it under
  // "Changed" with the rest is the panel filing an emergency under
  // housekeeping.
  const conflicted = status.files.filter((file) => file.kind === 'unmerged');
  const changed = unstaged.filter((file) => file.kind !== 'unmerged');

  // The row the user picked may be gone — staged, discarded, committed. The
  // file is then looked for in the other list before the pane is emptied: a
  // file staged from the left is still on screen on the right, and clearing
  // the diff at that moment is the interface losing the user's place.
  //
  // The WHOLE unstaged list is what a selection is resolved against, not the
  // `changed` slice above: the conflicted rows are drawn separately but they
  // are still `unstaged` rows, and a lookup that missed them would empty the
  // pane the moment somebody clicked the file they most needed to see.
  const current = resolveSelection(selected, staged, unstaged);
  const side = current === undefined ? 'unstaged' : sideOf(current.file, current.row);

  const busy =
    operations.stage.isPending ||
    operations.unstage.isPending ||
    operations.discard.isPending ||
    operations.discardPlan.isPending ||
    operations.save.isPending ||
    operations.resolve.isPending;

  const editingConflict = current?.file.kind === 'unmerged';
  const editable = current !== undefined && isOnDisk(current.file);
  const showing = paneFor(editingConflict, editable, pane);

  // Only while a diff is what the pane is drawing. See the `enabled` note on
  // useFileDiff: a conflicted path has no diff to ask for, and asking anyway
  // buys a refusal every two seconds that nothing renders.
  const diff = useFileDiff(repository.id, current?.file.path, side, showing === 'diff');

  function report(action: string, error: Error) {
    toast.push({
      tone: 'danger',
      // The heading is the daemon's when the daemon knows something the
      // message does not carry. "Could not stage" over a 413 tells the user
      // the click failed and nothing about the one thing that would let them
      // succeed: stage the file rather than its lines.
      title: refusalHeading(error) ?? `Could not ${action}`,
      detail: errorDescription(error),
    });
  }

  function move(operation: 'stage' | 'unstage', paths: string[], lines?: LineSelection) {
    operations[operation].mutate(
      { paths, ...(lines === undefined ? {} : { lines }) },
      { onError: (error) => report(operation, error) },
    );
  }

  /**
   * Puts the discard question, once the daemon has said what answering yes
   * would run.
   *
   * The dialog opens on the answer rather than beside it. Its whole purpose is
   * to show the exact command, and a dialog that opened first would have to
   * draw a placeholder where that command goes — which is the same as not
   * showing it, for as long as it is on screen.
   */
  function proposeDiscard(files: FileStatus[], lines?: LineSelection) {
    operations.discardPlan.mutate(
      { paths: files.map((file) => file.path), ...(lines === undefined ? {} : { lines }) },
      {
        onSuccess: ({ commands }) => {
          const [first, ...rest] = commands;
          if (first === undefined) {
            // The daemon names at least one command for any selection it
            // accepted. An empty list would open a confirmation promising
            // nothing, which is worse than not asking at all.
            report(
              'read what discarding would run',
              new Error('the daemon named no command for this discard'),
            );
            return;
          }
          setPendingDiscard({
            files,
            ...(lines === undefined ? {} : { lines }),
            commands: [first, ...rest],
          });
        },
        onError: (error) => report('read what discarding would run', error),
      },
    );
  }

  /**
   * Runs a line action, and says whether it went.
   *
   * A discard is proposed rather than run — it is the one operation nothing
   * can undo — and the answer is what tells the diff to keep the selection
   * while the dialog is open. Throwing it away at the moment the question is
   * asked would punish the user for saying no to the one dialog that exists to
   * let them.
   */
  /**
   * Takes one side of a conflict for whole files, through git.
   *
   * Two commands and not one — `git checkout --ours`, then `git add` — and the
   * daemon runs both: the checkout writes the file and only the add collapses
   * the index stages that make git call it unmerged. It is also the daemon
   * that decides which of the two commands a path actually needs, because the
   * side somebody keeps may be the side that has no file at all.
   */
  function keepWholeFile(paths: string[], side: ConflictSide) {
    operations.resolve.mutate(
      { paths, side },
      { onError: (error) => report(`take the ${side} version`, error) },
    );
  }

  function applyToDiff(action: DiffAction, indices: number[]): boolean {
    if (current === undefined || diff.data === undefined) {
      return false;
    }
    const lines: LineSelection = { diff: diff.data.id, indices };

    if (action === 'discard') {
      proposeDiscard([current.file], lines);
      return false;
    }
    move(action, [current.file.path], lines);
    return true;
  }

  return (
    <div className="flex min-h-0 flex-1 gap-3 p-3">
      <Panel
        className="w-96 shrink-0"
        title={changesTitle(staged.length, changed.length, conflicted.length)}
        flush
      >
        <div className="flex h-full min-h-0 flex-col">
          <div className="min-h-0 flex-1 overflow-auto">
            {status.files.length === 0 ? (
              <EmptyState
                title="Nothing to commit"
                description="The working directory matches the last commit. Edit a file and it will appear here."
              />
            ) : (
              <>
                {/* First, because it is what stops everything else. */}
                <ChangeList
                  title="Conflicted"
                  row="unstaged"
                  files={conflicted}
                  selected={selected}
                  onSelect={onSelect}
                  onMove={(paths) => move('stage', paths)}
                  // Not "Stage". It IS `git add`, and `git add` on an unmerged
                  // path does something staging does not: it collapses the
                  // three index stages into one, which is the act that ends
                  // the conflict. The button says what it accomplishes.
                  moveLabel="Mark resolved"
                  busy={busy}
                />
                <ChangeList
                  title="Staged"
                  row="staged"
                  files={staged}
                  selected={selected}
                  onSelect={onSelect}
                  onMove={(paths) => move('unstage', paths)}
                  moveLabel="Unstage"
                  busy={busy}
                />
                <ChangeList
                  title="Changed"
                  row="unstaged"
                  files={changed}
                  selected={selected}
                  onSelect={onSelect}
                  onMove={(paths) => move('stage', paths)}
                  moveLabel="Stage"
                  onDiscard={(files) => proposeDiscard(files)}
                  busy={busy}
                />
              </>
            )}
          </div>

          <CommitBox
            repositoryId={repository.id}
            status={status}
            draft={draft}
            onDraftChange={onDraftChange}
            busy={operations.commit.isPending}
            error={operations.commit.error}
            onCommit={(message, amend, onRecorded) => {
              operations.commit.mutate(
                { message, amend },
                {
                  // Only here. Until git has answered, the message the user
                  // wrote is the only copy of it that exists.
                  onSuccess: () => {
                    onRecorded();
                    toast.push({ tone: 'success', title: amend ? 'Commit amended' : 'Committed' });
                  },
                },
              );
            }}
          />
        </div>
      </Panel>

      {/* The panel is titled "Diff", not "parser.go": a panel title is set in
          capitals and a path is case-sensitive. The path is inside the view,
          in the case the filesystem actually uses. */}
      <Panel
        className="min-w-0 flex-1"
        title={paneTitle(showing)}
        actions={
          editable && !editingConflict ? (
            <SegmentedControl
              label="What to show of this file"
              value={pane}
              onChange={setPane}
              segments={[
                { value: 'diff', label: 'Diff' },
                { value: 'edit', label: 'Edit' },
              ]}
            />
          ) : undefined
        }
        flush
      >
        {current === undefined ? (
          <EmptyState
            title="No file chosen"
            description="Pick a file on the left to see what changed in it, and to choose which of those changes to keep."
          />
        ) : showing === 'gone' ? (
          <MissingConflictFile
            file={current.file}
            busy={busy}
            onResolve={() => keepWholeFile([current.file.path], 'ours')}
          />
        ) : showing === 'edit' ? (
          <FileEditor
            // Keyed by path, so moving to another row starts from that file
            // rather than showing the previous one's text under the new one's
            // name while it loads.
            key={current.file.path}
            repositoryId={repository.id}
            file={current.file}
            busy={busy}
            operation={status.state.operation}
            onSave={(path, text, base, onSaved) => {
              operations.save.mutate(
                { path, text, base },
                { onSuccess: onSaved, onError: (error) => report('save that file', error) },
              );
            }}
            onKeepWholeFile={(path, sideKept) => keepWholeFile([path], sideKept)}
            onStage={(path) => move('stage', [path])}
          />
        ) : diff.isPending ? (
          <div className="grid h-full place-items-center">
            <Spinner label="Reading the diff" />
          </div>
        ) : diff.isError ? (
          <DiffFailure error={diff.error} />
        ) : (
          <DiffView diff={diff.data} side={side} busy={busy} onApply={applyToDiff} />
        )}
      </Panel>

      <DiscardDialog
        request={pendingDiscard}
        busy={operations.discard.isPending}
        onCancel={() => setPendingDiscard(undefined)}
        onConfirm={(request) => {
          setPendingDiscard(undefined);
          operations.discard.mutate(
            {
              paths: request.files.map((file) => file.path),
              ...(request.lines === undefined ? {} : { lines: request.lines }),
            },
            { onError: (error) => report('discard those changes', error) },
          );
        }}
      />
    </div>
  );
}

/** Which face of the chosen file the right pane draws. */
type Pane = 'edit' | 'diff' | 'gone';

/**
 * A conflicted file gets no say in this, and that is git's answer rather than
 * a gap here: `git diff` on an unmerged path produces a COMBINED diff, three
 * versions wide, which no patch can be built from and which the daemon refuses
 * by name. The editor is the only view of such a file.
 *
 * Only while there IS a file, though. Both sides of a merge can delete the
 * same path, and forcing the editor onto that row asks the daemon for
 * something that is not on disk and draws its 404 — a blank pane with a raw
 * error in it, in answer to a row the panel invited the user to click.
 */
function paneFor(conflicted: boolean, editable: boolean, preference: 'diff' | 'edit'): Pane {
  if (conflicted) {
    return editable ? 'edit' : 'gone';
  }
  return editable ? preference : 'diff';
}

function paneTitle(showing: Pane): string {
  switch (showing) {
    case 'edit':
      return 'Edit';
    case 'gone':
      return 'Conflict';
    case 'diff':
      return 'Diff';
  }
}

/**
 * A conflict with no file to open.
 *
 * git leaves something in the work tree for every conflict but one: the merged
 * text with markers when both sides changed the file, and whichever side kept
 * it when only one did. "Both deleted" leaves nothing, and it is still a row in
 * the list and still stops the commit — so the pane says what the state is and
 * offers the one thing that ends it.
 *
 * That is a command rather than an edit. Neither side has a version to check
 * out, so `git rm` is the resolution: it collapses the three index stages into
 * one, which is what the daemon runs for a side with no content.
 */
function MissingConflictFile({
  file,
  busy,
  onResolve,
}: {
  file: FileStatus;
  busy: boolean;
  onResolve: () => void;
}) {
  return (
    <EmptyState
      title="Nothing on disk to edit"
      description={`git calls ${file.path} "${file.conflict ?? 'unmerged'}", so there is no version of it in the work tree to open. Recording the deletion is what ends the conflict.`}
      action={
        <Button variant="secondary" size="sm" disabled={busy} onClick={onResolve}>
          Record the deletion
        </Button>
      }
    />
  );
}

/**
 * Why a diff could not be read.
 *
 * The daemon tells three cases apart and used to be answered with one screen
 * for all of them. A diff past the cap is a 413 carrying git's words about a
 * command that was stopped, and the file can still be staged whole — the one
 * sentence that matters, and the one nothing was saying. A path git no longer
 * lists on the side this pane asked for is a 404, which is what a row becomes
 * when it is staged from another window between being picked and being read.
 *
 * The daemon's own message stays underneath either way: it names the file and
 * the command, and it is the authority on what happened.
 */
function DiffFailure({ error }: { error: Error }) {
  const detail = errorDescription(error);

  const tooLarge = refusalHeading(error);
  if (tooLarge !== undefined) {
    return (
      <EmptyState
        title={tooLarge}
        description="The daemon stopped git rather than hold the whole of it, so there are no lines to choose between. Staging or discarding the whole file still works — it is git add, and needs no patch."
        detail={detail}
      />
    );
  }

  if (error instanceof ApiError && error.status === 404) {
    return (
      <EmptyState
        title="No diff on this side"
        description="git no longer reports this file the way the list did when it was picked. The list refreshes every couple of seconds; pick it again once it has."
        detail={detail}
      />
    );
  }

  return <EmptyState title="Could not read the diff" description="" detail={detail} />;
}

/** What a pending discard is about to throw away, and how. */
interface DiscardRequest {
  files: FileStatus[];
  /** Present when only part of one file is being discarded. */
  lines?: LineSelection;
  /**
   * The commands the daemon answered with, shown verbatim.
   *
   * Not composed here, and that is the point: the paths reach git as
   * `:(literal)` pathspecs and the split between `git restore` and `git clean`
   * follows a status this side never reads. A line written here would be a
   * second definition of what runs, and a second definition is exactly what
   * drifted the first time.
   */
  commands: [string, ...string[]];
}

/**
 * The confirmation for the one operation nothing can undo.
 *
 * What it removes was never committed and is in no reflog, so this dialog
 * names the files and shows the exact commands — and there are two of them
 * whenever the selection mixes tracked files with ones git has never seen,
 * because `git restore` cannot remove the second kind and `git clean` would
 * refuse the first. Which is which, and how each path is spelled to git, is
 * the daemon's answer: see DiscardRequest.commands.
 */
function DiscardDialog({
  request,
  busy,
  onCancel,
  onConfirm,
}: {
  request: DiscardRequest | undefined;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (request: DiscardRequest) => void;
}) {
  if (request === undefined) {
    return null;
  }

  return (
    <ConfirmDialog
      open
      destructive
      onCancel={onCancel}
      onConfirm={() => onConfirm(request)}
      title={request.lines === undefined ? 'Discard these changes?' : 'Discard these lines?'}
      command={request.commands}
      losing={losingFrom(request)}
      confirmLabel="Discard"
      busy={busy}
    />
  );
}

function losingFrom(request: DiscardRequest): [string, ...string[]] {
  if (request.lines !== undefined) {
    const file = request.files[0];
    return [`${pluralize(request.lines.indices.length, 'line')} of ${file?.path ?? 'this file'}`];
  }

  const named = request.files.slice(0, 5).map(describeLoss);
  if (request.files.length > named.length) {
    named.push(`and ${pluralize(request.files.length - named.length, 'other file')}`);
  }
  return named as [string, ...string[]];
}

function describeLoss(file: FileStatus): string {
  if (file.kind === 'untracked') {
    // Not the same loss at all, and the wording has to say so: the file
    // itself goes, not merely the changes to it.
    return `${file.path} — the file itself, which git has never seen`;
  }
  return `the changes to ${file.path}`;
}

function changesTitle(staged: number, changed: number, conflicted: number): string {
  if (staged === 0 && changed === 0 && conflicted === 0) {
    return 'Changes';
  }
  // The conflicts lead when there are any: they are the only count in this
  // title that stops a commit, and a title that buried them among the others
  // would be sorting by arithmetic rather than by what matters.
  const parts = [
    ...(conflicted > 0 ? [`${conflicted} conflicted`] : []),
    `${staged} staged`,
    `${changed} unstaged`,
  ];
  return `Changes — ${parts.join(', ')}`;
}

/**
 * Turns the row the user clicked into the file it still points at.
 *
 * Staging a file moves it from one list to the other, and the click that moved
 * it was a click on a row that no longer exists. Following the path into the
 * other list keeps the diff on screen through the operation the user just
 * performed, which is when they most want to see it.
 */
export function resolveSelection(
  selected: Selection | undefined,
  staged: FileStatus[],
  unstaged: FileStatus[],
): { file: FileStatus; row: Selection['row'] } | undefined {
  if (selected === undefined) {
    return undefined;
  }

  const lists = { staged, unstaged } as const;
  const preferred = lists[selected.row].find((file) => file.path === selected.path);
  if (preferred !== undefined) {
    return { file: preferred, row: selected.row };
  }

  const other = selected.row === 'staged' ? 'unstaged' : 'staged';
  const fallback = lists[other].find((file) => file.path === selected.path);
  if (fallback !== undefined) {
    return { file: fallback, row: other };
  }

  return undefined;
}
