import { useEffect, useRef, useState, type KeyboardEvent } from 'react';

import type { FileStatus } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { describeKind, FileStatusMark } from '../components/FileStatusMark';
import { cx } from '../lib/cx';
import { pluralize } from '../lib/format';
import { REVEALED_ON_ATTENTION } from '../lib/reveal';

/**
 * The files that differ, in two lists.
 *
 * Two lists and not one with checkboxes, because a file can be in both at
 * once: `git add`, then edit again, and the same path has something staged and
 * something not. A single row with a tick would have to choose which half of
 * that to show, and whichever it chose would be wrong half the time.
 *
 * The headings are yagit's own two words for that split. `git status` prints
 * "Changes to be committed", "Changes not staged for commit" and "Untracked
 * files" — sentences rather than headings, and a third list for files git has
 * never seen. Those files are staged by the same `git add` as every other row,
 * so they are listed with the rest and the mark at the start of the row says
 * which kind of change each one is.
 */

/** Which list a row belongs to. It decides the diff side and the actions. */
export type ChangeRow = 'staged' | 'unstaged';

export interface Selection {
  path: string;
  row: ChangeRow;
}

/**
 * A discard, coloured rather than filled — and coloured differently in the two
 * places it appears.
 *
 * `variant="danger"` is a solid red button. This one sits at the head of the
 * unstaged list and again on every row of it, so the filled variant would
 * paint a screen of red buttons that are never the action anybody came for.
 * The label carries the colour instead, which is what Menu already does for
 * its destructive items, and the tinted ground waits for the hover — the point
 * at which the pointer has committed to this button rather than the one beside
 * it.
 *
 * The row's copy gives up the resting colour, and the reason is measured
 * rather than aesthetic: a row's ground is `--color-selected` while it is the
 * chosen one, and `--color-danger` over that is 4.27:1 in the light theme and
 * 4.36:1 in the dark — under the 4.5:1 the project gates on, for 12px text.
 * Over the header's `--color-surface` the same ink is 5.83:1 and 6.06:1, and
 * over `--color-danger-soft` on hover it is 5.08:1 and 5.22:1, so both of
 * those keep it.
 */
const DISCARD_ON_SURFACE = 'text-danger hover:bg-danger-soft hover:text-danger';
const DISCARD_ON_A_ROW = 'hover:bg-danger-soft hover:text-danger';

interface ChangeListProps {
  title: string;
  row: ChangeRow;
  files: FileStatus[];
  selected: Selection | undefined;
  onSelect: (selection: Selection) => void;

  /** The action that moves a whole file the other way. */
  onMove: (paths: string[]) => void;
  moveLabel: string;

  /** Absent on the staged list: unstaging is how a staged change is undone. */
  onDiscard?: (files: FileStatus[]) => void;

  busy: boolean;
}

