import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';

// The one axe run in the suite. This file used to hold a second copy of it
// with the four tags spelled out again, so a rule or an option added to the
// helper silently did not apply to the workbench — the screen with the most
// components on it.
import { violations } from './accessibility';
import {
  BRANCH_COMMITS,
  CLONE_COMMITS,
  CLONE_ONLY_SUBJECT,
  FIXTURE_COMMITS,
  FIXTURE_COMMITS_ALL_REFS,
  TRUNK_COMMITS,
  UNMERGED_COMMITS,
  fixtureDir,
  fixtureName,
} from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * The product screen, against the daemon's own repository.
 *
 * yagit is a git client, and the repository it is developed in is a real one
 * with a real history — merges, tags, remotes, French subjects, an upstream
 * that no longer exists. Testing against that rather than a fixture is the
 * point: a fixture only ever contains what its author thought to put in it.
 */

/**
 * Builds a repository, opens it, and waits for its history to land.
 *
 * An earlier version pointed these tests at yagit's own checkout, which
 * passed here and failed in CI: `actions/checkout` leaves a detached HEAD with
 * no local branch, so the "Branches" group never rendered and the history was
 * a different shape entirely. A test that depends on the shape of whatever
 * repository happens to be lying around is a test that only knows about the
 * machine it was written on.
 *
 * So the repository is built. Known commit count, known branch, known tag.
 */
async function openFixtureRepository(page: Page): Promise<string> {
  await openWorkbench(page);

  // Hidden under the checkout's runtime directory. The name and the shape it
  // encodes both come from fixtures.ts, which is also what removes the
  // generations nobody opens any more.
  const name = fixtureName('worker');
  const path = join(fixtureDir(), name);
  buildRepository(path);

  await openByPath(page, path);
  await page.reload();

  // Selecting the tab is not optional. The workbench opens on the first
  // repository the daemon lists, which on a real machine is whichever one the
  // person was already working in — not the one this test just made.
  await page.getByRole('tab', { name: new RegExp(name) }).click();

  // Waits for the count, not merely for the panel: the title reads "History"
  // while the query is in flight and "History — 80" once it has landed, and a
  // test that reads it in between gets no number at all.
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();

  return name;
}

/** Asks the daemon to open a repository by path, the way the dialog does. */
async function openByPath(page: Page, path: string): Promise<void> {
  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
}

/**
 * Runs git inside a fixture.
 *
 * `-c user.*` and an empty config: whoever runs this may sign every commit by
 * default, or template new repositories, and the test would then pass or fail
 * depending on whose machine it is — the same reason the Go tests point git at
 * os.DevNull.
 */
function git(cwd: string, ...args: string[]): void {
  execFileSync(
    'git',
    ['-c', 'user.name=Ada Lovelace', '-c', 'user.email=ada@example.com', ...args],
    {
      cwd,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    },
  );
}

/** Creates the repository on disk, from nothing. */
function buildRepository(path: string): void {
  // Built once per worker, not once per test, and this is not only about
  // speed. The daemon remembers a repository by the identity of its git
  // directory, so deleting and recreating one it already has open is exactly
  // the "replaced underneath" case it refuses with a 409 — correctly. Every
  // test after the first would then be asserting against that refusal.
  if (existsSync(join(path, '.git'))) {
    // Reused, so it is whatever the last run left — and now that yagit can
    // check a branch out, the last run may have left it on another one. Every
    // count these tests assert is a count of what is reachable from HEAD, so a
    // fixture that came back on `feature/a-second-lane` fails them all with a
    // number that is right about the wrong branch. Put back rather than
    // rebuilt: the directory's identity is what the daemon holds it by.
    git(path, 'switch', '--quiet', '--no-guess', '--', 'main');
    return;
  }

  mkdirSync(path, { recursive: true });

  git(path, 'init', '-b', 'main');
  for (let index = 1; index <= TRUNK_COMMITS; index += 1) {
    git(path, 'commit', '--allow-empty', '-m', `feat: commit number ${index}`);
  }

  // A branch leaving the trunk near the top and coming back. Near the top so
  // that the first window on screen holds all of it: a graph the tests can
  // only reach by scrolling is a graph they cannot assert on.
  git(path, 'checkout', '-b', 'feature/a-second-lane', 'HEAD~2');
  for (let index = 1; index <= BRANCH_COMMITS; index += 1) {
    git(path, 'commit', '--allow-empty', '-m', `feat: on the branch, ${index}`);
  }
  git(path, 'checkout', 'main');
  git(path, 'merge', '--no-ff', '-m', 'merge: the second lane', 'feature/a-second-lane');

  git(path, 'tag', 'v1.0.0');

  // Left unmerged on purpose, and left behind: the checkout returns to main so
  // the repository opens on the branch every other test measures. These are
  // the commits the two scopes of the history disagree about — reachable from
  // a ref, and from nothing that is checked out.
  git(path, 'checkout', '-b', 'feature/never-merged');
  for (let index = 1; index <= UNMERGED_COMMITS; index += 1) {
    git(path, 'commit', '--allow-empty', '-m', `feat: never merged, ${index}`);
  }
  git(path, 'checkout', 'main');
}

