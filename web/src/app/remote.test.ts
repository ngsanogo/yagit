import { describe, expect, it } from 'vitest';

import type { WorkingDirectory } from '../api/types';
import {
  canForcePush,
  forcePushUnavailableReason,
  pullDescription,
  pushDescription,
  remoteOffers,
} from './remote';

/**
 * What the network buttons are, for a repository in each of the states it can
 * be in.
 *
 * Every case here is one where being wrong shows no error at all. A Push
 * button on a detached HEAD runs a command git will refuse; a Pull button on a
 * branch that follows nothing runs one whose failure reads as a network
 * problem; a Publish drawn where a Push belonged sends a branch to a second
 * name on the server and follows it there forever.
 */

function status(overrides: Partial<WorkingDirectory> = {}): WorkingDirectory {
  return {
    branch: 'main',
    detached: false,
    head_sha: 'a2801ba',
    unborn: false,
    upstream: 'origin/main',
    ahead: 0,
    behind: 0,
    state: { operation: '' },
    files: [],
    ...overrides,
  };
}

describe('remoteOffers', () => {
  it('offers nothing at all in a repository with no remote', () => {
    const offers = remoteOffers(status(), 0);

    expect(offers.canFetch).toBe(false);
    expect(offers.pull.kind).toBe('unavailable');
    expect(offers.push.kind).toBe('unavailable');
  });

  it('offers all three on a branch that follows one', () => {
    const offers = remoteOffers(status({ ahead: 2, behind: 3 }), 1);

    expect(offers.canFetch).toBe(true);
    expect(offers.pull).toEqual({ kind: 'pull', behind: 3 });
    expect(offers.push).toEqual({ kind: 'push', ahead: 2, behind: 3 });
    expect(offers.diverged).toBe(true);
  });

  it('offers a publish, not a push, on a branch that follows nothing', () => {
    // Two different operations: this one needs a remote chosen by somebody and
    // records the choice, and it is the only one that can happen at all here.
    const offers = remoteOffers(status({ branch: 'feature', upstream: undefined }), 1);

    expect(offers.push).toEqual({ kind: 'publish', branch: 'feature' });
    // And nothing to pull: there is no branch on the other side yet.
    expect(offers.pull.kind).toBe('unavailable');
  });

  it('refuses both on a detached HEAD, and still fetches', () => {
    // Fetching writes under refs/remotes and nowhere else, so it is true
    // wherever HEAD happens to be — which is what makes it the thing to offer
    // when the other two cannot be.
    const offers = remoteOffers(status({ detached: true, branch: '', upstream: undefined }), 1);

    expect(offers.canFetch).toBe(true);
    expect(offers.pull.kind).toBe('unavailable');
    expect(offers.push.kind).toBe('unavailable');
    if (offers.push.kind === 'unavailable') {
      expect(offers.push.reason).toContain('detached');
    }
  });

  it('refuses both on a branch with no commit yet', () => {
    const offers = remoteOffers(
      status({ unborn: true, head_sha: '', upstream: undefined, branch: 'main' }),
      1,
    );

    expect(offers.push.kind).toBe('unavailable');
    expect(offers.pull.kind).toBe('unavailable');
  });

  it('waits rather than guessing while the status is still loading', () => {
    // The guess would be a Push button on what turns out to be a detached
    // HEAD, and it would be on screen for exactly as long as it takes somebody
    // to click it.
    const offers = remoteOffers(undefined, 1);

    expect(offers.canFetch).toBe(true);
    expect(offers.push.kind).toBe('unavailable');
  });

  it('names that wait the way the pane below names it', () => {
    // Word for word what the workbench puts on the spinner for the same wait,
    // trailing "…" included — which is to say not included. In this interface
    // the mark means a control asks a question before it acts, and these two
    // reasons were spending it on "in progress": one mark, two opposite
    // promises, and the same sentence spelt two ways on one screen.
    const offers = remoteOffers(undefined, 1);

    if (offers.pull.kind !== 'unavailable' || offers.push.kind !== 'unavailable') {
      throw new Error('a status that has not answered refuses both');
    }
    expect(offers.pull.reason).toBe('Reading the work tree');
    expect(offers.push.reason).toBe('Reading the work tree');
  });
});

