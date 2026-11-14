import type { CompareResult } from '../api/types';
import { pluralize } from '../lib/format';

/**
 * One sentence about how far HEAD and another tip have drifted.
 *
 * The counts come from the daemon; this only turns them into English the
 * compare dialog shows.
 */
export function compareSummary(result: CompareResult): string {
  const here = result.head === '' ? 'HEAD' : result.head;
  const there = result.against_short;
  if (result.ahead === 0 && result.behind === 0) {
    return `${here} and ${there} point at the same commit.`;
  }
  if (result.behind === 0) {
    return `${here} is ${pluralize(result.ahead, 'commit')} ahead of ${there}.`;
  }
  if (result.ahead === 0) {
    return `${here} is ${pluralize(result.behind, 'commit')} behind ${there}.`;
  }
  return (
    `${here} is ${pluralize(result.ahead, 'commit')} ahead of ${there} ` +
    `and ${pluralize(result.behind, 'commit')} behind it.`
  );
}
