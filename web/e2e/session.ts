import { expect, type Page } from '@playwright/test';

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

let cachedRoot: string | undefined;

/** The yagit checkout these tests run from. */
export function projectRoot(): string {
  return resolve(import.meta.dirname, '../..');
}

/**
 * The session token every route requires.
 *
 * From the environment, and from nowhere else. `./do test e2e` passes the
 * token of the stack it is about to drive — the one it started on a throwaway
 * token of its own, or the one `./do up` is running — and `./do test release`
 * does the same for the built binary. Nothing here reads a file under
 * `.yagit/`: the file the daemon used to write outlived every daemon that was
 * killed outright, and the checkout's own token is not this suite's to use — a
 * tab left open on it would join the test run.
 */
export function sessionToken(): string {
  const fromEnv = process.env['YAGIT_TOKEN'];
  if (fromEnv !== undefined && fromEnv !== '') {
    return fromEnv;
  }

  throw new Error(
    'YAGIT_TOKEN is not set. The end-to-end tests need the daemon and its token: ' +
      'run `./do test e2e`, which starts the daemon and passes the token here.',
  );
}

/**
 * The repository root the daemon was started with.
 *
 * `./do test e2e` passes it through the environment. The suite reads it in one
 * place — the check in global-setup that the fixture directory is inside it —
 * because a fixture outside the root is refused by every test at once, with a
 * message about a security boundary rather than about configuration.
 */
export function yagitRoot(): string {
  const fromEnv = process.env['YAGIT_ROOT'];
  if (fromEnv !== undefined && fromEnv !== '') {
    return fromEnv;
  }

  if (cachedRoot !== undefined) return cachedRoot;

  const envPath = resolve(projectRoot(), '.env');
  try {
    const contents = readFileSync(envPath, 'utf8');
    for (const line of contents.split('\n')) {
      const trimmed = line.trim();
      if (trimmed.startsWith('YAGIT_ROOT=')) {
        cachedRoot = trimmed.slice('YAGIT_ROOT='.length).trim();
        return cachedRoot;
      }
    }
  } catch {
    // fall through
  }

  throw new Error(
    `YAGIT_ROOT is not set and could not be read from ${envPath}. ` +
      'Run the end-to-end tests through `./do test e2e`.',
  );
}

/**
 * Where the daemon answers.
 *
 * `./do test e2e` passes the address of the stack it started or reused; the
 * fallback is what `./do dev` serves. Here rather than in
 * playwright.config.ts, which reads it from here, because the global setup
 * talks to the daemon before any test has a baseURL to be given.
 */
export function baseURL(): string {
  return process.env['YAGIT_BASE_URL'] ?? 'http://127.0.0.1:7420';
}

/** Exchanges the session token for an HttpOnly cookie. */
export async function establishSession(page: Page): Promise<void> {
  const response = await page.request.post('/api/session', {
    form: { token: sessionToken() },
  });
  if (!response.ok()) {
    throw new Error(`session exchange failed: ${response.status()} ${await response.text()}`);
  }
}

/**
 * Opens the design system showcase. It is not linked from the workbench;
 * `/design` is the way in (ADR 0006).
 */
export async function openDesignSystem(page: Page): Promise<void> {
  await establishSession(page);
  await clearSessionPreferences(page);
  await page.goto('/design');
  await page.getByRole('heading', { name: 'Type scale' }).waitFor();
}

/** Opens the application at the workbench, where a person lands. */
export async function openWorkbench(page: Page): Promise<void> {
  await establishSession(page);
  await clearSessionPreferences(page);
  await page.goto('/');
  await page.getByRole('heading', { name: 'yagit', level: 1 }).waitFor();
}

/**
 * Drops UI preferences the workbench would restore on load.
 *
 * Without this, a path one test wrote into localStorage is re-opened on the
 * next test's first paint — and a tab matcher that looks for a short name
 * finds two repositories where the suite expected one. The daemon already
 * holds whatever every worker opened; the store must not add more.
 */
async function clearSessionPreferences(page: Page): Promise<void> {
  await page.addInitScript(() => {
    for (const key of ['yagit.open-paths', 'yagit.active-path', 'yagit.history-scope']) {
      localStorage.removeItem(key);
    }
  });
}

/**
 * Closes the repository at a path, if the daemon has it open.
 *
 * Through the API rather than the tab bar: several workers share one daemon,
 * so the tab may not be the active one and closing the wrong tab is worse than
 * not closing any.
 *
 * Here rather than in one spec because two different things need it. A test
 * about OPENING has to start from a repository that is closed. And a test that
 * built its own throwaway repository has to give it back afterwards: the daemon
 * outlives the run whenever `./do test e2e` reuses a `./do dev`, and the sweep
 * refuses to delete a directory the daemon still holds — so a fixture nobody
 * closes is a fixture nothing can ever remove.
 *
 * It does not reload the page. Some callers need the interface to catch up and
 * reload themselves; a cleanup hook running after the assertions does not.
 */
export async function closeRepositoryAt(page: Page, path: string): Promise<void> {
  const headers = { 'X-Yagit-Token': sessionToken() };

  const listed = await page.request.get('/api/repos', { headers });
  expect(listed.ok(), await listed.text()).toBeTruthy();

  const { repos } = (await listed.json()) as { repos: { id: string; path: string }[] };
  const open = repos.find((repository) => repository.path === path);
  if (open === undefined) {
    return;
  }

  const closed = await page.request.delete(`/api/repos/${open.id}`, { headers });
  expect(closed.ok(), await closed.text()).toBeTruthy();
}