describe('canForcePush', () => {
  it('is offered once the branch has moved away from the remote', () => {
    // What an amend leaves: one commit ahead and one behind, the same work
    // said differently.
    expect(canForcePush({ kind: 'push', ahead: 1, behind: 1 })).toBe(true);
    // And what a reset leaves: nothing ahead, and commits on the remote this
    // branch no longer has.
    expect(canForcePush({ kind: 'push', ahead: 0, behind: 2 })).toBe(true);
  });

  it('is not offered when there is nothing to force', () => {
    // The command would be "Everything up-to-date", run with the two most
    // dangerous flags in the application attached to it.
    expect(canForcePush({ kind: 'push', ahead: 0, behind: 0 })).toBe(false);
    expect(canForcePush({ kind: 'publish', branch: 'feature' })).toBe(false);
    expect(canForcePush({ kind: 'unavailable', reason: 'no' })).toBe(false);
  });

  it('says why a force push is refused', () => {
    expect(forcePushUnavailableReason({ kind: 'push', ahead: 0, behind: 0 })).toContain(
      'moved away',
    );
    expect(forcePushUnavailableReason({ kind: 'publish', branch: 'feature' })).toContain(
      'Publish it first',
    );
    expect(forcePushUnavailableReason({ kind: 'unavailable', reason: 'HEAD is detached.' })).toBe(
      'HEAD is detached.',
    );
  });

  // One state, one description. Pull and force push are refused by the same
  // fact — the branch follows nothing — and they used to say so in two
  // sentences that had drifted: one said the branch "follows no branch on a
  // remote", the other that it "is not on a remote yet", which is a stronger
  // claim than the reading supports and one its author can check and find
  // false. They are built from one sentence now, and this is what says so.
  it('describes a branch with no upstream the same way for pull and force push', () => {
    const offers = remoteOffers(status({ branch: 'feature', upstream: '' }), 1);

    expect(offers.push.kind).toBe('publish');
    if (offers.pull.kind !== 'unavailable') {
      throw new Error('a branch that follows nothing has nothing to pull');
    }

    const forced = forcePushUnavailableReason(offers.push);
    expect(offers.pull.reason).toContain('feature follows no branch on a remote');
    expect(forced).toContain('feature follows no branch on a remote');
    expect(forced).not.toContain('not on a remote yet');

    // Same state, same words about it — only the verb differs.
    expect(offers.pull.reason.replace('pull', 'force-update')).toBe(forced);
  });
});

describe('the sentences under the buttons', () => {
  it('names the counts and the upstream rather than the verb', () => {
    expect(pullDescription({ kind: 'pull', behind: 3 }, 'origin/main')).toBe(
      'Bring in 3 commits from origin/main',
    );
    expect(pushDescription({ kind: 'push', ahead: 1, behind: 0 }, 'origin/main')).toBe(
      'Send 1 commit to origin/main',
    );
  });

  it('says a push will be refused before it is attempted', () => {
    // git refuses a non-fast-forward push, and it is right to. Saying so here
    // is cheaper than the round trip that finds out, and it names the fix.
    expect(pushDescription({ kind: 'push', ahead: 2, behind: 1 }, 'origin/main')).toContain(
      'pull first',
    );
  });

  it('does not call a pull with nothing behind a no-op', () => {
    // `git pull` fetches before it integrates, so it is the one of the three
    // that always does something. The count is what was true at the last
    // fetch, and this button is how somebody who has not fetched finds out.
    expect(pullDescription({ kind: 'pull', behind: 0 }, 'origin/main')).toBe(
      'Fetch origin/main and bring in anything new',
    );
  });

  it('passes the reason through when the operation cannot happen', () => {
    expect(pullDescription({ kind: 'unavailable', reason: 'HEAD is detached.' }, undefined)).toBe(
      'HEAD is detached.',
    );
  });
});
