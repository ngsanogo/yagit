/**
 * The two questions the interface asks of a path it was handed.
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
