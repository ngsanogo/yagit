import { fixtureDir, sweepStaleFixtures } from './fixtures';
import { baseURL, sessionToken } from './session';

/**
 * Clears the fixtures an older shape left behind, before the first worker
 * builds anything.
 *
 * Before the run and not after it: the suite keeps its repositories on purpose
 * — buildRepository explains that the daemon identifies a repository by its
 * git directory, so removing one it has open is the "replaced underneath" case
 * it answers with a 409 — and a teardown that deleted them would either break
 * that or run while a repository is still open.
 *
 * Before the run is not the same as nothing being open, though. `./do test
 * e2e` reuses the stack a `./do dev` is already running, and that daemon
 * outlives runs: change the fixture's shape and the repositories the previous
 * run opened are stale and still pinned. So the sweep is told what is open
 * instead of assuming, which is a request and not a race — the daemon answers
 * before this runs, `./do test e2e` having either started it and waited for it
 * or reused one that already responded.
 */
export default async function globalSetup(): Promise<void> {
  await checkFixturesAreReachable();
  sweepStaleFixtures(fixtureDir(), await openRepositories());
}

/**
 * Refuses to start a run whose fixtures the daemon would not be allowed to
 * open.
 *
 * The fixtures live in the checkout, under `.yagit/e2e/`. The daemon opens
 * nothing outside its root — that is the boundary, and it holds here like
 * anywhere else — so fixtures outside it make every test that opens a
 * repository fail at once, each with a 403 about a security boundary. The
 * cause is one line of configuration, and nothing in that message says so.
 *
 * The daemon is asked rather than an environment variable read: the suite may
 * have started its own, rooted at the fixtures, or be reusing a `./do up`
 * with whatever root that one has, and only the daemon knows which. A scan of
 * the fixture directory one level deep is refused with 403 exactly when it is
 * outside the root, and costs nothing when it is not.
 *
 * Checked here rather than in each test: it is one fact about the machine, it
 * is true or false before any browser starts, and saying it once beats saying
 * it forty times in the wrong words.
 */
async function checkFixturesAreReachable(): Promise<void> {
  const endpoint = new URL('/api/repos/discover', baseURL());
  endpoint.searchParams.set('dir', fixtureDir());
  endpoint.searchParams.set('depth', '1');

  const response = await fetch(endpoint, {
    headers: { 'X-Yagit-Token': sessionToken() },
    signal: AbortSignal.timeout(10_000),
  });
  if (response.status !== 403) {
    return;
  }

  throw new Error(
    `the end-to-end fixtures live in ${fixtureDir()}, which the running daemon ` +
      `refuses: ${await response.text()}. Every test that opens a repository ` +
      'would be refused the same way. Stop the stack that is running, or point ' +
      'its YAGIT_ROOT at a directory that contains this checkout.',
  );
}

/**
 * The paths of the repositories the daemon currently holds open.
 *
 * They are resolved paths — the registry canonicalizes one before it opens it
 * — and so are the ones the sweep builds, so the two sides of the comparison
 * are the same kind of string.
 *
 * A daemon that will not answer is not a reason to sweep blind: deleting a
 * repository it has open is the whole failure this asks about. So this throws
 * rather than guessing at an empty set. The suite needs the daemon for every
 * one of its tests anyway, and failing here says why once instead of once per
 * test.
 */
async function openRepositories(): Promise<ReadonlySet<string>> {
  const endpoint = new URL('/api/repos', baseURL());

  let response: Response;
  try {
    response = await fetch(endpoint, {
      headers: { 'X-Yagit-Token': sessionToken() },
      // Node's fetch waits forever by default, and a global setup that hangs
      // prints nothing at all — Playwright has not started a test to report
      // against. Ten seconds is long for a daemon that has just answered a
      // health check.
      signal: AbortSignal.timeout(10_000),
    });
  } catch (cause) {
    throw new Error(
      `could not ask ${endpoint} which repositories are open, which the fixture ` +
        'sweep has to know before it removes anything. The end-to-end tests need ' +
        'the daemon: run `./do test e2e`, which starts it for you.',
      { cause },
    );
  }

  if (!response.ok) {
    throw new Error(
      `asked ${endpoint} which repositories are open and got ` +
        `${response.status}: ${await response.text()}`,
    );
  }

  const payload = (await response.json()) as { repos?: { path: string }[] };
  return new Set((payload.repos ?? []).map((repository) => repository.path));
}
