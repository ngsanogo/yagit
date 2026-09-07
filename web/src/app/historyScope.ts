import { pluralize } from '../lib/format';
import type { Segment } from '../components/SegmentedControl';
import type { HistoryScope, Ref } from '../api/types';

/**
 * Which refs the picture on screen is drawn from.
 *
 * The scope and the chosen refs travel together everywhere — the history
 * pages, one commit's position in them, a search — because under `scope=refs`
 * the chosen set IS the scope: main alone and main with a topic branch are two
 * different histories, not two views of one (docs/adr/0033).
 */

/**
 * The chosen set as one cache key.
 *
 * Sorted, because the order refs were ticked in is nobody's business: the same
 * three refs picked in a different sequence are the same walk, and a key that
 * disagreed would refetch every page to answer with identical rows. The
 * daemon's own store key does the same thing for the same reason.
 *
 * Empty under the two scopes that do not read refs, so ticking a ref and then
 * switching to "Current branch" does not invalidate a history that never
 * depended on it.
 */
export function refsKey(scope: HistoryScope, refs: readonly string[]): string {
  if (scope !== 'refs') {
    return '';
  }
  return [...new Set(refs)].sort().join('\u0000');
}

/**
 * What "Selected references" starts from: the branch HEAD is on, when there is
 * one.
 *
 * Something rather than nothing, and that is the whole point. The daemon
 * refuses `scope=refs` with no refs — an empty walk drawn is indistinguishable
 * from an empty repository — so a picker that opened empty would open on a
 * refusal. Starting on what is already on screen makes the first click a
 * narrowing or a widening rather than a repair.
 */
export function initialSelectedRefs(refs: readonly Ref[], headName: string | undefined): string[] {
  if (headName !== undefined && headName !== '') {
    const current = refs.find((ref) => ref.kind === 'branch' && ref.short_name === headName);
    if (current !== undefined) {
      return [current.name];
    }
  }
  // A detached HEAD, or a branch with no commit yet: the first local branch
  // there is, and failing that the first reference of any kind — a mirror
  // holding only remote-tracking refs is a repository somebody reads.
  //
  // An empty list only where there is nothing at all, which the caller has to
  // keep off the screen: the daemon refuses `scope=refs` with none.
  const branch = refs.find((ref) => ref.kind === 'branch') ?? refs[0];
  return branch === undefined ? [] : [branch.name];
}

/**
 * The three sets of refs the graph can be drawn from, as the switch offers
 * them.
 *
 * "All references" rather than "All branches" because that is what `--all` is:
 * a repository's tags are most of what makes its graph three hundred columns
 * wide, and a label naming only branches would leave the reason for the width
 * off the screen. The other option names the ordinary case; a detached HEAD
 * draws what is checked out under the same option, which is what the daemon
 * walks either way (docs/adr/0016).
 */
export const HISTORY_SCOPES: readonly Segment<HistoryScope>[] = [
  { value: 'head', label: 'Current branch' },
  { value: 'all', label: 'All references' },
  { value: 'refs', label: 'Selected' },
];

/**
 * The walk on screen, in the words a sentence about it needs.
 *
 * The same three phrases the daemon uses in its own refusals — see
 * git.Scope.Describe — because a search that says it covered one thing and a
 * 404 that names another is two answers about one walk.
 */
export function describeScope(scope: HistoryScope, chosen: number): string {
  switch (scope) {
    case 'all':
      return 'every reference';
    case 'refs':
      return pluralize(chosen, 'chosen reference');
    default:
      return 'what is checked out';
  }
}
