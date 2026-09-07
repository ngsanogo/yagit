import { readdirSync, realpathSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';

import { projectRoot } from './session';

/**
 * The shape of the scratch repositories the end-to-end tests build, and the
 * names that carry it.
 *
 * The commit count is part of every name because buildRepository reuses a
 * repository that is already on disk: a fixture whose name did not change when
 * its shape did would be found stale forever on a machine that had run the
 * suite before. So changing a count here renames every fixture, and the
 * previous generation is left behind under `.yagit/e2e/` — which is what
 * sweepStaleFixtures is for.
 *
 * Naming and sweeping therefore live in one file and share one function. Two
 * places computing the current name is how a sweep starts deleting the
 * repositories the run is about to open.
 */

/** Where the scratch repositories live, under the checkout's runtime directory. */
export function fixtureDir(): string {
  const fromEnv = process.env['YAGIT_FIXTURE_DIR'];
  if (fromEnv !== undefined && fromEnv !== '') {
    return fromEnv;
  }
  return join(projectRoot(), '.yagit', 'e2e');
}

/**
 * Commits on the trunk of the repository these tests build.
 *
 * More than one page of history, deliberately. The daemon serves two hundred
 * rows at a time, and a fixture that fits in one of them would let every
 * paging defect through — the rows nobody fetched are exactly the rows a test
 * has to reach. Creating them costs about a third of a second.
 */
export const TRUNK_COMMITS = 250;

/**
 * Commits on the branch that leaves the trunk and comes back, plus the merge
 * that brings it back. Two columns and a merge is the smallest history whose
 * graph is a picture rather than a straight line.
 */
export const BRANCH_COMMITS = 2;

/**
 * Commits on a branch that is never merged, and never checked out.
 *
 * They are the whole difference between the two scopes of the history: they
 * are reachable from a ref and from nothing that is checked out. Without them
 * the two scopes answer the same number, and the control that chooses between
 * them could do nothing and still pass (docs/adr/0016).
 */
export const UNMERGED_COMMITS = 1;

/** Commits reachable from the current branch: the picture drawn by default. */
export const FIXTURE_COMMITS = TRUNK_COMMITS + BRANCH_COMMITS + 1;

/** Commits reachable from every ref, which is the other option on screen. */
export const FIXTURE_COMMITS_ALL_REFS = FIXTURE_COMMITS + UNMERGED_COMMITS;

/**
 * What a fixture is opened for: by path through /api/repos, or by finding it
 * in the discover list. One repository each, because the discover test scans
 * for its own name and a fixture shared with a sibling would make the run
 * order decide what it finds.
 *
 * A list rather than a union type, because FIXTURE_NAME below is built from it
 * and the type is read off it. A role the pattern did not know would be
 * classified 'unknown', which the sweep skips — so that role's directories
 * would accumulate forever with nothing reporting them, which is the leak this
 * file exists to close.
 */
export const FIXTURE_ROLES = ['worker', 'discover', 'clone'] as const;

export type FixtureRole = (typeof FIXTURE_ROLES)[number];

/**
 * The one commit that exists only in the clone, and the count that follows
 * from it.
 *
 * The clone is a copy of the worker fixture with a commit on top: two
 * checkouts of one project share their shas, which is the case a selection
 * crossing a tab switch goes wrong in, and the extra commit is how a test
 * tells the two histories apart when they are otherwise identical down to the
 * subjects.
 */
export const CLONE_ONLY_SUBJECT = 'feat: only in the clone';

/** Commits reachable from the clone's current branch. */
export const CLONE_COMMITS = FIXTURE_COMMITS + 1;

/**
 * How many commits each role's fixture carries.
 *
 * The count is in the name because it is what says which generation a
 * directory belongs to: change the shape and the name changes with it, so the
 * sweep can tell a fixture this run will open from one no test will ever ask
 * for again. Here rather than in the roles list because the clone carries one
 * more than the repository it was cloned from, and a single FIXTURE_COMMITS in
 * the name would classify a clone this run is about to open as stale.
 *
 * Every commit in the repository, not the ones a default scope draws. The
 * unmerged branch is reachable from no checkout, so a name built from
 * FIXTURE_COMMITS would not have moved when that branch was added — and every
 * run after would have asserted against the repository the run before it left,
 * which is the one thing this file exists to prevent.
 */
const FIXTURE_COMMIT_COUNTS: Record<FixtureRole, number> = {
  worker: FIXTURE_COMMITS_ALL_REFS,
  discover: FIXTURE_COMMITS_ALL_REFS,
  clone: CLONE_COMMITS + UNMERGED_COMMITS,
};

/** The name of this worker's fixture for one role. */
export function fixtureName(role: FixtureRole): string {
  return nameFor(role, process.env['TEST_WORKER_INDEX'] ?? '0');
}

function nameFor(role: FixtureRole, worker: string): string {
  return `${role}-${worker}-of-${FIXTURE_COMMIT_COUNTS[role]}`;
}

/**
 * A name this suite has produced at some point.
 *
 * The roles come from FIXTURE_ROLES rather than being spelled out again, so a
 * third role cannot be added to the list and left out of the sweep. They are
 * bare words, so there is nothing in them for a regular expression to read as
 * syntax.
 *
 * The suffix is optional because the generation before this one carried no
 * commit count at all, and those directories are the ones that have been
 * accumulating the longest. Nothing else is recognised: the sweep deletes what
 * it can name, not everything it did not expect.
 */
const FIXTURE_NAME = new RegExp(String.raw`^(${FIXTURE_ROLES.join('|')})-(\d+)(?:-of-\d+)?$`);

/**
 * The prefix on a repository one test builds entirely for itself.
 *
 * The suites that CHANGE a working directory cannot share a fixture: staging,
 * discarding and resolving are the state under test, so each of those tests
 * deletes its repository and builds it again from nothing. Nothing ever reuses
 * one, which is what makes it always removable — the opposite of the fixtures
 * above, where reuse is the point and the name is what protects it.
 *
 * The prefix exists so the sweep can tell the two apart. Without one these
 * directories read as 'unknown', which the sweep deliberately leaves alone, and
 * a git repository per test accumulates under `.yagit/e2e/` forever.
 */
const SCRATCH_PREFIX = 'scratch-';

/** Where one test's own throwaway repository goes. */
export function scratchFixture(name: string): string {
  return join(fixtureDir(), SCRATCH_PREFIX + name);
}

/**
 * The scratch names from before the prefix.
 *
 * `wd-lists`, `cf-banner`: the two suites that build their own repository used
 * to name a directory straight after the test in it, so nothing could tell one
 * from a repository somebody had put there on purpose and the sweep left every
 * one of them alone. They are the generation SCRATCH_PREFIX replaces, and
 * naming them here is what finally removes them — the same courtesy the
 * count-less names above are given, and for the same reason: the directories
 * that accumulated are the ones nobody thought to name.
 */
const LEGACY_SCRATCH = /^(wd|cf)-[a-z]+(-[a-z]+)*$/;

/** What an entry under `.yagit/e2e/` is, to a run starting now. */
export type FixtureGeneration = 'current' | 'stale' | 'scratch' | 'unknown';

export function classifyFixture(name: string): FixtureGeneration {
  // First, because a scratch name is not a generation of anything: it is
  // rebuilt from nothing by the test that wants it, so last run's copy is of
  // no use to this one whatever it is called.
  if (name.startsWith(SCRATCH_PREFIX) || LEGACY_SCRATCH.test(name)) return 'scratch';

  const [, matched, worker] = FIXTURE_NAME.exec(name) ?? [];
  if (matched === undefined || worker === undefined) return 'unknown';

  // Sound because FIXTURE_NAME is built from FIXTURE_ROLES: nothing outside
  // the list can reach here.
  const role = matched as FixtureRole;

  // Rebuilt rather than matched against a second pattern holding
  // FIXTURE_COMMITS. The name a run builds is then the only definition of
  // which generation is current, and no fixture a run is about to open can be
  // called stale by an expression that drifted away from it.
  return nameFor(role, worker) === name ? 'current' : 'stale';
}

/**
 * Removes the fixtures nothing will ask for again under `parent`, except the
 * ones the daemon still has open.
 *
 * Two kinds go: the generation an older shape left behind, and every scratch
 * repository, which is rebuilt by the test that wants it and so is never worth
 * keeping. Reusing the CURRENT generation is deliberate and buildRepository
 * says why: the daemon identifies a repository by its git directory, so
 * deleting one it already has open is the "replaced underneath" case.
 *
 * A stale fixture is usually open in nothing, but "usually" is not a
 * precondition. `./do test e2e` reuses the stack a `./do dev` is already
 * running, and that daemon outlives runs: change the fixture's shape and the
 * repositories the previous run opened are stale and pinned at the same time.
 * Deleting one leaves the daemon listing a repository whose directory is gone
 * — GET /api/repos does not re-check, so the workbench keeps opening on it and
 * fails with "no longer reachable where it was opened" on every page load.
 * That is the hazard this function is supposed to be avoiding, arrived at from
 * the other side.
 *
 * `openRepositories` is therefore what the caller found the daemon holding:
 * resolved paths, as the ones built here are. What it names is kept and said
 * to be kept, and goes on the first run after that daemon stops.
 *
 * `parent` is the checkout's `.yagit/e2e/` directory, so this is exact about
 * what it deletes and says so on stdout. A test run that silently removes
 * files elsewhere is worse than the leak it fixes.
 */
export function sweepStaleFixtures(parent: string, openRepositories: ReadonlySet<string>): void {
  let root: string;
  try {
    // Resolved once, so every path below is built from a directory whose
    // symlinks are already followed: what gets removed is then decided
    // against a place on disk rather than against the name that led there.
    root = realpathSync(parent);
  } catch (cause) {
    // No parent means no fixture has ever been built here, which is the
    // ordinary state of a fresh clone and of every CI runner.
    if ((cause as NodeJS.ErrnoException).code === 'ENOENT') return;
    throw cause;
  }

  for (const entry of readdirSync(root, { withFileTypes: true })) {
    // withFileTypes reports a symlink as a symlink whatever it points at, so
    // this is also what keeps the sweep inside the directory: a link named
    // like a stale fixture is not a directory and is left alone.
    if (!entry.isDirectory()) continue;
    const generation = classifyFixture(entry.name);
    if (generation !== 'stale' && generation !== 'scratch') continue;

    const path = join(root, entry.name);
    // readdir cannot return a name containing a separator, and this refuses
    // to trust that forever: the day the recognised shape admits one, the
    // failure is a thrown path rather than a deletion somewhere above.
    if (dirname(path) !== root) {
      throw new Error(`refusing to remove ${path}: not a direct child of ${root}`);
    }

    if (openRepositories.has(path)) {
      console.log(
        `kept the ${generation} end-to-end fixture ${path}: the daemon still has it open`,
      );
      continue;
    }

    try {
      rmSync(path, { recursive: true });
    } catch (cause) {
      // Reported and survived rather than thrown. A stale fixture is inert —
      // no test asks for its name — so a directory that will not go away
      // would otherwise trade a leak for a suite that cannot start at all.
      console.error(
        `could not remove the ${generation} end-to-end fixture ${path}: ${String(cause)}`,
      );
      continue;
    }
    console.log(`removed the ${generation} end-to-end fixture ${path}`);
  }
}
