import { useEffect, useMemo, useRef, useState } from 'react';

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
import { cx } from '../lib/cx';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { pluralize } from '../lib/format';
import { ChangeList, type Selection } from './ChangeList';
import { CommitBox } from './CommitBox';
import { DiffView, type DiffAction } from './DiffView';
import { FileEditor, KeepWholeFileDialog } from './FileEditor';
import { isOnDisk, sideOf, useFileDiff, useWorktreeOperations } from './useWorkingDirectory';

/**
 * The work tree: what differs, what it looks like, and the message that will
 * record it.
 *
 * The files on the left and the diff on the right, which is the only
 * arrangement that lets a file be chosen and read at once. Depth stays at two —
 * a repository tab, then this — and there is no third pane: the reference list
 * belongs to the history, where the graph is the main object.
 */

/**
 * How many files it takes before the panel offers to filter them.
 *
 * A search box above three rows costs more attention than the list it filters,
 * and a vendored drop or a formatter run puts hundreds in it — where dragging
 * a scrollbar three percent tall is the only way to find one path. Twelve is
 * about where the list stops fitting on a short window, which is the point at
 * which scrolling starts being the answer to a question.
 *
 * The box stays once it is on screen and holding something, whatever the count
 * does: a filter that vanished mid-typing because it had narrowed the list
 * below the threshold would leave a panel hiding files with no visible reason.
 */