export function ChangeList({
  title,
  row,
  files,
  selected,
  onSelect,
  onMove,
  moveLabel,
  onDiscard,
  busy,
}: ChangeListProps) {
  /*
   * Which row the list hands the keyboard, and why the whole list costs three
   * tab stops rather than three per file.
   *
   * Five hundred changed files is an ordinary morning after a formatter run,
   * and every row was three tab stops of its own — the row, its Stage and its
   * Discard — so reaching the commit box from the top of the list cost a press
   * per button per file. Tabs.tsx argues exactly this case for repository tabs
   * and answers it the same way: one row is in the page's tab order and the
   * arrows move between the rest.
   *
   * Its two buttons come with it, and that is the half a roving tabindex is
   * usually written without. Taking only the row out of the tab order leaves
   * the Stage and the Discard of all five hundred in it — a thousand stops
   * instead of fifteen hundred, which is not the fix it looks like. So Tab
   * walks the row the arrows are standing on, its actions included, and then
   * leaves the list; the actions of every other row are reached by arrowing to
   * that row first.
   *
   * Held as a path rather than an index: the list is rebuilt from a status
   * that arrives every two seconds, and an index would outlive the row it was
   * taken from, quietly meaning whichever file moved up into it.
   */
  const [focusedPath, setFocusedPath] = useState<string>();
  const rowButtons = useRef(new Map<string, HTMLButtonElement>());

  /**
   * The row whose own button was pressed, and where it stood when it was.
   *
   * Kept because that row is about to stop existing: staging a file moves it
   * to the other list, discarding one removes it altogether, and the button
   * that was pressed goes with it. The index is taken at the press, since by
   * the time the row is gone there is no row left to ask where it was.
   *
   * A ref and not state: nothing renders it, and what tells the effect below
   * that the row has gone is `files` changing rather than this. Held as state
   * it would be a second render per press and a setState inside an effect,
   * which is the shape React's own lint rule refuses.
   */
  const acted = useRef<{ path: string; at: number }>(undefined);

  /*
   * Where the keyboard goes when the button holding it disappears.
   *
   * Two things eject focus from this list on every press and neither is
   * visible: `busy` disables every row button while the mutation is in flight,
   * and a browser blurs an element it disables — then the row itself leaves
   * for the other list. Focus lands on <body>, so the next Tab starts at the
   * top of the document, and somebody staging a run of files with the keyboard
   * has to travel back into the list for each one.
   *
   * The row that took its place, rather than the file that left. Following the
   * file into the staged list was the other candidate and it is worse for the
   * thing people actually do here: staging several files in a row. Standing
   * still is what a list does when a row is deleted from it, and it is what
   * makes the next Enter mean the next file.
   *
   * Only from <body>, though. A press whose focus survived — the header's
   * "Stage all", a pointer user who has moved on to the commit box — is left
   * exactly where it is; a list that grabbed the keyboard back a second after
   * the click would be the trap every autofocus falls into.
   *
   * And only for as long as the press is still going on, which is the other
   * half and the one with no symptom until much later. A press git refused —
   * an index.lock a terminal is holding, a discard the user cancelled — ends
   * with the row exactly where it was, so the check below finds it listed and
   * leaves. The press stayed recorded for the rest of the session, and the
   * next time that path left the list for any reason at all, minutes later
   * and by somebody else's hand, this list took the keyboard for it. `busy`
   * is what says the press is over: ChangesView counts a confirmation still
   * on screen as part of the act, so a discard is one press from the button
   * to git's answer rather than two with a gap in the middle.
   */
  useEffect(() => {
    const pressed = acted.current;
    if (pressed === undefined) {
      return;
    }
    // Still listed: the mutation has not landed, or it did not move this row.
    if (files.some((file) => file.path === pressed.path)) {
      if (!busy) {
        acted.current = undefined;
      }
      return;
    }
    acted.current = undefined;

    const active = document.activeElement;
    if (active !== null && active !== document.body) {
      return;
    }
    const heir = files[Math.min(pressed.at, files.length - 1)];
    if (heir === undefined) {
      return;
    }
    setFocusedPath(heir.path);
    rowButtons.current.get(heir.path)?.focus();
  }, [files, busy]);

  if (files.length === 0) {
    return null;
  }

  // The chosen row when it is in this list, so tabbing back in lands where the
  // user was; otherwise the top of the list.
  const roving =
    files.find((file) => file.path === focusedPath) ??
    (selected !== undefined && selected.row === row
      ? files.find((file) => file.path === selected.path)
      : undefined) ??
    files[0];

  /*
   * Focus moves; the selection does not.
   *
   * Selecting on every arrow press would fetch a diff per keystroke, and the
   * diff of a lockfile is the one request this screen cannot afford to make
   * casually — the daemon's cap is ten megabytes and the parse is on the main
   * thread. Enter and Space select, which is what the row button always did.
   *
   * The ends are clamped rather than wrapped, and rather than left to the
   * browser: the native answer to ArrowDown is to scroll the rows out from
   * under the focused one, which desynchronises where the user is from what
   * they can see — worse than the key doing nothing.
   */
  function moveFocus(event: KeyboardEvent<HTMLUListElement>) {
    // Only from a row. The Stage and Discard buttons live inside this list
    // too, and an arrow press on one of them means whatever the browser says
    // it means.
    if (!(event.target instanceof HTMLElement) || !event.target.hasAttribute('data-file-row')) {
      return;
    }

    const at = files.findIndex((file) => file.path === roving?.path);
    let next: number;
    switch (event.key) {
      case 'ArrowDown':
        next = at + 1;
        break;
      case 'ArrowUp':
        next = at - 1;
        break;
      case 'Home':
        next = 0;
        break;
      case 'End':
        next = files.length - 1;
        break;
      default:
        return;
    }

    const target = files[Math.min(Math.max(next, 0), files.length - 1)];
    if (target === undefined) {
      return;
    }
    event.preventDefault();
    setFocusedPath(target.path);
    rowButtons.current.get(target.path)?.focus();
  }

  return (
    <section className="flex min-h-0 flex-col">
      {/* Stuck to the top of the scroller, because which side of the index a
          file is on is the most important fact on this screen and one flick of
          the wheel used to take it away — the two row types are otherwise near
          identical, down to the mark letter for a file staged and then edited
          again. The ground is the panel's own, so the rows pass underneath
          rather than through. HunkView does the same over `bg-sunken`. */}
      <header className="sticky top-0 z-10 flex shrink-0 items-center gap-2 border-b border-line bg-surface px-3 py-1.5">
        <h3 className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">{title}</h3>
        <Badge>{files.length}</Badge>

        {/* The action the user came for first, and the irreversible one after
            it: a pointer travelling right to Stage no longer crosses Discard
            on the way. The gap is two steps rather than one for the same
            reason — these were 4px apart in identical chrome. */}
        <div className="ml-auto flex items-center gap-2">
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() => onMove(files.map((file) => file.path))}
            aria-label={`${moveLabel} all ${pluralize(files.length, 'file')}`}
          >
            {moveLabel} all
          </Button>
          {onDiscard !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              className={DISCARD_ON_SURFACE}
              disabled={busy}
              onClick={() => onDiscard(files)}
              aria-label={`Discard all ${pluralize(files.length, 'unstaged change')}`}
            >
              Discard all
            </Button>
          )}
        </div>
      </header>

      {/* No overflow of its own: this list is a block inside the panel's one
          scroller, and the `overflow-auto` that used to be here never fired —
          it only suggested to the next reader that each section scrolls
          separately, which is what would break the sticky headers above. */}
      <ul onKeyDown={moveFocus}>
        {files.map((file, index) => (
          <li key={file.path}>
            <FileRow
              file={file}
              row={row}
              selected={selected?.path === file.path && selected.row === row}
              tabbable={file.path === roving?.path}
              register={(node) => {
                if (node === null) {
                  rowButtons.current.delete(file.path);
                } else {
                  rowButtons.current.set(file.path, node);
                }
              }}
              onFocus={() => setFocusedPath(file.path)}
              onSelect={onSelect}
              // Where the row stood when its own button was pressed. Both of
              // its actions end with the row gone and the keyboard nowhere;
              // see `acted` above.
              onAct={() => {
                acted.current = { path: file.path, at: index };
              }}
              onMove={onMove}
              moveLabel={moveLabel}
              onDiscard={onDiscard}
              busy={busy}
            />
          </li>
        ))}
      </ul>
    </section>
  );
}

