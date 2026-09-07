import type { EntryKind, FileStatus, StatusCode } from '../api/types';
import { cx } from '../lib/cx';

/**
 * The one-letter mark git puts beside a path, in the colour its kind of change
 * owns.
 *
 * A letter rather than an icon, and git's own letter: M, A, D, R, C, T for a
 * type change, ? for a file git has never seen, ! for a conflict. Someone who
 * has read `git status` once already knows how to read this, and someone who
 * has not learns a vocabulary that works in their terminal too — which is the
 * same reason the log panel shows raw commands.
 */

/** Every colour comes from the file-status tokens, one per kind of change. */
const MARK_CLASSES: Record<string, string> = {
  M: 'text-modified border-modified/45 bg-modified/12',
  A: 'text-added border-added/45 bg-added/12',
  D: 'text-deleted border-deleted/45 bg-deleted/12',
  R: 'text-renamed border-renamed/45 bg-renamed/12',
  C: 'text-renamed border-renamed/45 bg-renamed/12',
  T: 'text-modified border-modified/45 bg-modified/12',
  '?': 'text-untracked border-untracked/45 bg-untracked/12',
  '!': 'text-conflicted border-conflicted/45 bg-conflicted/12',
};

const MARK_LABELS: Record<string, string> = {
  M: 'modified',
  A: 'added',
  D: 'deleted',
  R: 'renamed',
  C: 'copied',
  T: 'type changed',
  '?': 'untracked',
  '!': 'conflicted',
};

interface FileStatusMarkProps {
  file: FileStatus;
  /**
   * Which side's code to show. A file can be added to the index and deleted
   * from the work tree afterwards, and the two rows have to say different
   * things about it.
   */
  side: 'index' | 'work_tree';
}

export function FileStatusMark({ file, side }: FileStatusMarkProps) {
  const mark = markFor(file, side);

  return (
    <span
      // The colour alone would carry the meaning for everyone but the people
      // who cannot see it. The letter is the same information in a channel
      // that survives, and the title spells it out for a screen reader.
      title={MARK_LABELS[mark] ?? 'changed'}
      className={cx(
        'inline-flex size-4 shrink-0 items-center justify-center rounded-sm border',
        'font-mono text-2xs leading-none font-semibold',
        MARK_CLASSES[mark] ?? 'text-ink-subtle border-line bg-hover',
      )}
    >
      {mark}
    </span>
  );
}

/**
 * The letter for one side of a file.
 *
 * Untracked and unmerged are decided by the kind, not by the codes: git writes
 * `..` for an untracked path and a pair of stage letters for an unmerged one,
 * and neither means what an ordinary entry's codes mean.
 */
export function markFor(file: FileStatus, side: 'index' | 'work_tree'): string {
  if (file.kind === 'untracked') {
    return '?';
  }
  if (file.kind === 'unmerged') {
    return '!';
  }
  const code: StatusCode = side === 'index' ? file.index : file.work_tree;
  return code === '.' ? 'M' : code;
}

/** How a kind of entry reads in a sentence, for the rows that need one. */
export function describeKind(kind: EntryKind): string {
  switch (kind) {
    case 'untracked':
      return 'new file';
    case 'renamed':
      return 'renamed';
    case 'copied':
      return 'copied';
    case 'unmerged':
      return 'conflicted';
    case 'ordinary':
      return 'changed';
  }
}
