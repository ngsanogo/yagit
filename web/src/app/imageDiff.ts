import type { DiffSide, FileDiff } from '../api/types';

/**
 * Image previews for a binary FileDiff.
 *
 * git answers a PNG with "Binary files differ" and nothing else. The path's
 * extension is what says the bytes are worth drawing; the blob route is what
 * hands them over. Staging and discarding stay whole-file — a patch cannot
 * carry pixels.
 */

const IMAGE_EXTENSION = /\.(png|jpe?g|gif|webp|svg)$/i;

/** Whether this path is an image the blob route will serve. */
export function isImagePath(path: string): boolean {
  return IMAGE_EXTENSION.test(path);
}

/**
 * Whether a binary FileDiff should draw a side-by-side preview instead of the
 * "Binary file" notice.
 *
 * Both names matter on a rename: either side can be the image the other was
 * converted from or to.
 */
export function isImageDiff(diff: FileDiff): boolean {
  if (!diff.binary) {
    return false;
  }
  if (isImagePath(diff.path)) {
    return true;
  }
  return diff.old_path !== undefined && diff.old_path !== '' && isImagePath(diff.old_path);
}

/** One half of an image preview: which path to load from which place. */
export interface ImageSide {
  path: string;
  /** worktree | index | head | a revision the blob route accepts. */
  at: string;
  label: string;
}

/**
 * The two halves (or one) to draw for a working-directory diff.
 *
 * Mapped from the DiffSide the panel already chose: staged is HEAD→index,
 * unstaged is index→worktree, untracked is worktree alone. Added and removed
 * keep a single panel rather than an empty slot labeled "missing".
 */
export function imageSidesForWorkingDiff(diff: FileDiff, side: DiffSide): ImageSide[] {
  const current = diff.path;
  const previous = diff.old_path !== undefined && diff.old_path !== '' ? diff.old_path : diff.path;

  const sides: ImageSide[] = [];

  if (!diff.added && side !== 'untracked') {
    sides.push({
      path: previous,
      at: side === 'staged' ? 'head' : 'index',
      label: side === 'staged' ? 'HEAD' : 'index',
    });
  }

  if (!diff.removed) {
    const at = side === 'staged' ? 'index' : 'worktree';
    sides.push({
      path: current,
      at,
      label: at === 'index' ? 'staged' : 'worktree',
    });
  }

  return sides;
}

/**
 * The two halves (or one) to draw for a commit or stash patch.
 *
 * new is the revision itself; old is its first parent. A root commit and a
 * pure addition keep only the new side.
 */
export function imageSidesForRevision(diff: FileDiff, revision: string): ImageSide[] {
  const current = diff.path;
  const previous = diff.old_path !== undefined && diff.old_path !== '' ? diff.old_path : diff.path;

  const sides: ImageSide[] = [];
  if (!diff.added) {
    sides.push({
      path: previous,
      at: `${revision}^`,
      label: 'before',
    });
  }
  if (!diff.removed) {
    sides.push({
      path: current,
      at: revision,
      label: 'after',
    });
  }
  return sides;
}

/** Same-origin URL the session cookie already opens. */
export function blobURL(repositoryId: string, side: ImageSide): string {
  const query = new URLSearchParams({ path: side.path, at: side.at });
  return `/api/repos/${repositoryId}/blob?${query}`;
}
