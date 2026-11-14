import type { CompareResult } from '../api/types';
import { pluralize } from '../lib/format';

/**
 * One sentence about how far HEAD and another tip have drifted.
 *
 * The counts come from the daemon; this only turns them into English the
 * history dialog can put above its Merge and Rebase buttons.
 */
export function compareSummary(result: CompareResult): string {
  const here = result.head === '' ? 'HEAD' : result.head;
  const there = result.against_short;
  if (result.ahead === 0 && result.behind === 0) {
    return `${here} and ${there} point at the same commit.`;
  }
  const parts: string[] = [];
  if (result.ahead > 0) {
    parts.push(`${here} is ${pluralize(result.ahead, 'commit')} ahead of ${there}`);
  }
  if (result.behind > 0) {
    parts.push(`${pluralize(result.behind, 'commit')} behind`);
  }
  if (parts.length === 1) {
    return `${parts[0]}.`;
  }
  return `${parts[0]}, and ${parts[1]}.`;
}