interface FileRowProps {
  file: FileStatus;
  row: ChangeRow;
  selected: boolean;
  /**
   * This row, and its two buttons, are the list's place in the page's tab
   * order. Exactly one row per list carries it. See ChangeList.
   */
  tabbable: boolean;
  register: (node: HTMLButtonElement | null) => void;
  onFocus: () => void;
  onSelect: (selection: Selection) => void;
  /**
   * Called first by both of this row's actions, and by neither of the list's.
   *
   * The list needs to know which row was acted on before the row disappears,
   * and it is the row that knows the press happened. Selecting is not one of
   * these: it leaves the row exactly where it is.
   */
  onAct: () => void;
  onMove: (paths: string[]) => void;
  moveLabel: string;
  onDiscard?: (files: FileStatus[]) => void;
  busy: boolean;
}

function FileRow({
  file,
  row,
  selected,
  tabbable,
  register,
  onFocus,
  onSelect,
  onAct,
  onMove,
  moveLabel,
  onDiscard,
  busy,
}: FileRowProps) {
  const { directory, name } = splitPath(file.path);

  return (
    // A row, not a button: it holds buttons of its own, and a button inside a
    // button is invalid HTML that browsers repair by moving it out.
    <div
      className={cx(
        'group/row flex items-center gap-2 pr-1 pl-3',
        selected ? 'bg-selected' : 'hover:bg-hover',
      )}
    >
      {/* The label is spelled out because the row's contents are not a
          sentence: a coloured letter, a path split into two spans of different
          weights, and a badge. Read out in order they say "M src/ parser.go",
          which is not what anyone needs to hear. */}
      <button
        type="button"
        data-file-row=""
        ref={register}
        tabIndex={tabbable ? 0 : -1}
        onFocus={onFocus}
        onClick={() => onSelect({ path: file.path, row })}
        aria-current={selected}
        aria-label={describeRow(file, row)}
        className={cx(
          'flex min-w-0 flex-1 items-center gap-2 py-1.5 text-left outline-none',
          'focus-visible:focus-ring',
        )}
      >
        <FileStatusMark file={file} side={row === 'staged' ? 'index' : 'work_tree'} />

        {/* The name at full strength, the directory quiet behind it — which is
            what this said before it was true. Both spans shrank in proportion
            to their length, so the longer string kept more of the width and a
            path like internal/services/oidc/token_exchange_handler.go was
            drawn as the directory almost whole and the name cut in half: the
            identifying part of a row, cut to make room for the part that
            merely places it.

            shrink-0 alone is not the fix and was measured doing damage: the
            name then overflows its button by the length of the basename and
            runs across the diff pane. `max-w-full` is what bounds it, so the
            name takes what it needs up to the whole path column and the
            directory yields the rest. The title on the wrapper stays as the
            recovery for whichever half is gone. */}
        <span className="flex min-w-0 items-baseline gap-1" title={file.path}>
          {directory !== '' && (
            <span className="min-w-0 truncate font-mono text-2xs text-ink-subtle">{directory}</span>
          )}
          <span className="max-w-full shrink-0 truncate font-mono text-xs text-ink">{name}</span>
        </span>

        {file.old_path !== undefined && file.old_path !== '' && (
          <span className="shrink-0 font-mono text-2xs text-ink-subtle" title={file.old_path}>
            ← {renamedFrom(file.old_path, file.path)}
          </span>
        )}

        {file.conflict !== undefined && file.conflict !== '' && (
          <Badge tone="danger" className="shrink-0">
            {file.conflict}
          </Badge>
        )}
      </button>

      {/* Staging one file is the reason to be in a graphical client rather
          than in `git add -A`, and it used to be invisible until the pointer
          arrived: at rest the only staging on the screen was all-or-nothing.
          So the constructive action is drawn on every row, and only the
          destructive one waits to be asked for.

          Each is labelled with the file it acts on. The visible word is
          "Discard" on every row, which is right on screen — the row says which
          file — and useless to anyone reading the buttons on their own, where
          twenty identical "Discard" buttons name nothing. */}
      <div className="flex shrink-0 items-center gap-2">
        <Button
          size="sm"
          variant="ghost"
          tabIndex={tabbable ? 0 : -1}
          disabled={busy}
          onClick={() => {
            onAct();
            onMove([file.path]);
          }}
          aria-label={`${moveLabel} ${file.path}`}
        >
          {moveLabel}
        </Button>
        {onDiscard !== undefined && (
          <Button
            size="sm"
            variant="ghost"
            // Out of the tab order with its row, not out of the page: a
            // pointer still finds it, arrows still reach the row that owns it,
            // and it keeps its accessible name wherever it stands.
            tabIndex={tabbable ? 0 : -1}
            className={cx(REVEALED_ON_ATTENTION, DISCARD_ON_A_ROW)}
            disabled={busy}
            onClick={() => {
              onAct();
              onDiscard([file]);
            }}
            aria-label={describeDiscard(file)}
          >
            Discard
          </Button>
        )}
      </div>
    </div>
  );
}

