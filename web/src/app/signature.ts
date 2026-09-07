import type { BadgeTone } from '../components/Badge';

/**
 * What git's signature verdict says, in words somebody can act on.
 *
 * The daemon sends `%G?` through as the letter git printed, and the mapping
 * lives here rather than in the pane so it can be asserted: eight cases, and
 * getting one of them wrong means telling somebody a commit is trustworthy
 * when git said the opposite.
 *
 * Three tones and not two, because "signed" is not a yes-or-no. A good
 * signature from a key this machine does not trust, an expired key, a
 * signature git had no key to check at all — none of those is a forgery and
 * none is a clean verification either, and collapsing them into one colour is
 * how a badge stops meaning anything.
 */
export interface SignatureNote {
  label: string;
  tone: BadgeTone;
}

/**
 * The badge for one verdict, or undefined where there is nothing to say.
 *
 * 'N' — no signature — is the ordinary case and gets no badge. Most commits in
 * most repositories are unsigned, and a badge on every row would be a badge
 * nobody reads; the ones worth a colour are the ones that are not ordinary.
 *
 * An empty string is the same silence, for a different reason: an older daemon
 * that did not send the field, and a repository is not less trustworthy for
 * being read by one.
 */
export function signatureNote(verdict: string): SignatureNote | undefined {
  switch (verdict) {
    case 'G':
      return { label: 'signature verified', tone: 'success' };
    case 'B':
      return { label: 'BAD signature', tone: 'danger' };
    case 'U':
      return { label: 'signed, trust unknown', tone: 'warning' };
    case 'X':
      return { label: 'signed, signature expired', tone: 'warning' };
    case 'Y':
      return { label: 'signed by an expired key', tone: 'warning' };
    case 'R':
      return { label: 'signed by a revoked key', tone: 'danger' };
    case 'E':
      return { label: 'signature not checkable here', tone: 'info' };
    default:
      return undefined;
  }
}