const FILTER_FROM = 12;

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
   * The conflicted file whose deletion is being proposed, or nothing.
   *
   * The other half of the same question the editor asks. Only a conflict with
   * no file on disk reaches it — see MissingConflictFile — and the button that
   * opens it runs `git rm`, so it goes through the confirmation the editor's
   * whole-file buttons go through rather than round it.
   */
  const [pendingDeletion, setPendingDeletion] = useState<FileStatus>();

  /**
   * Which face of the chosen file the right pane shows.
   *
   * A preference and not a fact about the file: it survives moving between
   * rows, because somebody who opened the editor to fix one typo usually has a
   * second one to fix.
   */
  const [pane, setPane] = useState<'diff' | 'edit'>('diff');

  /**
   * What the list is narrowed to, over the whole path.
   *
   * On the list and not on the query: `git status` is answered in a few
   * milliseconds for the whole work tree and is already cached, so asking the
   * daemon for a subset of an answer it has just given would spend a request
   * per keystroke to draw fewer rows.
   */
  const [filter, setFilter] = useState('');

  const toast = useToast();
  const operations = useWorktreeOperations(repository.id);

  /*
   * The four lists the panel is made of, derived once per status rather than
   * once per render.
   *
   * `status.files` keeps its identity between polls — the query only builds a
   * new array when the answer actually changed — so memoising here is what
   * carries that identity down to everything derived from it. Filtered afresh
   * each render, five hundred rows were re-filtered on every keystroke in the
   * filter box, every diff arrival and every two-second poll; worse, each pass
   * handed the lists below a new array, which is a dependency that changes
   * whether or not anything did. The auto-open effect below is the one that
   * paid for it, and ChangeList's own effects read `files` the same way.
   */
  const staged = useMemo(() => status.files.filter((file) => file.staged), [status.files]);
  const unstaged = useMemo(() => status.files.filter((file) => file.unstaged), [status.files]);

  // A third list, cut out of the unstaged one.
  //
  // A conflicted file is unstaged — it differs from the index, which holds
  // three versions of it — so it belongs there by the same rule as everything
  // else, and it does not belong there at all. It cannot be discarded (`git
  // restore` refuses an unmerged path), its diff is a shape nothing here
  // reads, and nothing else in the list can stop a commit. Putting it under
  // "Changed" with the rest is the panel filing an emergency under
  // housekeeping.
  const conflicted = useMemo(
    () => status.files.filter((file) => file.kind === 'unmerged'),
    [status.files],
  );
  const changed = useMemo(() => unstaged.filter((file) => file.kind !== 'unmerged'), [unstaged]);

  /*
   * What the filter narrows, and what it deliberately does not.
   *
   * The rows, and only the rows. The panel's title goes on counting the whole
   * work tree — it is a fact about the repository rather than about the box —
   * and a selection is resolved against the whole lists below, so typing in
   * the filter never empties the pane on the right.
   *
   * The section buttons DO follow it: "Stage all" over a filtered list stages
   * what is listed, which is both what the rows in front of the user say and
   * the safe direction for the discard beside it. Their labels count the same
   * files, so what a screen reader hears is what is on screen.
   */
  const query = filter.trim().toLowerCase();
  const listed = (files: FileStatus[]) =>
    query === '' ? files : files.filter((file) => file.path.toLowerCase().includes(query));
  const matches = listed(status.files).length;
  const showFilter = status.files.length >= FILTER_FROM || filter !== '';

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

  /*
   * The pane on the right is three quarters of the screen, and entering this
   * view used to leave all of it saying "No file chosen". The commonest state
   * of the screen is one to three changed files, and in every one of them the
   * first click is a foregone conclusion the user was made to perform in front
   * of the pane that would have answered their question.
   *
   * Once, and only into an empty selection. A second automatic choice — after
   * a commit empties the list, say — would be the panel taking the pointer
   * back from somebody who had just put it down. What it costs is one diff
   * request on entry, which for a lockfile as the first row is a real one; the
   * daemon's cap and the drawn-line limit both still apply to it.
   */
  const opening = useMemo(
    () => firstToShow(conflicted, changed, staged),
    [conflicted, changed, staged],
  );
  const opened = useRef(false);
  useEffect(() => {
    if (opened.current || selected !== undefined || opening === undefined) {
      return;
    }
    opened.current = true;
    onSelect(opening);
  }, [opening, selected, onSelect]);

  /*
   * Whether an act the user started is still going on.
   *
   * The confirmation counts, and that is the part worth writing down. A
   * discard is two mutations with a question between them: the plan lands, the
   * dialog opens, and until it is answered nothing is pending — so a `busy`
   * built only out of `isPending` reads false in the middle of the one
   * operation on this screen that destroys work. Nothing is drawn differently
   * for it, because the dialog is modal and the panel behind it is inert
   * either way; what changes is that the lists can tell "the press is over"
   * from "the press is being asked about", which is what ChangeList needs to
   * know before it decides the keyboard is owed anywhere.
   *
   * `pendingDeletion` is the other confirmation on this screen and it is not
   * counted, because it is not started from a row: the button that opens it is
   * in the pane on the right, so no press in either list is waiting on it.
   */
  const busy =
    operations.stage.isPending ||
    operations.unstage.isPending ||
    operations.discard.isPending ||
    operations.discardPlan.isPending ||
    operations.save.isPending ||
    operations.resolve.isPending ||
    pendingDiscard !== undefined;

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
      {
        // Said, not drawn. Staging is a per-file action performed many times a
        // minute and a notification for each would be a card in front of the
        // work every time the work succeeds — while a reader who cannot see
        // the rows move from one list to the other is told nothing at all.
        onSuccess: () => toast.announce(describeMove(operation, paths, lines)),
        onError: (error) => report(operation, error),
      },
    );
  }

  /**
   * `git add` on an unmerged path, which is the same mutation as a stage and
   * not the same act.
   *
   * What the command does there is collapse the three index stages into one,
   * and that is what ends the conflict — the reason the button beside these
   * rows says "Mark resolved" and deliberately does not say "Stage". Routed
   * through its own handler so that the two sentences nobody sees on screen
   * agree with the one they do: a reader who presses "Mark resolved" hears it
   * confirmed in the word they pressed, and a failure names that word too,
   * rather than answering with the one the label was chosen to avoid.
   */
  function markResolved(paths: string[]) {
    operations.stage.mutate(
      { paths },
      {
        onSuccess: () => toast.announce(`Marked ${describePaths(paths)} resolved`),
        onError: (error) =>
          report(paths.length === 1 ? 'mark that resolved' : 'mark those resolved', error),
      },
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
   * Takes one side of a conflict for whole files, through git.
   *
   * Two commands and not one — `git checkout --ours`, then `git add` — and the
   * daemon runs both: the checkout writes the file and only the add collapses
   * the index stages that make git call it unmerged. It is also the daemon
   * that decides which of the two commands a path actually needs, because the
   * side somebody keeps may be the side that has no file at all.
   */
  function keepWholeFile(paths: string[], side: ConflictSide, onSettled: () => void) {
    const kept = `Kept ${side} for ${describePaths(paths)}`;

    operations.resolve.mutate(
      { paths, side },
      {
        // Announced with a card rather than politely, and it is the one thing
        // this screen does that nothing can undo alongside a discard: the
        // other side's version of the file is gone from the work tree, and
        // every comparable operation in the application — reset, revert,
        // stash, undo, checkout — says so when it succeeds.
        onSuccess: () => toast.push({ tone: 'success', title: kept }),
        // "keep ours", not "take the ours version": the enum is git's own word
        // and it is already on screen as the buttons Ours and Theirs, but "the
        // ours version" is not English, and a failure heading is the one
        // sentence on this screen that has to read like the rest of them.
        onError: (error) => report(side === 'ours' ? 'keep ours' : 'keep theirs', error),
        // Handed back to whoever asked, so the confirmation that put the
        // question closes on git's answer rather than on the press. Settled
        // and not succeeded: a box still on screen over a refusal is a box
        // with no way out of it. The discard dialog beside it is held open
        // the same way.
        onSettled,
      },
    );
  }

  /**
   * The other resolution, which is not keeping a side at all.
   *
   * Both branches deleted the file, so there is no version to check out and
   * the button on the pane says "Record the deletion". Routed through resolve
   * like the rest — the daemon decides that a side with no content is a
   * `git rm` — but named after the button that was pressed: a failure reading
   * "Could not keep ours" would name a side the pane had just finished saying
   * does not exist.
   *
   * A side is still sent, because the route takes one and this is the same
   * mutation. Which one is arbitrary and the daemon never uses it here: with
   * neither side holding a version, `HasSide` is false for both and the
   * command is the same `git rm` either way.
   */
  function recordDeletion(path: string, onSettled: () => void) {
    const recorded = `Recorded the deletion of ${path}`;

    operations.resolve.mutate(
      { paths: [path], side: 'ours' },
      {
        onSuccess: () => toast.push({ tone: 'success', title: recorded }),
        onError: (error) => report('record the deletion', error),
        onSettled,
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
          {showFilter && (
            <div className="shrink-0 border-b border-line px-3 py-2">
              {/* Not a Field: that one draws a label above itself and stands
                  36px tall, and this is a strip over a list in a 384px panel.
                  The name is on the control instead, where a screen reader
                  reads it and the placeholder repeats it for everyone else. */}
              <input
                type="text"
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                aria-label="Filter files by path"
                placeholder="Filter files…"
                className={cx(
                  'h-7 w-full min-w-0 rounded-sm border border-line-strong bg-sunken px-2',
                  'font-mono text-xs text-ink placeholder:text-ink-subtle',
                  'transition-colors transition-instant outline-none',
                  'focus-visible:focus-ring hover:border-ink-subtle',
                )}
              />
            </div>
          )}

          {/* A floor under the list. The commit box below is a constant 245px
              at every window height and the list used to take the whole
              squeeze — 499px of rows at 900, 59 at 460, none at all at 400.
              Below the floor it is the commit box that scrolls, which is why
              its wrapper has an overflow of its own: a box allowed to shrink
              without one puts its Commit button outside the panel, and a
              button that cannot be reached is worse than a short list. */}
          <div className="min-h-32 flex-1 overflow-auto">
            {status.files.length === 0 ? (
              <EmptyState
                title="Nothing to commit"
                description="The work tree matches the last commit. Edit a file and it will appear here."
              />
            ) : matches === 0 ? (
              <EmptyState
                title="No file matches"
                description={`Nothing in the work tree has "${filter.trim()}" in its path.`}
                action={
                  <Button variant="secondary" size="sm" onClick={() => setFilter('')}>
                    Clear the filter
                  </Button>
                }
              />
            ) : (
              <>
                {/* First, because it is what stops everything else. */}
                <ChangeList
                  title="Conflicted"
                  row="unstaged"
                  files={listed(conflicted)}
                  selected={selected}
                  onSelect={onSelect}
                  onMove={markResolved}
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
                  files={listed(staged)}
                  selected={selected}
                  onSelect={onSelect}
                  onMove={(paths) => move('unstage', paths)}
                  moveLabel="Unstage"
                  busy={busy}
                />
                {/* "Unstaged", the word the panel's own title counts in.
                    "Changed" had no matching count, "2 unstaged" had no
                    matching heading, and the same files were called four
                    things within a few hundred pixels — including a file git
                    has never seen, filed under a heading asserting it
                    changed. */}
                <ChangeList
                  title="Unstaged"
                  row="unstaged"
                  files={listed(changed)}
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

          <div className="min-h-0 shrink overflow-auto">
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
                      toast.push({
                        tone: 'success',
                        title: amend ? 'Commit amended' : 'Committed',
                      });
                    },
                  },
                );
              }}
            />
          </div>
        </div>
      </Panel>

      {/* The panel is titled "Diff", not "parser.go": a panel title is set in
          capitals and a path is case-sensitive. The path is inside the view,
          in the case the filesystem actually uses. */}
      <Panel
        className="min-w-0 flex-1"
        title={paneTitle(showing)}
        // Not for a binary file, and the condition is the diff's own answer
        // rather than a guess from the path: git decides what is binary, and
        // it says so in the diff this pane has already read. The control used
        // to offer an Edit pane that could only ever end in the daemon's 409 —
        // a request spent to be told, in an error, what the question was.
        // FileEditor's "Not a text file" stays as the backstop for the race
        // where a file becomes binary after its diff was read.
        actions={
          editable && !editingConflict && diff.data?.binary !== true ? (
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
            onRecord={() => setPendingDeletion(current.file)}
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
            onKeepWholeFile={(path, sideKept, onSettled) => {
              keepWholeFile([path], sideKept, onSettled);
            }}
            // The editor's only use of this is its own "Mark resolved", drawn
            // beside the conflict markers and nowhere else, so it is the same
            // press as the one on the row.
            onStage={(path) => markResolved([path])}
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
          operations.discard.mutate(
            {
              paths: request.files.map((file) => file.path),
              ...(request.lines === undefined ? {} : { lines: request.lines }),
            },
            {
              // It happened, and nothing on screen would otherwise say so: the
              // rows leave the list, which is also what an ordinary refetch
              // looks like, and this is the operation the code beside it calls
              // the one nothing can undo.
              onSuccess: () => toast.push({ tone: 'success', title: describeDiscarded(request) }),
              onError: (error) => report('discard those changes', error),
              // Closed when git has answered, not when the button was pressed.
              // The dialog was handed `busy` and then unmounted itself a tick
              // before the flag could become true, so the spinner it draws and
              // the Cancel it refuses were unreachable by construction — and a
              // discard across a large work tree read as a click that did
              // nothing until the status came back. RemoteActions holds its
              // force-push dialog open the same way.
              onSettled: () => setPendingDiscard(undefined),
            },
          );
        }}
      />

      {/* The editor's own question, put over the pane that has no editor.
          Both buttons run the same `git rm`, and the rule this product keeps
          is that a command which removes something shows itself first — a
          confirmation on one of two routes into one command is an exception,
          not a rule. Rendered only while it is being asked, because the file
          it is about is the one on screen at the time.

          `resolve.isPending` is precise here rather than the panel-wide
          `busy`: the editor is not mounted over this pane, so the only resolve
          that can be in flight is the one this box just sent. */}
      {pendingDeletion !== undefined && (
        <KeepWholeFileDialog
          side="ours"
          file={pendingDeletion}
          busy={operations.resolve.isPending}
          onCancel={() => setPendingDeletion(undefined)}
          onConfirm={() => {
            recordDeletion(pendingDeletion.path, () => setPendingDeletion(undefined));
          }}
        />
      )}
    </div>
  );
}