/**
 * A row, in a sentence: the path, what happened to it, and which side it is
 * on.
 *
 * The path comes first because that is what someone is looking for, and
 * because a list read aloud is only navigable when every entry starts with the
 * thing that tells them apart.
 */
export function describeRow(file: FileStatus, row: ChangeRow): string {
  const state = row === 'staged' ? 'staged' : 'not staged';
  if (file.conflict !== undefined && file.conflict !== '') {
    return `${file.path}, ${file.conflict}`;
  }
  // Where it came from, when git says it came from somewhere. The arrow on
  // the row carries that for a reader who can see it; "renamed" on its own
  // tells the one who cannot that something moved and never says what.
  const kind =
    file.old_path === undefined || file.old_path === ''
      ? describeKind(file.kind)
      : `${describeKind(file.kind)} from ${file.old_path}`;
  return `${file.path}, ${kind}, ${state}`;
}

/**
 * What follows the arrow on a row git reports as a rename or a copy.
 *
 * The basename alone was what this drew, and for a file that MOVED it drew
 * nothing: `src/gen2.txt` becoming `pkg/gen2.txt` read "R pkg/ gen2.txt ←
 * gen2.txt", where the arrow points back at the word it started from and the
 * one fact the row exists to carry — it moved, and from where — is the one it
 * dropped.
 *
 * So the whole old path whenever the directory has changed, which is what the
 * header over the patch shows for the same file; and the name alone when the
 * file stayed where it was, because the directory is then already drawn a few
 * pixels to the left and repeating it would spend the row's width saying
 * something the reader can see.
 */
export function renamedFrom(oldPath: string, newPath: string): string {
  const from = splitPath(oldPath);
  return from.directory === splitPath(newPath).directory ? from.name : oldPath;
}

/**
 * What the row's Discard button does to this particular file.
 *
 * "Discard the changes to src/b.ts" is a promise about changes, and for a file
 * git has never seen there are none: what runs is `git clean`, and what
 * disappears is the file. The confirmation has always said so — "the file
 * itself, which git has never seen" — and the button that opens it said
 * something else, which is the wrong way round for the two of them.
 */
export function describeDiscard(file: FileStatus): string {
  if (file.kind === 'untracked') {
    return `Delete ${file.path}, which git has never seen`;
  }
  return `Discard the changes to ${file.path}`;
}

/**
 * Splits a path into the part that identifies the file and the part that
 * places it.
 *
 * Forward slashes only: that is what git speaks, on every platform.
 */
export function splitPath(path: string): { directory: string; name: string } {
  const cut = path.lastIndexOf('/');
  if (cut < 0) {
    return { directory: '', name: path };
  }
  return { directory: path.slice(0, cut + 1), name: path.slice(cut + 1) };
}
