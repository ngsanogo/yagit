import type { ReactNode } from 'react';

/**
 * The ceiling on how many lines a pane draws at once, and the two sentences
 * that say it was reached.
 *
 * A file of its own because three views share it and none of them owns it. It
 * used to live in DiffView, where the blame panel and the diff poll both had
 * to reach for it — and where the second view to want the sentences wrote its
 * own pair, down to a comparison spelled the other way round. Held here, a
 * change to where the cap bites is one change.
 *
 * The sentences are shared for a reason of their own, which is the reason the
 * copy gave for being a copy: blame and a patch open in the same slot, and a
 * reader who has learned the sentence in one should not have to learn it
 * again in the other. Two copies of a promise to say the same thing are two
 * things to keep saying it.
 */

/**
 * How many lines are drawn before a view says it is not drawing the rest.
 *
 * A generated file, a lockfile, a vendored dependency: diffs of tens of
 * thousands of lines exist and nobody reads them line by line. Past this the
 * lines stop and the number is named, the way the graph names the width it
 * will not draw — the whole-file actions still work, because they are `git
 * add` and need no patch at all.
 *
 * Counted across everything drawn at once, not per file: a commit's patch is
 * every file it touched, and a cap granted to each of five hundred files is no
 * cap at all.
 */
export const MAX_DRAWN_LINES = 2000;

/**
 * Whether the cap bit.
 *
 * One comparison, so no view can draw a capped pane and fail to say so — and
 * so the notices that say it cannot disagree about when to appear.
 */
export function overCap(lines: number): boolean {
  return lines > MAX_DRAWN_LINES;
}

/**
 * That the pane is cut, said where the reader begins.
 *
 * The paragraph at the foot is the honest full version and it is two thousand
 * rows away: on a rewritten lockfile it sits fifty screens down, which is a
 * sentence only somebody who already knows the pane is cut will ever reach. A
 * reader starting at the top otherwise has no way to learn that the file
 * continues.
 */
export function Capped({ lines, children }: { lines: number; children?: ReactNode }) {
  if (!overCap(lines)) {
    return null;
  }

  return (
    <p className="border-b border-line bg-sunken px-3 py-2 font-sans text-2xs text-ink-muted">
      <span className="text-ink">
        Showing the first {MAX_DRAWN_LINES.toLocaleString('en-GB')} lines
      </span>{' '}
      of {lines.toLocaleString('en-GB')}. {children}
    </p>
  );
}

/**
 * The same fact where the rows stop, for the reader who scrolled to the end.
 *
 * The comparison lives with the number rather than at each call site, so a
 * view cannot draw a truncated pane and forget to say that it did.
 *
 * `subject` is the one word the two panes disagree on — the blame of a file
 * and the patch of a commit are not the same object, and calling either by
 * the other's name would be the shared sentence lying to save a prop. The
 * numbers are grouped, because four digits run together as `2000` and the two
 * this sentence compares are read against each other.
 */
export function Truncated({
  lines,
  subject,
  children,
}: {
  lines: number;
  subject: 'file' | 'patch';
  children?: ReactNode;
}) {
  if (!overCap(lines)) {
    return null;
  }

  return (
    <p className="border-t border-line px-3 py-2 font-sans text-2xs text-ink-muted">
      {lines.toLocaleString('en-GB')} lines in this {subject}; the first{' '}
      {MAX_DRAWN_LINES.toLocaleString('en-GB')} are shown. {children}
    </p>
  );
}
