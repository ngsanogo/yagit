import { randomBytes } from 'node:crypto';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { defineConfig, devices } from '@playwright/test';

/**
 * `mise run test:release`: one spec, against the binary `mise run build` made
 * for this machine, with its frontend embedded, on a scratch root and port.
 */
const binary = `../dist/yagit-${process.platform === 'win32' ? 'windows' : process.platform}-${
  process.arch === 'x64' ? 'amd64' : process.arch
}${process.platform === 'win32' ? '.exe' : ''}`;
const address = '127.0.0.1:7430';
process.env['YAGIT_BASE_URL'] ??= `http://${address}`;
process.env['YAGIT_TOKEN'] ??= randomBytes(32).toString('base64url');
const root = mkdtempSync(join(tmpdir(), 'yagit-release-'));

export default defineConfig({
  testDir: './e2e',
  testMatch: 'release.spec.ts',
  // Same reason as the main configuration: a `.only` here would silently
  // reduce the release gate to whichever test carried it. This suite is one
  // spec, so the reduction would be to nothing at all.
  forbidOnly: process.env.CI !== undefined,
  reporter: process.env.CI === undefined ? 'list' : 'github',
  use: {
    baseURL: process.env['YAGIT_BASE_URL'],
    ...devices['Desktop Chrome'],
    // Pinned, because the workbench now follows the machine's colour
    // preference when the reader has not chosen one — and a test runner's
    // preference is whatever the machine it happens to be on reports. Without
    // this the suite would assert against one theme locally and the other in
    // CI, and every axe scan would measure a palette nobody picked.
    colorScheme: 'dark',
  },
  webServer: {
    command: `${binary} -root ${root} -addr ${address}`,
    url: `${process.env['YAGIT_BASE_URL']}/api/health`,
    reuseExistingServer: false,
  },
});
