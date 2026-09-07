import type { FileDiff } from '../api/types';

/**
 * What a diff of a Git LFS pointer actually says.
 *
 * A pointer is three short lines — a version URL, an OID and a size — so a
 * diff of one is perfectly readable and perfectly useless: it reports that an
 * object identifier changed where somebody expected to see their picture
 * change. The lines are still worth showing, because staging one is what
 * records the file, but they need a sentence above them saying what they
 * stand for.
 *
 * Kept out of the pane that draws it so the sentence can be asserted without a
 * browser. Which sides are pointers is the daemon's reading — see
 * internal/git/lfs.go.
 */

export interface PointerNote {
  /** What happened, in one clause: the heading of the note. */
  title: string;
  /** The size the reader came for, and what to do about the content. */
  detail: string;
}

/**
 * The note for a diff, or nothing where neither side is a pointer.
 *
 * Four cases, and they are genuinely different things:
 *
 *   Both sides pointers — the ordinary one. A file stored in LFS was
 *   replaced, and the sizes are the only part of it a person can read.
 *
 *   New side only — and which of two things that is depends on whether the
 *   file is new. A file the diff ADDS has no old side to have been anything,
 *   so there is nothing it moved out of; a file that was already tracked has
 *   its content on the left and a pointer on the right, which reads as the
 *   whole file being deleted unless it is named.
 *
 *   Old side only — the mirror of it. A file the diff REMOVES is gone, and
 *   the pointer is what it had; one that stays is coming back out of LFS,
 *   which reads as a huge file being added.
 *
 * The added and removed flags are read rather than inferred, and that is the
 * whole of why the two pairs are separated: "the content on the left is the
 * file itself" is a sentence about a left-hand side, and a file being added
 * has none.
 */
export function pointerNote(diff: FileDiff): PointerNote | undefined {
  const before = diff.lfs?.old;
  const after = diff.lfs?.new;

  if (before !== undefined && after !== undefined) {
    return {
      title: 'Stored with Git LFS',
      detail:
        before.oid === after.oid
          ? `The pointer is unchanged — ${describeSize(after.size)}. What differs here is not the file's content.`
          : `${describeSize(before.size)} → ${describeSize(after.size)}. The lines below are the pointer, not the file.`,
    };
  }

  if (after !== undefined) {
    return diff.added
      ? {
          title: 'Added, stored with Git LFS',
          detail: `The file is new and kept outside the repository, ${describeSize(after.size)}. The lines below are the pointer committed in its place, not its content.`,
        }
      : {
          title: 'Moved into Git LFS',
          detail: `The content on the left is the file itself, ${describeSize(after.size)}; the right is the pointer that now stands for it. Nothing was deleted.`,
        };
  }

  if (before !== undefined) {
    return diff.removed
      ? {
          title: 'Deleted, was stored with Git LFS',
          detail: `The lines on the left are the pointer that stood for ${describeSize(before.size)}. The file is gone from this commit; the LFS object it named is not removed by deleting it.`,
        }
      : {
          title: 'Taken out of Git LFS',
          detail: `The left is the pointer that stood for ${describeSize(before.size)}; the right is the file itself. Nothing was added.`,
        };
  }

  return undefined;
}

/**
 * A byte count in the unit a person would say it in.
 *
 * Powers of 1024 with the SI-style names git itself prints — `git count-objects
 * -H` says "1.20 MiB" — because the number beside them has to agree with what
 * git says about the same file.
 *
 * A size of zero is a real answer: an empty file tracked by LFS has a pointer
 * like any other.
 */
export function describeSize(bytes: number): string {
  const units = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB'];
  let size = bytes;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  // Whole bytes stay whole: "1.00 bytes" is a number nobody writes.
  return unit === 0 ? `${size} ${units[0]}` : `${size.toFixed(2)} ${units[unit]}`;
}
