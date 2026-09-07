import type { FileStatus } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { describeKind, FileStatusMark } from '../components/FileStatusMark';
import { cx } from '../lib/cx';

/**
 * The files that differ, in two lists.
 *
 * Two lists and not one with checkboxes, because a file can be in both at
 * once: `git add`, then edit again, and the same path has something staged and
 * something not. A single row with a tick would have to choose which half of
 * that to show, and whichever it chose would be wrong half the time.
 *
 * The two lists are what `git status` itself prints, under the same names.
 */

/** Which list a row belongs to. It decides the diff side and the actions. */
export type ChangeRow = 'staged' | 'unstaged';

export interface Selection {
  path: string;
  row: ChangeRow;
}

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
  if (files.length === 0) {
    return null;
  }

  return (
    <section className="flex min-h-0 flex-col">
      <header className="flex shrink-0 items-center gap-2 border-b border-line px-3 py-1.5">
        <h3 className="text-2xs font-medium tracking-wide text-ink-subtle uppercase">{title}</h3>
        <Badge>{files.length}</Badge>

        <div className="ml-auto flex items-center gap-1">
          {onDiscard !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => onDiscard(files)}
              aria-label={`Discard all ${files.length} unstaged changes`}
            >
              Discard all
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() => onMove(files.map((file) => file.path))}
            aria-label={`${moveLabel} all ${files.length} files`}
          >
            {moveLabel} all
          </Button>
        </div>
      </header>

      <ul className="min-h-0 overflow-auto">
        {files.map((file) => (
          <li key={file.path}>
            <FileRow
              file={file}
              row={row}
              selected={selected?.path === file.path && selected.row === row}
              onSelect={onSelect}
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
  onSelect: (selection: Selection) => void;
  onMove: (paths: string[]) => void;
  moveLabel: string;
  onDiscard?: (files: FileStatus[]) => void;
  busy: boolean;
}

function FileRow({
  file,
  row,
  selected,
  onSelect,
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
        onClick={() => onSelect({ path: file.path, row })}
        aria-current={selected}
        aria-label={describeRow(file, row)}
        className={cx(
          'flex min-w-0 flex-1 items-center gap-2 py-1.5 text-left outline-none',
          'focus-visible:focus-ring',
        )}
      >
        <FileStatusMark file={file} side={row === 'staged' ? 'index' : 'work_tree'} />

        {/* The name at full strength, the directory quiet behind it. A list of
            twenty paths under one deep directory is unreadable when every row
            leads with the same forty characters. */}
        <span className="flex min-w-0 items-baseline gap-1" title={file.path}>
          {directory !== '' && (
            <span className="truncate font-mono text-2xs text-ink-subtle">{directory}</span>
          )}
          <span className="truncate font-mono text-xs text-ink">{name}</span>
        </span>

        {file.old_path !== undefined && file.old_path !== '' && (
          <span className="shrink-0 font-mono text-2xs text-ink-subtle" title={file.old_path}>
            ← {splitPath(file.old_path).name}
          </span>
        )}

        {file.conflict !== undefined && file.conflict !== '' && (
          <Badge tone="danger" className="shrink-0">
            {file.conflict}
          </Badge>
        )}
      </button>

      {/* Shown on hover and on keyboard focus. focus-within is what keeps the
          actions reachable without a mouse: a Tab into them would otherwise
          land on a button nobody can see.

          Each one is labelled with the file it acts on. The visible word is
          "Discard" on every row, which is right on screen — the row says which
          file — and useless to anyone reading the buttons on their own, where
          twenty identical "Discard" buttons name nothing. */}
      <div
        className={cx(
          'flex shrink-0 items-center gap-0.5 opacity-0',
          'group-hover/row:opacity-100 group-focus-within/row:opacity-100',
        )}
      >
        {onDiscard !== undefined && (
          <Button
            size="sm"
            variant="ghost"
            disabled={busy}
            onClick={() => onDiscard([file])}
            aria-label={`Discard the changes to ${file.path}`}
          >
            Discard
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          disabled={busy}
          onClick={() => onMove([file.path])}
          aria-label={`${moveLabel} ${file.path}`}
        >
          {moveLabel}
        </Button>
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
  return `${file.path}, ${describeKind(file.kind)}, ${state}`;
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
