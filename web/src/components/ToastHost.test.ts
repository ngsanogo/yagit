import { describe, expect, it } from 'vitest';

import { nextStack } from './ToastHost';

/**
 * The two decisions that keep a run of failures from taking the screen: how
 * many notifications may be drawn at once, and when two of them are one.
 *
 * Worth testing away from the component because the failure mode is not a
 * broken render — it is a stack that grows past the top of the window, where
 * the oldest card's dismiss button is unreachable and the header actions are
 * behind the message telling the user to try again.
 */
describe('nextStack', () => {
  const failed = { tone: 'danger' as const, title: 'Could not fetch from origin' };

  it('collapses a repeat of the newest notification into one card', () => {
    // Three clicks on a remote that is not there. One card, and it says so.
    const once = nextStack([], { ...failed, detail: 'first' }, 1);
    const twice = nextStack(once, { ...failed, detail: 'second' }, 2);
    const thrice = nextStack(twice, { ...failed, detail: 'third' }, 3);

    expect(thrice).toHaveLength(1);
    expect(thrice.at(0)?.repeats).toBe(3);
  });

  it('keeps the identity of the card it collapsed into', () => {
    // The id is the React key and the argument the dismiss button closes over.
    // A collapse that minted a new one would remount the card under the
    // pointer and leave the cross pointing at a toast that no longer exists.
    const once = nextStack([], { ...failed, detail: 'first' }, 7);
    const twice = nextStack(once, { ...failed, detail: 'second' }, 8);

    expect(twice.at(0)?.id).toBe(7);
    // The newest attempt's account, not the first one's: it is the one the
    // user has just caused.
    expect(twice.at(0)?.detail).toBe('second');
  });

  it('does not collapse across another notification', () => {
    // Two failures with something between them are two things that happened,
    // and folding them together would report the second as a repeat of the
    // first long after it.
    const first = nextStack([], failed, 1);
    const other = nextStack(first, { tone: 'success', title: 'Pushed main to origin/main' }, 2);
    const again = nextStack(other, failed, 3);

    expect(again.map((toast) => toast.repeats)).toEqual([1, 1]);
    expect(again.map((toast) => toast.title)).toEqual(['Pushed main to origin/main', failed.title]);
  });

  it('treats the same sentence in another tone as another notification', () => {
    const refused = nextStack([], { tone: 'danger', title: 'Stashed the work tree' }, 1);
    const done = nextStack(refused, { tone: 'success', title: 'Stashed the work tree' }, 2);

    expect(done).toHaveLength(2);
  });

  it('drops the oldest rather than growing past the window', () => {
    const one = nextStack([], { tone: 'danger', title: 'Could not fetch from origin' }, 1);
    const two = nextStack(one, { tone: 'danger', title: 'Could not push main' }, 2);
    const three = nextStack(two, { tone: 'danger', title: 'Could not pull' }, 3);

    // Newest last, because the stack is anchored to the bottom corner and the
    // last child is the one in it.
    expect(three.map((toast) => toast.title)).toEqual(['Could not push main', 'Could not pull']);
  });

  it('costs the older card nothing when the newest one repeats at the cap', () => {
    // Two failures on screen and the second one happening again. Collapsing
    // is what keeps the first from being pushed out by a card that is already
    // there — and it is why the host counts arrivals rather than the length
    // of this list: nothing about the stack moved, and something did arrive.
    const one = nextStack([], { tone: 'danger', title: 'Could not fetch from origin' }, 1);
    const two = nextStack(one, { tone: 'danger', title: 'Could not push main' }, 2);
    const again = nextStack(two, { tone: 'danger', title: 'Could not push main' }, 3);

    expect(again.map((toast) => toast.title)).toEqual([
      'Could not fetch from origin',
      'Could not push main',
    ]);
    expect(again.map((toast) => toast.repeats)).toEqual([1, 2]);
  });
});