/**
 * The row this view opens on when nothing has been chosen.
 *
 * Conflicted first, for the reason that list is drawn first: it is what stops
 * everything else. Unstaged next, because that is where the work being done
 * is, and the staged list only when it is the whole of what differs.
 */
export function firstToShow(
  conflicted: FileStatus[],
  changed: FileStatus[],
  staged: FileStatus[],
): Selection | undefined {
  const unstagedFirst = conflicted[0] ?? changed[0];
  if (unstagedFirst !== undefined) {
    return { path: unstagedFirst.path, row: 'unstaged' };
  }
  const stagedFirst = staged[0];
  return stagedFirst === undefined ? undefined : { path: stagedFirst.path, row: 'staged' };
}

/** One path, or how many there were. A toast has room for one file name. */
function describePaths(paths: string[]): string {
  return paths.length === 1 ? (paths[0] ?? 'the file') : pluralize(paths.length, 'file');
}

/**
 * What a stage or an unstage just did, for the reader who cannot see it.
 *
 * Past tense and no more: this is said into a live region while the pointer is
 * still on the button, and a sentence is a sentence to sit through every time
 * a file moves.
 */
export function describeMove(
  operation: 'stage' | 'unstage',
  paths: string[],
  lines?: LineSelection,
): string {
  const verb = operation === 'stage' ? 'Staged' : 'Unstaged';
  if (lines !== undefined) {
    return `${verb} ${pluralize(lines.indices.length, 'line')} of ${describePaths(paths)}`;
  }
  return `${verb} ${describePaths(paths)}`;
}

