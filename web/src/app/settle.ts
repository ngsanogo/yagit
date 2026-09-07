import type { QueryClient } from '@tanstack/react-query';

import type { RefsPayload, Stash } from '../api/types';

/**
 * What each kind of change leaves stale, in one place.
 *
 * A dozen mutations across three hooks — checking out, creating a branch,
 * renaming one, deleting one, merging, rebasing, cherry-picking, reverting,
 * resetting, fetching, pulling, pushing, and each of those failing — end by
 * dropping the same handful of queries, and they drop them for reasons that
 * belong to git rather than to any one of them: HEAD moved, so the files on
 * disk are somebody else's; a ref moved, so every badge drawn on a row moved
 * with it. Written out a dozen times, the list is a dozen chances for one
 * entry to be forgotten, and a forgotten entry is a screen that quietly
 * disagrees with the repository until the next poll.
 *
 * So the lists live here and the reasoning lives with them. What stays in the
 * hooks is what HAPPENED, which is the part that differs.
 *
 * One function rather than three that a caller combines, and the difference is
 * not tidiness. The three situations overlap — a stopped merge moved
 * references AND rewrote the work tree — so a caller in two of them at once
 * used to ask for `status` twice, which is two refetches of one thing and, for
 * the next reader, two lists that both look like they own it. Here the
 * situations are arguments, the keys are collected before anything is dropped,
 * and asking twice is not something a caller can do.
 */

/** The queries this screen keeps, by the name at the head of their key. */
type QueryName =
  | 'refs'
  | 'commits'
  | 'commit'
  | 'status'
  | 'diff'
  | 'prepared-message'
  | 'stashes'
  | 'stash'
  | 'undo';

/** What an operation did, in the terms that decide what is now stale. */
export interface Settled {
  /**
   * The references the operation answered with, or `'stale'` when it answered
   * none.
   *
   * A written answer is worth the distinction: the daemon read the references
   * a moment ago, after the operation, so asking again is a second
   * `for-each-ref` for something already in hand. A failure answers with an
   * error instead — and it still moved references, because a pull that stops
   * on a conflict has already fetched and a `--all` fetch that could not reach
   * one remote has brought back the others.
   */
  refs: RefsPayload | 'stale';

  /**
   * HEAD points at another commit, so the files under it are another commit's.
   *
   * The work tree goes with it, and so does the prepared message; see
   * workTree below, which is the same reading without the commit having
   * changed.
   */
  headMoved?: boolean;

  /**
   * The files on disk are not what they were.
   *
   * Told apart from headMoved above because a stash moves the work tree
   * without moving HEAD: pushing one puts every changed file back to its HEAD
   * version, popping one writes them out again, and HEAD sits where it always
   * was. The prepared message is deliberately not in this list — git writes
   * MERGE_MSG and its siblings from an operation, and stashing is not one.
   */
  workTree?: boolean;

  /**
   * The stash stack the operation answered with, `'stale'` when it answered
   * none, and absent when the stack cannot have changed.
   *
   * Written where there is an answer, for the reason refs above is: every
   * stash route replies with the list as it stands afterwards, so asking again
   * would be a second `git stash list` for something already in hand. It also
   * closes a gap this family cannot afford — a stash is addressed by position,
   * so a list stale for one render is a list whose rows carry the wrong
   * number.
   *
   * Its own field rather than a consequence of workTree, because the two come
   * apart in both directions: staging a file changes the work tree and no
   * stash, and a drop refused because the stack moved changed the stack
   * without touching a single file.
   */
  stashes?: Stash[] | 'stale';

  /**
   * Where the branch stands against its upstream moved.
   *
   * The three commands that leave the machine all change it, including when
   * they fail halfway, and the ahead/behind counts on those very buttons are
   * read from the status rather than from the references.
   */
  tracking?: boolean;
}

/**
 * Drops what an operation made stale, and writes what it answered.
 *
 * Awaited by its callers for the cancel: a request already in flight — the
 * poll, or a click before this one — must not land on top of the references
 * with the list as it was before.
 */
export async function settle(client: QueryClient, id: string, what: Settled) {
  // A set, so that two reasons for the same query are still one refetch.
  const stale = new Set<QueryName>();

  // The history always. A branch is drawn on the row it points at, so a name
  // that appeared, moved or went takes a badge with it, and under `scope=head`
  // a walk that started at HEAD is a different walk entirely.
  stale.add('commits');
  stale.add('commit');

  if (what.refs === 'stale') {
    stale.add('refs');
  } else {
    await client.cancelQueries({ queryKey: ['refs', id] });
    client.setQueryData(['refs', id], what.refs);
  }

  if (what.headMoved === true || what.workTree === true) {
    stale.add('status');
    stale.add('diff');
  }

  // Only where HEAD moved. git starts the next commit's message from where
  // HEAD is — MERGE_MSG after a merge that stopped, the replaced commit under
  // an amend — and that query never goes stale on its own, so nothing else
  // would ever drop it.
  if (what.headMoved === true) {
    stale.add('prepared-message');
    // The tip's undo offer is a reading of the HEAD reflog; a new tip is a
    // different offer, or none.
    stale.add('undo');
  }

  if (what.stashes !== undefined) {
    if (what.stashes === 'stale') {
      stale.add('stashes');
    } else {
      await client.cancelQueries({ queryKey: ['stashes', id] });
      client.setQueryData(['stashes', id], what.stashes);
    }

    // The open stash's own patch either way, and this one is never written.
    // It is keyed by POSITION, and a push or a drop renumbers every entry —
    // so a cached answer would be drawn under a row that is now a different
    // stash.
    stale.add('stash');
  }

  if (what.tracking === true) {
    stale.add('status');
  }

  for (const name of stale) {
    // Not awaited: an invalidation resolves when the refetch it starts comes
    // back, and nothing here is waiting for a screen to have redrawn.
    void client.invalidateQueries({ queryKey: [name, id] });
  }
}
