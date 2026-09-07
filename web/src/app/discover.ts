import type { DiscoverResult, DiscoverSkipped } from '../api/types';

/**
 * The two things the scan controls have to work out for themselves.
 *
 * Both are pure functions and neither touches React, which is what makes them
 * testable: the sentence a person reads when a scan finds nothing is the whole
 * point of the feature, and it is not something to verify by looking at it.
 */

/**
 * The part of a scan's answer a sentence about it is written from: what was
 * skipped, and how deep the scan that skipped it was allowed to go.
 */
type ScanSummary = Pick<DiscoverResult, 'skipped' | 'depth' | 'depth_limit'>;

interface SkipReason {
  readonly count: (skipped: DiscoverSkipped) => number;
  readonly one: (scan: ScanSummary) => string;
  readonly many: (count: number, scan: ScanSummary) => string;
}

/**
 * What to do about a directory the scan never reached.
 *
 * Raising the depth is the move at every depth but the last one. At the
 * ceiling there is no raising it — the same screen prints that maximum in the
 * field's own label — and a sentence that asks for it anyway leaves somebody
 * exactly where the empty state left them before it existed: told what is
 * wrong and offered a control that refuses.
 */
function deeperMove(scan: ScanSummary): string {
  return scan.depth >= scan.depth_limit
    ? 'Point “Scan in” further down the tree.'
    : 'Raise the depth to look further down.';
}

/**
 * The reasons a scan skipped a directory, in the order they are worth reading.
 *
 * A scan of a home directory skips thousands of directories, so listing every
 * reason says as little as listing none. The order settles ties, and it is not
 * arbitrary: a repository five levels down under a default of four is the
 * commonest way for a scan to miss something a person knows is there, and one
 * under a dotted directory is the next.
 */
const SKIP_REASONS: readonly SkipReason[] = [
  {
    count: (skipped) => skipped.too_deep,
    one: (scan) => `1 directory was deeper than the scan depth. ${deeperMove(scan)}`,
    many: (count, scan) =>
      `${count} directories were deeper than the scan depth. ${deeperMove(scan)}`,
  },
  {
    count: (skipped) => skipped.dotted,
    one: () =>
      '1 directory was skipped for a name beginning with a dot. Point “Scan in” at it to look inside.',
    many: (count) =>
      `${count} directories were skipped for names beginning with a dot. Point “Scan in” at one to look inside.`,
  },
  {
    count: (skipped) => skipped.ignored_name,
    one: () =>
      '1 directory was skipped by name — node_modules, vendor, target, .cache. Point “Scan in” at it to look inside.',
    many: (count) =>
      `${count} directories were skipped by name — node_modules, vendor, target, .cache. Point “Scan in” at one to look inside.`,
  },
  {
    count: (skipped) => skipped.worktrees,
    one: () => '1 linked worktree was skipped. Turn on “Include linked worktrees” to list it.',
    many: (count) =>
      `${count} linked worktrees were skipped. Turn on “Include linked worktrees” to list them.`,
  },
  {
    count: (skipped) => skipped.submodules,
    one: () => '1 submodule was skipped. Turn on “Include submodules” to list it.',
    many: (count) => `${count} submodules were skipped. Turn on “Include submodules” to list them.`,
  },
  {
    count: (skipped) => skipped.unreadable,
    one: () => '1 directory could not be read, and nothing below it was scanned.',
    many: (count) => `${count} directories could not be read, and nothing below them was scanned.`,
  },
  {
    count: (skipped) => skipped.not_a_repository,
    one: () => '1 directory holds a .git that git does not recognise as a repository.',
    many: (count) =>
      `${count} directories hold a .git that git does not recognise as a repository.`,
  },
];

/**
 * The one sentence that turns "no repositories found" into a next move, or
 * undefined when the scan skipped nothing and there is nothing to add.
 *
 * The reason with the highest count, and only that one. It is a guess, and the
 * best one available: the scan cannot know which directory was being looked
 * for, so it names the reason likeliest to be hiding it.
 */
export function scanSkipExplanation(scan: ScanSummary): string | undefined {
  let chosen: SkipReason | undefined;
  let highest = 0;

  for (const reason of SKIP_REASONS) {
    const count = reason.count(scan.skipped);
    // Strictly greater, so a tie keeps the reason listed first.
    if (count > highest) {
      chosen = reason;
      highest = count;
    }
  }

  if (chosen === undefined) {
    return undefined;
  }
  return highest === 1 ? chosen.one(scan) : chosen.many(highest, scan);
}

/**
 * What the depth field holds after somebody has typed in it: a depth to ask
 * for, or '' for a box emptied on purpose, which hands the choice back to the
 * daemon.
 *
 * '' and not undefined, because undefined already means something else in that
 * field — never touched — and collapsing the two would refill the box the
 * moment it was cleared, which is a box nobody can retype.
 */
export function scanDepthFromInput(typed: string): number | '' {
  const parsed = Number.parseInt(typed, 10);
  return Number.isNaN(parsed) ? '' : parsed;
}

/**
 * The depth a typed number actually stands for, under the ceiling the last
 * scan reported.
 *
 * Held here rather than applied as the digits arrive, and that is the whole of
 * it: the ceiling comes back with a scan, so a keystroke lands while no
 * ceiling is known — the first of a session, and every one aimed at the scan
 * already running, whose own answer is what would have carried the number.
 * Clamping on the way in left 12 in a box labelled 1–8, beside a scan the
 * daemon had silently capped at 8, which is the interface saying something
 * untrue. Clamping where the value is read means the box corrects itself the
 * moment a ceiling exists to correct it against.
 *
 * A depth below one scans nothing at all, so there is a floor as well.
 */
export function boundedScanDepth(depth: number, limit: number | undefined): number {
  const atLeastOne = Math.max(1, depth);
  return limit === undefined ? atLeastOne : Math.min(atLeastOne, limit);
}
