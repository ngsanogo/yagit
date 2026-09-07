import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { NETWORK_IDLE_MS, idleDeadline, waitedFor } from './fetchTimeout';

describe('idleDeadline', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('gives up when nothing arrives', () => {
    const deadline = idleDeadline(1000);
    expect(deadline.signal.aborted).toBe(false);

    vi.advanceTimersByTime(1001);
    expect(deadline.signal.aborted).toBe(true);
  });

  it('does not give up while the stream keeps talking', () => {
    // The case an elapsed-time limit got wrong: a clone of a large repository
    // is an hour of legitimate work that reports progress the whole way.
    const deadline = idleDeadline(1000);

    for (let elapsed = 0; elapsed < 10_000; elapsed += 900) {
      vi.advanceTimersByTime(900);
      deadline.alive();
    }

    expect(deadline.signal.aborted).toBe(false);
  });

  it('gives up once the talking stops', () => {
    const deadline = idleDeadline(1000);

    vi.advanceTimersByTime(900);
    deadline.alive();
    expect(deadline.signal.aborted).toBe(false);

    // Silence from here.
    vi.advanceTimersByTime(1001);
    expect(deadline.signal.aborted).toBe(true);
  });

  it('stops counting once the stream has ended', () => {
    // A timer left armed aborts nothing, but it keeps the page awake for the
    // whole allowance after the work it was watching finished.
    const deadline = idleDeadline(1000);
    deadline.settled();

    vi.advanceTimersByTime(5000);
    expect(deadline.signal.aborted).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('aborts with a reason the client can tell apart from a cancelled request', () => {
    const deadline = idleDeadline(1000);
    vi.advanceTimersByTime(1001);

    const reason: unknown = deadline.signal.reason;
    expect(reason).toBeInstanceOf(DOMException);
    expect((reason as DOMException).name).toBe('TimeoutError');
  });

  it('defaults to the shared allowance', () => {
    const deadline = idleDeadline();
    vi.advanceTimersByTime(NETWORK_IDLE_MS - 1);
    expect(deadline.signal.aborted).toBe(false);

    vi.advanceTimersByTime(2);
    expect(deadline.signal.aborted).toBe(true);
  });
});

describe('waitedFor', () => {
  it('says the number a person would say', () => {
    expect(waitedFor(35_000)).toBe('35 seconds');
    expect(waitedFor(NETWORK_IDLE_MS)).toBe('5 minutes');
  });
});