/**
 * Clones a fixture, and puts one commit on top of the copy.
 *
 * Kept once per worker for the reason buildRepository is: the daemon holds it
 * open, and a repository recreated underneath one it already knows is refused
 * with a 409.
 */
function cloneRepository(source: string, path: string): void {
  if (existsSync(join(path, '.git'))) {
    // Put back on its branch, for the reason buildRepository above is.
    git(path, 'switch', '--quiet', '--no-guess', '--', 'main');
    return;
  }

  mkdirSync(path, { recursive: true });
  git(path, 'clone', source, '.');
  git(path, 'commit', '--allow-empty', '-m', CLONE_ONLY_SUBJECT);
}

test('toggles the workbench theme', async ({ page }) => {
  await openFixtureRepository(page);

  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

  await page.getByRole('button', { name: 'Light' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  expect(await page.evaluate(() => localStorage.getItem('yagit.theme'))).toBe('light');

  await page.getByRole('button', { name: 'Dark' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

test('remembers theme and open tabs across a reload', async ({ page }) => {
  const name = await openFixtureRepository(page);

  await page.getByRole('button', { name: 'Light' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  const path = await page.evaluate(() => {
    const raw = localStorage.getItem('yagit.open-paths');
    if (raw === null) {
      return null;
    }
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) && typeof parsed[0] === 'string' ? parsed[0] : null;
  });
  expect(path).toBeTruthy();

  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await expect(page.getByRole('tab', { name: new RegExp(name) })).toBeVisible();
});

test('lists the repository, its history and its references', async ({ page }) => {
  const name = await openFixtureRepository(page);

  // Its own tab, not any worker's: several run in parallel against one daemon,
  // so a pattern loose enough to match a sibling matches two.
  await expect(page.getByRole('tab', { name: new RegExp(name) })).toBeVisible();

  // The history is not empty and is counted in the panel title, so the number
  // on screen is the number of rows the daemon actually returned.
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();

  const rows = page.getByRole('listitem');
  await expect(rows.first()).toBeVisible();

  // Every row carries what makes a commit identifiable at a glance: a subject,
  // an abbreviated sha, an author and a date.
  await expect(rows.first()).toContainText(/[0-9a-f]{7}/);

  // References, grouped. This repository always has at least a branch.
  await expect(page.getByRole('heading', { name: 'References' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Branches' })).toBeVisible();
});

/**
 * The whole reason for the virtualizer: a repository with a long history must
 * not put a long history in the DOM.
 *
 * Without this, "virtualised" is a word in an ADR. With it, a change that
 * renders every row fails here rather than in a tab that will not open on
 * someone's monitorepo two years from now.
 */
test('renders only the rows on screen', async ({ page }) => {
  await openFixtureRepository(page);

  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();

  const rendered = await page.getByRole('listitem').count();
  expect(rendered).toBeLessThan(FIXTURE_COMMITS);

  // A viewport of about 900px at 56px per row is ~16 rows, plus the
  // virtualizer's overscan on both sides. Anything near the total means the
  // list is rendering everything and the scroll is the browser's, not ours.
  expect(rendered).toBeLessThan(60);
});

/**
 * The point of paging, and the test that fails when its arithmetic is wrong.
 *
 * The daemon serves two hundred rows at a time. The rows at the end of this
 * fixture were never rendered and, past the first page, never even fetched:
 * reaching them means a second request went out, came back, and landed under
 * the right rows. Wrong arithmetic does not fail here — it shows the wrong
 * commits, which is why the assertion names one.
 */
test('scrolling reaches rows that were never fetched', async ({ page }) => {
  await openFixtureRepository(page);

  const oldest = page.getByText('feat: commit number 1', { exact: true });
  await expect(oldest).toHaveCount(0);

  await page.getByRole('list', { name: 'Commits' }).evaluate((list) => {
    list.scrollTop = list.scrollHeight;
  });

  await expect(oldest).toBeVisible();

  // And the graph came with them. Its lines are sent with the page they
  // cross, so a page fetched without them would leave the rows bare.
  const graph = page.getByRole('list', { name: 'Commits' }).locator('> div > svg');
  expect(await graph.locator('circle').count()).toBeGreaterThan(0);
});

/**
 * The other half of paging: a page that does not arrive.
 *
 * The rows it would have held are already on screen as skeletons, and nothing
 * else on this screen can report the failure — the panel's error state belongs
 * to the first page, which succeeded. So the rows report it themselves, with
 * what git said, and offer the one way back.
 *
 * The daemon is let through by the click rather than by the route being lifted
 * before it, and that ordering is what makes this test deterministic. React
 * mounts the list twice in development, and @tanstack/react-virtual replays
 * the scroll offset it read while the first of the two was alive — zero —
 * once, 150 milliseconds after mounting. A scroll that lands inside that
 * window is undone by it for a frame or two: the list renders the top of the
 * history again and the failed page, its rows and its Retry go with it. With
 * the failure lifted first, the page then arrives on its own during that gap
 * and the button never comes back, which is the click waiting out its timeout
 * on a control that no longer exists. Keeping every pre-click request failing
 * puts the button back instead, and makes the rows filling in afterwards
 * evidence that the click is what asked for them.
 */
test('a page that fails says what git said, and can be asked for again', async ({ page }) => {
  await openFixtureRepository(page);

  // Every page but the first. Page zero is what the panel is built from, and
  // failing it would only exercise the error state the panel already has.
  const laterPage = (url: URL) =>
    url.pathname.endsWith('/commits') && url.searchParams.get('page') !== '0';

  // Retry marks the document as it is clicked. The listener is in the capture
  // phase, so it runs before React's own and the mark is already there when
  // the refetch that click starts reaches the route below.
  await page.evaluate(() => {
    document.addEventListener(
      'click',
      (event) => {
        if ((event.target as Element | null)?.closest('button')?.textContent === 'Retry') {
          document.documentElement.dataset['retryClicked'] = 'true';
        }
      },
      true,
    );
  });

  await page.route(laterPage, async (route) => {
    const retried = await page.evaluate(
      () => document.documentElement.dataset['retryClicked'] === 'true',
    );
    if (retried) {
      await route.continue();
      return;
    }

    await route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({
        error: {
          message: 'could not read the history',
          git: {
            command: 'git log --max-count=200 --skip=200',
            args: ['log', '--max-count=200', '--skip=200'],
            exit_code: 128,
            stderr: 'fatal: bad object HEAD',
          },
        },
      }),
    });
  });

  await page.getByRole('list', { name: 'Commits' }).evaluate((list) => {
    list.scrollTop = list.scrollHeight;
  });

  // The message, and under it git's own words, the exit code and the command —
  // on the rows themselves, where the skeletons were.
  await expect(page.getByText('could not read the history').first()).toBeVisible();
  await expect(
    page.getByText(/fatal: bad object HEAD.*exit 128.*git log --max-count=200 --skip=200/).first(),
  ).toBeVisible();

  // One way out for the page, not one per row. Every visible row is holding
  // the same failure, and a control repeated down the list is a tab order
  // nobody can get out of.
  const retry = page.getByRole('button', { name: 'Retry' });
  await expect(retry).toHaveCount(1);
  await expect(retry).toBeVisible();

  // It asks for that page again, and the rows it was holding up arrive — the
  // oldest commit of the fixture is on it. Nothing else could have brought
  // them: the route above answers with the failure until this click, and lets
  // the daemon answer only afterwards.
  await retry.click();

  await expect(page.getByText('feat: commit number 1', { exact: true })).toBeVisible();
});

/**
 * The graph is the main object on this screen, and it is drawn rather than
 * written: nothing else in the suite would notice if it stopped appearing.
 */
test('draws the graph beside the rows', async ({ page }) => {
  await openFixtureRepository(page);

  // The graph sits beside the rows, inside the sized box the scrollbar
  // measures. Reaching it by that path rather than by `svg` alone is what
  // keeps the icons inside the ref badges out of the match.
  const graph = page.getByRole('list', { name: 'Commits' }).locator('> div > svg');
  await expect(graph).toBeVisible();

  // A picture of relationships between rows that are already in the
  // accessibility tree with their author, date and subject. Announcing a
  // column number would add noise, not information (docs/adr/0003).
  await expect(graph).toHaveAttribute('aria-hidden', 'true');

  // A branch and a merge means at least two columns, and a column is one
  // path: the lines of a column are batched into a single element rather
  // than drawn one per edge.
  expect(await graph.locator('path').count()).toBeGreaterThanOrEqual(2);

  // Every dot on screen, one per rendered row.
  expect(await graph.locator('circle').count()).toBeGreaterThan(0);
});

/**
 * The colours come from tokens.css and nowhere else. A path whose stroke had
 * been resolved in JavaScript would still look right — until the theme
 * changed underneath it.
 */
test('colours the graph from the design tokens', async ({ page }) => {
  await openFixtureRepository(page);

  const strokes = await page
    .getByRole('list', { name: 'Commits' })
    .locator('> div > svg path')
    .evaluateAll((paths) => paths.map((path) => path.getAttribute('stroke')));

  expect(strokes.length).toBeGreaterThan(0);
  for (const stroke of strokes) {
    expect(stroke).toMatch(/^var\(--color-lane-([1-9]|10)\)$/);
  }

  // Two columns, two colours: a graph that painted every branch the same
  // would pass every assertion above.
  expect(new Set(strokes).size).toBeGreaterThanOrEqual(2);
});

/**
 * The graph defaults to the current branch, and every ref is a choice.
 *
 * Drawing every ref at once is why the graph was absent on every repository
 * with enough tags: 280 columns on a large history, and the honest refusal
 * above the rows instead of a picture. The choice is now on screen, and the
 * default is the branch you are on.
 */
test('draws the current branch by default, and every ref on request', async ({ page }) => {
  await openFixtureRepository(page);

  // The commit on the unmerged branch is reachable from a ref and from
  // nothing main can see, so the two scopes differ by exactly it.
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();
  await expect(page.getByRole('radio', { name: 'Current branch' })).toBeChecked();

  await page.getByRole('radio', { name: 'All references' }).click();

  await expect(
    page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS_ALL_REFS}` }),
  ).toBeVisible();
  await expect(page.getByRole('radio', { name: 'All references' })).toBeChecked();

  // And back, out of the cache on both sides: the daemon holds an assignment
  // per scope, and the query cache is keyed by it too.
  await page.getByRole('radio', { name: 'Current branch' }).click();
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();
});

test('selecting a commit marks it, and only it', async ({ page }) => {
  await openFixtureRepository(page);

  // Scoped to the history: the sidebar marks the branch HEAD is on with the
  // same attribute, and it means the same thing there — the current item of
  // its own list.
  const commits = page.getByRole('list', { name: 'Commits' });
  const rows = commits.getByRole('listitem').locator('button');
  await rows.first().click();

  await expect(rows.first()).toHaveAttribute('aria-current', 'true');
  await expect(commits.locator('[aria-current="true"]')).toHaveCount(1);
});

/**
 * Which branch you are on is the first thing a git client has to answer, and
 * the sidebar is the list of branches. Before this it was legible only as a
 * badge on whichever history row happened to be on screen.
 */
test('the sidebar marks the branch HEAD is on', async ({ page }) => {
  await openFixtureRepository(page);

  // Nothing is selected yet, so the one current item on the page is the
  // reference list's.
  const marked = page.locator('button[aria-current="true"]');
  await expect(marked).toHaveCount(1);
  await expect(marked).toContainText('main');
  await expect(marked).toContainText('HEAD');
});

/**
 * The point of the sidebar being buttons rather than divs: one action, and it
 * is the only one. Checkout is phase 6.
 */
test('clicking a reference takes the history to its commit', async ({ page }) => {
  await openFixtureRepository(page);

  // Scoped to the history throughout: the details panel shows the subject of
  // the selected commit too, and an unscoped locator would match both the row
  // and the panel — which is not the question this test is asking.
  const commits = page.getByRole('list', { name: 'Commits' });

  // From the far end of the history, so the jump cannot be the list happening
  // to be there already.
  await commits.evaluate((list) => {
    list.scrollTop = list.scrollHeight;
  });
  const oldest = commits.getByText('feat: commit number 1', { exact: true });
  await expect(oldest).toBeVisible();

  // The tag sits on the merge, which is the newest commit in this fixture.
  await page.getByRole('button', { name: /^v1\.0\.0/ }).click();

  await expect(commits.getByText('merge: the second lane', { exact: true })).toBeVisible();
  await expect(oldest).toHaveCount(0);
});

/**
 * A selection that means something: the whole SHA, which the row has no room
 * for, and the parents that make a merge a merge.
 */
test('the selected commit shows its sha and its parents', async ({ page }) => {
  await openFixtureRepository(page);

  await page.getByRole('button', { name: /^v1\.0\.0/ }).click();

  // The row shows seven characters; this is the string you take to a terminal.
  await expect(page.getByText(/^[0-9a-f]{40}$/)).toBeVisible();
  await expect(page.getByRole('button', { name: 'Copy SHA' })).toBeVisible();

  // Two of them, because the tag is on the merge.
  await expect(page.getByRole('heading', { name: 'Parents' })).toBeVisible();
});

/**
 * A selection belongs to the repository it was taken from.
 *
 * The tabs are how more than one repository is on screen, and the selected sha
 * used to survive a switch between them. Against a clone — two checkouts of one
 * project, which is the ordinary case rather than a contrived one — the same
 * sha is in both histories, so a row nobody had clicked lit up in the second.
 */
test('a selected commit does not cross to another repository', async ({ page }) => {
  const name = await openFixtureRepository(page);

  const fixtures = fixtureDir();
  const cloneName = fixtureName('clone');
  const clonePath = join(fixtures, cloneName);
  cloneRepository(join(fixtures, name), clonePath);

  await openByPath(page, clonePath);
  await page.reload();

  // Both tabs are named rather than reached by position: several workers share
  // one daemon, so which repository the workbench lands on after a reload is
  // whichever one it happens to list first.
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();

  // Scoped to the history, like the selection test above: the sidebar marks
  // the branch HEAD is on with the same attribute, and that mark is about the
  // repository rather than about anything clicked.
  const commits = page.getByRole('list', { name: 'Commits' });
  const rows = commits.getByRole('listitem').locator('button');
  await rows.first().click();
  await expect(commits.locator('[aria-current="true"]')).toHaveCount(1);

  await page.getByRole('tab', { name: new RegExp(cloneName) }).click();

  // The count and the extra commit are what say the clone's own history is on
  // screen. Its rows are otherwise the same rows, so a selection asserted
  // before they arrived would be asserted against the panel that was already
  // there.
  await expect(page.getByRole('heading', { name: `History — ${CLONE_COMMITS}` })).toBeVisible();
  await expect(page.getByText(CLONE_ONLY_SUBJECT, { exact: true })).toBeVisible();

  await expect(commits.locator('[aria-current="true"]')).toHaveCount(0);

  // Going back does not bring it out of storage either. The selection is made
  // and thrown away with the view; remembering one per tab would be a second
  // decision, and taking it here would mean taking it by accident.
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();
  await expect(commits.locator('[aria-current="true"]')).toHaveCount(0);
});

/**
 * The workbench fills the window and never grows past it.
 *
 * Every list on this screen is unbounded — a history of a million commits, a
 * repository with a thousand tags — and each one is supposed to scroll inside
 * its own panel. When one of them does not, it takes the whole page with it:
 * the header scrolls away, the graph scrolls away, and the panel that was
 * meant to have a scrollbar has the document's instead.
 *
 * The window is made short rather than the fixture made large, because the
 * assertion is about which element takes the overflow and not about how much
 * there is. Any list long enough to overflow proves the same thing.
 */
test('the panels scroll, and the page does not', async ({ page }) => {
  await openFixtureRepository(page);

  // Short enough that the fixture's own four references do not fit beside the
  // history. Making the window small is how a fixture with four refs stands in
  // for a repository with a thousand.
  await page.setViewportSize({ width: 1000, height: 200 });

  const references = page.getByRole('region', { name: 'References' });
  await expect(references).toBeVisible();

  const measured = await page.evaluate(() => ({
    page: {
      scroll: document.documentElement.scrollHeight,
      client: document.documentElement.clientHeight,
    },
  }));

  expect(measured.page.scroll).toBeLessThanOrEqual(measured.page.client);

  // And something took that overflow rather than shedding it: a column that
  // simply clipped would satisfy the line above and lose every panel past the
  // fold, with no way to reach them. Asked of the ancestors as well as of the
  // panel's own body, because either answer keeps the promise — the sidebar is
  // one scroller now, with each panel sized to what it holds, and the
  // references cap and scroll inside it only once their own list is long
  // enough to need it.
  const scroller = await references.evaluate((panel) => {
    const inside = Array.from(panel.querySelectorAll('*'));
    const above: Element[] = [];
    for (let node = panel.parentElement; node !== null; node = node.parentElement) {
      above.push(node);
    }
    const found = [...inside, panel, ...above].find(
      (element) => element.scrollHeight > element.clientHeight,
    );
    return found === undefined ? null : { scroll: found.scrollHeight, client: found.clientHeight };
  });
  expect(scroller).not.toBeNull();
});

test('the workbench has no accessibility violations', async ({ page }) => {
  await openFixtureRepository(page);

  expect(await violations(page)).toEqual([]);
});

/**
 * The project's central promise, on the one screen that can trigger it: when
 * git fails, the exact command, its exit code and its raw stderr reach the
 * user. Never "Something went wrong".
 */
test('a refused path is explained, not swallowed', async ({ page }) => {
  await openFixtureRepository(page);

  await page.getByRole('button', { name: 'Add repository' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();

  // A path outside the allowed root. The daemon refuses it with 403 and a
  // message naming the root, and that message has to reach the screen intact.
  await page.getByLabel('Repository path').fill('/etc');
  await page.getByRole('button', { name: 'Open', exact: true }).click();

  await expect(page.getByText(/outside the allowed root/)).toBeVisible();
});

test('a repository can be closed, and the tab goes with it', async ({ page }) => {
  const name = await openFixtureRepository(page);
  const tab = page.getByRole('tab', { name: new RegExp(name) });
  await expect(tab).toBeVisible();

  // The close button is a pointer affordance hidden from assistive technology
  // — the keyboard path is Delete on the tab, which is what the tablist
  // announces. Both live in the same wrapper.
  await tab.locator('..').getByRole('button', { includeHidden: true }).click();

  await expect(tab).toHaveCount(0);
});

test('a second repository can be opened while one is already open', async ({ page }) => {
  await openFixtureRepository(page);

  // The gap this closes: the form used to live only in the empty state, so an
  // application with one repository open had no way to open a second — and the
  // tab bar it would have appeared in was built for many.
  await expect(page.getByRole('button', { name: 'Add repository' })).toBeVisible();
  await page.getByRole('button', { name: 'Add repository' }).click();

  await expect(page.getByRole('dialog', { name: 'Add a repository' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Repositories on disk' })).toBeVisible();
});

test('a repository can be opened from the discover list', async ({ page }) => {
  await openWorkbench(page);

  const fixtures = fixtureDir();

  // Its own repository, not the one openFixtureRepository builds: this test
  // scans for a name, and the daemon may already have the worker fixture open
  // from a sibling — which is a different thing to assert about.
  const name = fixtureName('discover');
  const path = join(fixtures, name);
  buildRepository(path);

  // A row for a repository the daemon already holds is disabled — correctly,
  // since opening it again would do nothing. The fixtures are kept between
  // runs on purpose, so a daemon that outlives one run (a `./do dev` left
  // going, which is the normal case) starts the next one with this row already
  // dead. Closing it first is what makes the test assert on opening rather
  // than on which run it happens to be.
  await closeRepositoryAt(page, path);
  await page.reload();

  const dialog = page.getByRole('dialog', { name: 'Add a repository' });
  await page.getByRole('button', { name: 'Add repository' }).click();
  await expect(dialog).toBeVisible();

  // The scan skips hidden directories, which is right — nobody wants their
  // ~/.cache walked — and it is exactly where these fixtures live. Pointing
  // the scan AT the hidden parent is the case that matters anyway: the rule
  // exempts the directory you asked for.
  //
  // Filled only once the field has stopped moving on its own. Until the first
  // scan answers, the box shows '' and then the root the daemon scanned from —
  // and a controlled input re-rendered in the middle of a fill keeps the value
  // it had, so the box ends up holding both paths concatenated. Waiting for
  // the value it settles at is the difference between a test that passes and
  // one that passes on a fast machine.
  const scanIn = dialog.getByLabel('Scan in');
  await expect(scanIn).not.toHaveValue('');
  await scanIn.fill(fixtures);
  await expect(scanIn).toHaveValue(fixtures);
  await dialog.getByRole('button', { name: 'Scan again' }).click();

  // Anchored, and followed by the space the path starts after. This directory
  // is built by the test rather than by fixtureName, so the sweep in
  // global-setup cannot recognise it and every run leaves one behind:
  // `refused-row-2` matched `refused-row-21` and `refused-row-23` from runs
  // long finished, and strict mode killed the test rather than the assertion.
  const row = dialog.getByRole('button', { name: new RegExp(`^${name} `) });
  await expect(row).toBeVisible();
  await row.click();

  await expect(page.getByRole('tab', { name: new RegExp(name) })).toBeVisible();
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();
});

/**
 * The other half of the promise above: an error has to be readable where the
 * action was taken. One mutation opens a repository, and it is reached from
 * two places — a row in the scan list and the path field below it. A row the
 * daemon refuses used to report itself under that field, which is empty and
 * which nobody had typed in.
 */
test('a refused row explains itself on the row, not under the path field', async ({ page }) => {
  await openWorkbench(page);

  const fixtures = fixtureDir();

  // A real repository, left intact. An earlier version of this test made the
  // daemon refuse for real by deleting the row's .git between the scan and the
  // click, and that is a race it loses on a slow machine: the list refetches —
  // the queries go stale after five seconds and refetch when the window takes
  // focus, which clicking does — and the row the test is about is gone from
  // the answer, because deleting .git is exactly what stops the scan finding
  // it. What this test is about is where a failure is shown, not that git
  // produces one, so the refusal is the daemon's own payload played back and
  // the repository on disk stays a repository.
  //
  // That git really answers this way is asserted next door, in "a refused path
  // is explained, not swallowed", against the daemon with nothing stubbed.
  //
  // Prefixed `refused-row-` rather than `refused-`: worker 1's `refused-1`
  // otherwise also matches a leftover `refused-11` still under the fixtures.
  const name = `refused-row-${process.env['TEST_WORKER_INDEX'] ?? '0'}`;
  const path = join(fixtures, name);
  buildRepository(path);

  const command = 'git rev-parse --path-format=absolute --git-common-dir --is-bare-repository';
  const stderr = `fatal: not a git repository: '${path}'`;

  await page.route('**/api/repos', async (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    const asked = route.request().postDataJSON() as { path?: string };
    if (asked.path !== path) return route.continue();
    await route.fulfill({
      status: 400,
      contentType: 'application/json',
      body: JSON.stringify({
        error: {
          message: `${path} is not a git repository`,
          git: {
            command,
            args: [
              'rev-parse',
              '--path-format=absolute',
              '--git-common-dir',
              '--is-bare-repository',
            ],
            exit_code: 128,
            stderr,
          },
        },
      }),
    });
  });

  // Everything below is anchored to the dialog. While no repository is open
  // the workbench mounts OpenRepository twice — once in the empty state, once
  // here — so "Scan in", "Repository path" and the list of discovered
  // repositories each match two elements, and a bare locator would fail on
  // strict mode rather than on the behaviour under test.
  const dialog = page.getByRole('dialog', { name: 'Add a repository' });

  await page.getByRole('button', { name: 'Add repository' }).click();
  await expect(dialog).toBeVisible();

  // Waited for before it is filled, for the reason spelled out in "a
  // repository can be opened from the discover list": the field settles on the
  // root the daemon scanned from a request after the dialog opens, and a fill
  // that lands in the middle of that re-render appends instead of replacing.
  // The scan then runs against two paths glued together and finds nothing.
  const scanIn = dialog.getByLabel('Scan in');
  await expect(scanIn).not.toHaveValue('');
  await scanIn.fill(fixtures);
  await expect(scanIn).toHaveValue(fixtures);

  await dialog.getByRole('button', { name: 'Scan again' }).click();

  // Anchored for the reason the discover test above spells out: this
  // directory outlives its run, and `refused-row-2` matches `refused-row-21`.
  const row = dialog.getByRole('button', { name: new RegExp(`^${name} `) });
  await expect(row).toBeVisible();

  await row.click();

  // Anchored like the row locator above, and for the same reason: the row's
  // text begins with the name and continues into the path, so a plain
  // substring also matches `refused-row-13` left behind by a run with more
  // workers.
  const item = dialog
    .getByRole('list', { name: 'Discovered repositories' })
    .getByRole('listitem')
    .filter({ hasText: new RegExp(`^${name}/`) });

  const alert = item.getByRole('alert');
  await expect(alert).toBeVisible();
  await expect(alert).toContainText('is not a git repository');

  // Whole, not summarized: the same command, exit code and raw stderr the form
  // would have shown. Asserted on the element that carries the command rather
  // than on any text in the row — the message above it ends with the same
  // command line, so a text match finds both and dies on strict mode.
  await expect(item.locator('code')).toContainText(command);
  await expect(item).toContainText('exit 128');
  await expect(item).toContainText('fatal: not a git repository');

  // The field nobody typed in stays clean.
  await expect(dialog.getByLabel('Repository path')).toHaveAttribute('aria-invalid', 'false');

  // Still a row, and still reachable: the button that failed keeps its focus
  // ring and its place in the tab order rather than being replaced by text.
  await row.focus();
  await expect(row).toBeFocused();

  expect(await violations(page)).toEqual([]);
});

/**
 * A row in the history is a subject and nothing more. Selecting it has to
 * answer the question that made someone click: what did this commit actually
 * change?
 */
test('a selected commit shows its message and its patch', async ({ page }) => {
  await openFixtureRepository(page);

  // The merge is the tip of the fixture, so the first row is the one commit
  // whose diff git will not print without being asked — and the one whose
  // answer has to say which of its two parents it is against.
  await page.getByRole('listitem').first().getByRole('button').click();

  // Scoped to the panel, which is a named region: the subject is also on the
  // row that was clicked, and an unscoped match would pass on that alone.
  const pane = page.getByRole('region', { name: 'Commit' });
  await expect(pane).toBeVisible();
  await expect(pane.getByText('merge: the second lane')).toBeVisible();
  await expect(pane.getByText(/shown against its first parent/)).toBeVisible();

  // And it can be put away again, leaving the history where it was.
  await pane.getByRole('button', { name: 'Close the commit' }).click();
  await expect(pane).toBeHidden();
  await expect(page.getByRole('heading', { name: `History — ${FIXTURE_COMMITS}` })).toBeVisible();
});
