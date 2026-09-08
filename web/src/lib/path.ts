/**
 * The questions the interface asks of a path it was handed.
 *
 * Handed, never built: every path here came out of the daemon or out of a box
 * somebody typed, and nothing in this file joins, resolves or normalises one.
 * What a path means on disk is the daemon's business — it owns the root
 * boundary — and a browser guessing at it would be a second answer to keep in
 * step with the first.
 *
 * Both separators, always. The daemon may be running on Windows and the paths
 * it reports are that machine's, so a split on `/` alone answers with the
 * whole of `C:\src\repo` where a directory name was wanted. Written once here
 * because three screens ask, and three copies is three chances for one of them
 * to be the version that forgot.
 *
 * Whitespace is not touched, and that is a decision rather than an omission.
 * A trailing separator is punctuation — `/repos/build/` and `/repos/build` are
 * one directory — but a trailing SPACE is part of the name: `/repos/build ` is
 * a directory a person can make on every system yagit runs on, and a helper
 * that quietly trimmed it would put a name on screen that opens nothing.
 */

/** The last segment of a path — what a directory or a file is called. */
export function leafOf(path: string): string {
  const trimmed = path.replace(/[/\\]+$/, '');
  const cut = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  return cut < 0 ? trimmed : trimmed.slice(cut + 1);
}

/**
 * The directory a path sits in.
 *
 * A path with no separator, or one whose only separator is its first
 * character, answers with itself: there is no parent to name, and inventing
 * `/` or `.` would put a directory on screen that nobody asked about.
 */
export function parentDirectoryOf(path: string): string {
  const trimmed = path.replace(/[/\\]+$/, '');
  const cut = Math.max(trimmed.lastIndexOf('/'), trimmed.lastIndexOf('\\'));
  return cut <= 0 ? trimmed : trimmed.slice(0, cut);
}

/**
 * How much of a shortened path is kept.
 *
 * Two segments, because one is the leaf and the leaf is already drawn beside
 * the path — as a tab's label, as a row's name — so a one-segment answer
 * repeats what the reader has and says nothing new. The second segment is the
 * first that can tell two checkouts of the same project apart.
 */
const KEPT_SEGMENTS = 2;

/** Stands in for the head of a path that was cut away. */
const ELIDED = '…';

/**
 * A path cut down to the end that identifies it.
 *
 * Cut at the FRONT, and that is the whole reason this exists rather than a CSS
 * `truncate` on the element. Two checkouts of one project differ in their
 * parent directory and agree on everything after it, so cutting the tail takes
 * exactly the characters that tell them apart: `/home/me/work/service` and
 * `/home/me/spike/service` both draw as `/home/me/wor…`, and the interface has
 * then spent a line to say nothing.
 *
 * Sliced out of the original rather than split and re-joined, so the
 * separators come back as they went in: a Windows path stays a Windows path,
 * and one carrying both kinds is not quietly normalised into whichever this
 * file preferred.
 *
 * A path with nothing to cut is answered whole. The ellipsis is a promise that
 * something was removed, and one in front of a path that is already short is a
 * claim the reader cannot check.
 */
export function shortenPath(path: string): string {
  const trimmed = path.replace(/[/\\]+$/, '');
  let cut = trimmed.length;
  for (let kept = 0; kept < KEPT_SEGMENTS; kept += 1) {
    const separator = Math.max(
      trimmed.lastIndexOf('/', cut - 1),
      trimmed.lastIndexOf('\\', cut - 1),
    );
    // At the first character there is nothing left to elide: a leading
    // separator is the root, and `…/repos/yagit` for `/repos/yagit` would
    // claim a directory above it that the reader was never shown.
    if (separator <= 0) {
      return trimmed;
    }
    cut = separator;
  }
  // What goes has to be more than the root the path starts from. `C:\src\repo`
  // would otherwise draw as `…\src\repo`, and the drive letter is the one
  // character standing between that and `D:\src\repo` — the very confusion
  // this function is here to prevent, reintroduced by the fix for it.
  if (!/[/\\]/.test(trimmed.slice(0, cut))) {
    return trimmed;
  }
  return ELIDED + trimmed.slice(cut);
}
