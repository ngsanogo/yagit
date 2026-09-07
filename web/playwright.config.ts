import { defineConfig, devices } from '@playwright/test';

import { baseURL } from './e2e/session';

/**
 * The end-to-end tests run against a real daemon.
 *
 * Don't run them by hand: `./do test e2e` brings up the stack — daemon and
 * Vite — if it is not already running, and passes its address through
 * YAGIT_BASE_URL. The fallback is the address `./do dev` serves.
 */
export default defineConfig({
  testDir: './e2e',
  // The suite is the spec files. Playwright's default would also collect
  // fixtures.test.ts, which is a Vitest test of the sweep the global setup
  // runs — loaded here it fails on the first import.
  testMatch: '**/*.spec.ts',
  // release.spec.ts belongs to `./do test release`, which runs the built
  // binary with its frontend embedded. Running it here would prove only that
  // Vite still serves the page, which every other spec in this suite already
  // proves.
  testIgnore: 'release.spec.ts',
  fullyParallel: true,
  // Deletes the scratch repositories an older fixture shape left in the
  // daemon's root. Before the first worker, because the suite keeps the
  // current generation on purpose and a run holds it open.
  globalSetup: './e2e/global-setup.ts',
  // A `test.only` left in a spec makes Playwright run that test and exit 0.
  // The job goes green, the reporter shows a successful run, and the thirty
  // other spec files — conflicts, interactive rebase, reset, undo, LFS,
  // submodules — never ran. Nothing says so afterwards, and the state lasts
  // until somebody notices that CI got very fast.
  //
  // Same condition as the reporter below, so "we are in CI" is one notion in
  // this file rather than two. Locally a `.only` is the point of typing it.
  forbidOnly: process.env.CI !== undefined,
  // And retries stay at 0 deliberately. `./do test soak` exists to hunt the
  // flakiness that retries would hide, and a retry here would undo that
  // decision quietly.
  reporter: process.env.CI === undefined ? 'list' : 'github',
  use: {
    baseURL: baseURL(),
    ...devices['Desktop Chrome'],
  },
});
