import { execFileSync } from 'node:child_process';
import { homedir } from 'node:os';
import { defineConfig, devices } from '@playwright/test';

import { baseURL } from './e2e/session';

/**
 * The end-to-end tests run against a real daemon and Vite: the stack
 * `mise run dev` / `./do up` serves, reused when it is already up, otherwise
 * started for the run (`mise run test:e2e`).
 */
process.env['YAGIT_TOKEN'] ??= execFileSync('node', ['../scripts/session-token.mjs'], {
  encoding: 'utf8',
}).trim();
process.env['YAGIT_ROOT'] ??= homedir();

export default defineConfig({
  testDir: './e2e',
  // The suite is the spec files. Playwright's default would also collect
  // fixtures.test.ts, which is a Vitest test of the sweep the global setup
  // runs — loaded here it fails on the first import.
  testMatch: '**/*.spec.ts',
  // release.spec.ts belongs to `mise run test:release`, which runs the built
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
  // And retries stay at 0 deliberately: a retry would hide flakiness.
  reporter: process.env.CI === undefined ? 'list' : 'github',
  use: {
    baseURL: baseURL(),
    ...devices['Desktop Chrome'],
    // Pinned, because the workbench now follows the machine's colour
    // preference when the reader has not chosen one — and a test runner's
    // preference is whatever the machine it happens to be on reports. Without
    // this the suite would assert against one theme locally and the other in
    // CI, and every axe scan would measure a palette nobody picked.
    colorScheme: 'dark',
  },
  webServer: [
    {
      command: 'go run ./cmd/yagit',
      cwd: '..',
      url: `${baseURL()}/api/health`,
      reuseExistingServer: true,
      timeout: 180_000,
      env: {
        YAGIT_ADDR: '127.0.0.1:7420',
        YAGIT_FRONTEND_DEV_SERVER: 'http://127.0.0.1:7421',
      },
    },
    {
      command: 'pnpm run dev',
      url: 'http://127.0.0.1:7421',
      reuseExistingServer: true,
      env: { YAGIT_VITE_PORT: '7421' },
    },
  ],
});
