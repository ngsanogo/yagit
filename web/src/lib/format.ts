/**
 * Formatting helpers shared across the interface.
 *
 * Pure functions, with no access to the DOM or to the clock: the reference
 * instant is always passed in. That is what makes them testable without
 * faking time, and it is also what guarantees that a whole list of commits is
 * dated against the same instant.
 */

/** Display length of a SHA. Seven characters: git's convention. */
export const SHORT_SHA_LENGTH = 7;

export function shortenSha(sha: string, length: number = SHORT_SHA_LENGTH): string {
  return sha.slice(0, length);
}

/**
 * A commit as a sentence names it: the short SHA, and its subject where it has
 * one.
 *
 * One definition because every confirmation that talks about a commit says it
 * the same way, and two copies are two chances for one of them to drop the
 * subject — or to reach for a different quotation mark.
 */
export function namedCommit(sha: string, subject: string): string {
  const short = shortenSha(sha);
  return subject === '' ? short : `${short} (“${subject}”)`;
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * Renders a readable relative date: "3 minutes ago".
 *
 * Past a week it switches to an absolute date: at that distance "47 days ago"
 * no longer helps anyone place an event, whereas a date does.
 */
export function formatRelativeTime(value: Date, now: Date): string {
  const elapsed = now.getTime() - value.getTime();

  // A date in the future means a skewed clock, not a bug in yagit. Show it
  // as it is rather than inventing "in -3 minutes".
  if (elapsed < 0) {
    return formatAbsoluteTime(value);
  }
  if (elapsed < MINUTE) {
    return 'just now';
  }
  if (elapsed < HOUR) {
    return pluralize(Math.floor(elapsed / MINUTE), 'minute') + ' ago';
  }
  if (elapsed < DAY) {
    return pluralize(Math.floor(elapsed / HOUR), 'hour') + ' ago';
  }
  if (elapsed < 7 * DAY) {
    return pluralize(Math.floor(elapsed / DAY), 'day') + ' ago';
  }
  return formatAbsoluteTime(value);
}

export function formatAbsoluteTime(value: Date): string {
  return value.toLocaleDateString('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  });
}

export function pluralize(count: number, singular: string): string {
  return `${count} ${singular}${count === 1 ? '' : 's'}`;
}

/**
 * A count, its noun, and a verb that agrees with them.
 *
 * pluralize alone is enough for a noun phrase — "in 3 files", "2 commits of
 * its own" — and stops being enough the moment the phrase becomes a clause.
 * English does not derive the verb from the noun, so a sentence built by
 * concatenation reads "1 file differ", and a reader who notices that stops
 * trusting the number in front of it.
 *
 * Both forms are given rather than an "s" appended to one, because the verbs
 * that turn up here do not take one: is/are, has/have, goes/go.
 */
export function counted(count: number, noun: string, one: string, many: string): string {
  return `${pluralize(count, noun)} ${count === 1 ? one : many}`;
}

/**
 * An author's initials, for the author chip.
 *
 * Two letters at most: past that the chip turns into a label and loses its
 * job, which is to be recognized out of the corner of the eye.
 */
export function initialsFromName(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) {
    return '?';
  }
  // The first CODE POINT, not the first UTF-16 code unit. charAt(0) on
  // "👩 Smith" returns half a surrogate pair: a lone \uD83D, which is not
  // well-formed text and renders as a replacement character.
  const initial = (word: string | undefined): string =>
    word === undefined ? '' : ([...word][0] ?? '');

  const first = initial(words[0]);
  const last = words.length > 1 ? initial(words[words.length - 1]) : '';
  return (first + last).toUpperCase();
}

/**
 * Stable index derived from a string, used to pick a tint.
 *
 * The same author keeps the same color from one session to the next: that is
 * what makes the chip useful for spotting people. This is not a cryptographic
 * hash and has no reason to be one.
 */
export function stableIndex(text: string, buckets: number): number {
  let accumulator = 0;
  for (let position = 0; position < text.length; position += 1) {
    accumulator = (accumulator * 31 + text.charCodeAt(position)) % 1_000_000_007;
  }
  return accumulator % buckets;
}
