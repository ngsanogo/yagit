import { afterEach, expect, it, vi } from 'vitest';

import { api } from './client';

/**
 * The one thing about a request that fails silently.
 *
 * The daemon reads `?scope=` and falls back to the current branch when it is
 * absent — deliberately, because absence is a client that has not chosen. That
 * fallback is exactly why a misspelled or dropped parameter is worth a test: it
 * is not an error on either side. The daemon answers a perfectly good history,
 * and the only symptom is a control on screen that quietly stops doing
 * anything.
 */

/** Collects the paths asked for, and answers each with an empty page. */
function recordRequests(): string[] {
  const requested: string[] = [];

  vi.stubGlobal('fetch', (path: string) => {
    requested.push(path);
    return Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve({}),
    });
  });

  return requested;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

it('asks for a page of history by number and by scope', async () => {
  const requested = recordRequests();

  await api.commits('abc123', 3, 'head');
  await api.commits('abc123', 0, 'all');

  expect(requested).toEqual([
    '/api/repos/abc123/commits?page=3&scope=head',
    '/api/repos/abc123/commits?page=0&scope=all',
  ]);
});
