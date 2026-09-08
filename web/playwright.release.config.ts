import { defineConfig, devices } from '@playwright/test';

import { baseURL } from './e2e/session';

/**
 * What `./do test release` runs: one spec, against the built binary.
 *
 * A configuration of its own rather than a file argument to
 * playwright.config.ts, because what differs is the setup and not the file
 * list. That suite's global setup asks the daemon which repositories are open
 * and then deletes every fixture nobody holds — pointed at a release binary,
 * which runs on a scratch root of its own and has none open, it would answer
 * "none" and sweep away the fixtures a `./do up` in another terminal is still
 * using.
 *
 * Nothing here needs a fixture, or YAGIT_ROOT, or a token file: `./do test
 * release` passes the address and the session token in the environment.
 */
export default defineConfig({
  testDir: './e2e',
  testMatch: 'release.spec.ts',
  // Same reason as the main configuration: a `.only` here would silently
  // reduce the release gate to whichever test carried it. This suite is one
  // spec, so the reduction would be to nothing at all.
  forbidOnly: process.env.CI !== undefined,
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
});
