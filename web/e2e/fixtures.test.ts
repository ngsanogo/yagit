import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  realpathSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import {
  FIXTURE_COMMITS,
  FIXTURE_ROLES,
  classifyFixture,
  fixtureName,
  sweepStaleFixtures,
} from './fixtures';

/**
 * The sweep deletes directories from the checkout's `.yagit/e2e/`. Everything
 * it is allowed to remove, and everything it must leave where it is, is pinned
 * here — against a temporary directory, because the one way to find out that
 * this is wrong during an end-to-end run is to find out too late.
 */

describe('classifyFixture', () => {
  // Over the roles rather than over two names written out here, so a third
  // role added to FIXTURE_ROLES arrives with these assertions already made
  // about it. A role the pattern did not know would read as 'unknown', which
  // the sweep skips, and its directories would accumulate unremarked.
  //
  // The first of the two is the property the whole sweep rests on: if a name a
  // run is about to build ever reads as anything but current, the sweep
  // deletes the repository the next line of the suite opens.
  it.each(FIXTURE_ROLES)('recognises the %s role, current and stale alike', (role) => {
    expect(classifyFixture(fixtureName(role))).toBe('current');
    expect(classifyFixture(`${role}-0-of-${FIXTURE_COMMITS - 1}`)).toBe('stale');
  });

  it('calls an earlier commit count stale', () => {
    expect(classifyFixture(`worker-0-of-${FIXTURE_COMMITS - 1}`)).toBe('stale');
    expect(classifyFixture('discover-3-of-99')).toBe('stale');
  });

  // The generation from before the count was in the name at all. These are the
  // directories that have been accumulating the longest, and a sweep that did
  // not recognise them would leave exactly the leak this fixes.
  it('calls a name from before the count stale', () => {
    expect(classifyFixture('worker-0')).toBe('stale');
    expect(classifyFixture('discover-4')).toBe('stale');
  });

  // A scratch name is not a generation of anything: the test that wants one
  // deletes it and builds it again, so last run's copy is of no use to this
  // one whatever it is called. The sweep removes it for that reason, and
  // without the prefix it would read as 'unknown' and be left where it is.
  it.each([
    'scratch-wd-lists',
    'scratch-cf-banner',
    'scratch-',
    // The generation before the prefix, which nothing could name and so
    // nothing ever removed.
    'wd-lists',
    'wd-stage-lines',
    'cf-banner',
  ])('calls %s a scratch fixture', (name) => {
    expect(classifyFixture(name)).toBe('scratch');
  });

  it.each([
    'worker',
    'worker-',
    'not-scratch-0',
    // Close to the legacy scratch shape and not it: a name with a digit in it
    // is a worker's fixture, and the sweep does not guess.
    'wd-0',
    'cf-1-of-2',
    'worker-a-of-253',
    'Worker-0',
    `worker-0-of-${FIXTURE_COMMITS}-backup`,
    `my-worker-0-of-${FIXTURE_COMMITS}`,
    'notes',
    '.git',
    '..',
  ])('leaves %s unrecognised', (name) => {
    expect(classifyFixture(name)).toBe('unknown');
  });
});