/**
 * What a discard destroyed, in the toast that confirms it.
 *
 * The untracked case is a different loss and says so, exactly as the dialog
 * that asked did: nothing was discarded from that file, the file is gone.
 */
export function describeDiscarded(request: DiscardRequest): string {
  const [only] = request.files;

  if (request.lines !== undefined) {
    return `Discarded ${pluralize(request.lines.indices.length, 'line')} of ${only?.path ?? 'the file'}`;
  }
  if (request.files.length === 1 && only !== undefined) {
    return only.kind === 'untracked'
      ? `Deleted ${only.path}`
      : `Discarded the changes to ${only.path}`;
  }
  return `Discarded ${pluralize(request.files.length, 'file')}`;
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
 *
 * The button asks before it runs it. It used to be the one route into that
 * command with nothing in front of it — the same `git rm`, reached from the
 * editor's buttons, has shown itself since those learned to ask — and a
 * command that is quoted on one screen and silent on the next teaches the
 * reader that the quoting means nothing.
 */
function MissingConflictFile({
  file,
  busy,
  onRecord,
}: {
  file: FileStatus;
  busy: boolean;
  onRecord: () => void;
}) {
  return (
    <EmptyState
      title="Nothing on disk to edit"
      description={`git calls ${file.path} "${file.conflict ?? 'unmerged'}", so there is no version of it in the work tree to open. Recording the deletion is what ends the conflict.`}
      // No ellipsis, though the button now opens a question: the menus spell
      // a dialog with one and the buttons do not — the row's Discard opens a
      // confirmation and says "Discard" — and one button spelled a third way
      // would be a rule invented for a single control.
      action={
        <Button variant="secondary" size="sm" disabled={busy} onClick={onRecord}>
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

  return <EmptyState title="Could not read the diff" detail={detail} />;
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