describe('sweepStaleFixtures', () => {
  // A daemon holding nothing, which is what a throwaway stack is. Named so
  // that the tests about the other case are the ones that stand out.
  const NOTHING_OPEN: ReadonlySet<string> = new Set();

  let scratch: string;
  let parent: string;
  let outside: string;

  beforeEach(() => {
    scratch = mkdtempSync(join(tmpdir(), 'yagit-sweep-'));
    parent = join(scratch, '.yagit', 'e2e');
    outside = join(scratch, 'not-a-fixture');

    mkdirSync(join(parent, fixtureName('worker')), { recursive: true });
    mkdirSync(join(parent, 'worker-0-of-99', 'refs'), { recursive: true });
    mkdirSync(join(parent, 'worker-9'), { recursive: true });
    mkdirSync(join(parent, 'a-project'), { recursive: true });
    mkdirSync(outside, { recursive: true });

    writeFileSync(join(parent, 'worker-0-of-99', 'refs', 'head'), 'stale');
    writeFileSync(join(parent, 'worker-8-of-99'), 'a file, not a repository');
    writeFileSync(join(outside, 'work.txt'), 'work nobody asked this suite to touch');
  });

  afterEach(() => {
    rmSync(scratch, { recursive: true, force: true });
    vi.restoreAllMocks();
  });

  it('removes stale directories and nothing else', () => {
    sweepStaleFixtures(parent, NOTHING_OPEN);

    expect(readdirSync(parent).sort()).toEqual(
      ['a-project', fixtureName('worker'), 'worker-8-of-99'].sort(),
    );
  });

  it('removes a stale repository whole', () => {
    sweepStaleFixtures(parent, NOTHING_OPEN);

    expect(existsSync(join(parent, 'worker-0-of-99'))).toBe(false);
  });

  // A scratch repository is rebuilt from nothing by the test that wants it, so
  // keeping one buys nothing. Before the prefix these read as 'unknown', which
  // the sweep leaves alone — one git repository per test, forever.
  it('removes a scratch repository', () => {
    mkdirSync(join(parent, 'scratch-wd-lists'), { recursive: true });

    sweepStaleFixtures(parent, NOTHING_OPEN);

    expect(existsSync(join(parent, 'scratch-wd-lists'))).toBe(false);
  });

  it('keeps a scratch repository the daemon still has open', () => {
    const held = join(realpathSync(parent), 'scratch-wd-lists');
    mkdirSync(held, { recursive: true });

    sweepStaleFixtures(parent, new Set([held]));

    expect(existsSync(held)).toBe(true);
  });

  // Not a nicety: a run that deletes from a home directory without saying so
  // is worse than the leak, because nobody can tell afterwards what it took.
  it('names on stdout what it removed', () => {
    const printed = vi.spyOn(console, 'log').mockImplementation(() => {});

    sweepStaleFixtures(parent, NOTHING_OPEN);

    // The resolved parent, because that is what the sweep prints and what a
    // reader would have to go and look at. On macOS the two differ: the
    // system temporary directory is a symlink into /private.
    const resolved = realpathSync(parent);
    const lines = printed.mock.calls.map(([line]) => String(line));
    expect(lines).toHaveLength(2);
    expect(lines.join('\n')).toContain(join(resolved, 'worker-0-of-99'));
    expect(lines.join('\n')).toContain(join(resolved, 'worker-9'));
  });

  // The case a throwaway stack never produces. `./do test e2e` reuses the
  // stack a `./do dev` is already running, and that daemon outlives runs: a
  // fixture it opened under an older shape is stale and pinned at once.
  // Removing it would leave GET /api/repos still listing it — the registry
  // does not re-check the list — and the workbench opening on a repository
  // whose directory is gone.
  it('keeps a stale fixture the daemon still has open', () => {
    const held = join(realpathSync(parent), 'worker-0-of-99');

    sweepStaleFixtures(parent, new Set([held]));

    expect(existsSync(held)).toBe(true);
    // And only that one: a fixture nobody holds is still swept.
    expect(existsSync(join(parent, 'worker-9'))).toBe(false);
  });

  it('names on stdout a fixture it kept', () => {
    const printed = vi.spyOn(console, 'log').mockImplementation(() => {});
    const held = join(realpathSync(parent), 'worker-0-of-99');

    sweepStaleFixtures(parent, new Set([held]));

    const lines = printed.mock.calls.map(([line]) => String(line)).join('\n');
    expect(lines).toContain(held);
    expect(lines).toContain('still has it open');
  });

  // Junctions and privileges make a symlink a different subject on Windows,
  // and the frontend suite runs on Linux in CI. Skipped rather than
  // approximated.
  const onSymlinks = it.skipIf(process.platform === 'win32');

  onSymlinks('does not follow a symlink out of the parent', () => {
    symlinkSync(outside, join(parent, 'discover-9-of-99'));

    sweepStaleFixtures(parent, NOTHING_OPEN);

    expect(existsSync(join(outside, 'work.txt'))).toBe(true);
    expect(readdirSync(parent)).toContain('discover-9-of-99');
  });

  onSymlinks('sweeps a parent reached through a symlink', () => {
    const link = join(scratch, 'link-to-fixtures');
    symlinkSync(parent, link);

    sweepStaleFixtures(link, NOTHING_OPEN);

    expect(existsSync(join(parent, 'worker-0-of-99'))).toBe(false);
    expect(existsSync(join(parent, fixtureName('worker')))).toBe(true);
  });

  it('leaves the parent itself where it is', () => {
    sweepStaleFixtures(parent, NOTHING_OPEN);

    expect(existsSync(parent)).toBe(true);
  });

  // A fresh clone, and every CI runner. Nothing to sweep is not a failure.
  it('says nothing about a parent that does not exist', () => {
    rmSync(parent, { recursive: true });

    expect(() => sweepStaleFixtures(parent, NOTHING_OPEN)).not.toThrow();
  });

  // Removal failing is reported and survived rather than thrown, and that is a
  // decision: a stale fixture is inert, so a directory that will not go away
  // must not cost the suite its whole run. Nothing pinned it until this test —
  // deleting the try/catch left every other one green.
  //
  // Skipped where the permission would not mean what it says: Windows does not
  // decide an unlink by the parent directory's mode, and root ignores it.
  const onRefusedRemoval = it.skipIf(process.platform === 'win32' || process.getuid?.() === 0);

  onRefusedRemoval('reports what it could not remove and sweeps on', () => {
    const reported = vi.spyOn(console, 'error').mockImplementation(() => {});
    const untouchable = join(parent, fixtureName('worker'), 'HEAD');
    writeFileSync(untouchable, 'the current generation, which no failure may reach');

    // Unlinking reads the parent's write bit, not the entry's, so this is what
    // makes a removal fail — and it has to come after the fixtures are built,
    // because a parent that was read-only all along would have nothing in it
    // to fail on.
    chmodSync(parent, 0o500);
    try {
      expect(() => sweepStaleFixtures(parent, NOTHING_OPEN)).not.toThrow();
    } finally {
      // Restored before the assertions, so a failing one cannot leave behind a
      // directory afterEach is then unable to remove either.
      chmodSync(parent, 0o700);
    }

    expect(readdirSync(parent).sort()).toEqual(
      ['a-project', fixtureName('worker'), 'worker-0-of-99', 'worker-8-of-99', 'worker-9'].sort(),
    );
    expect(existsSync(untouchable)).toBe(true);

    // Both of them, not just the first: continuing is the point.
    const resolved = realpathSync(parent);
    const lines = reported.mock.calls.map(([line]) => String(line)).join('\n');
    expect(lines).toContain(join(resolved, 'worker-0-of-99'));
    expect(lines).toContain(join(resolved, 'worker-9'));
  });
});
