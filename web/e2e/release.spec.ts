import { expect, test } from '@playwright/test';

import { openWorkbench } from './session';

/**
 * The development end-to-end suite runs against Vite behind the daemon.
 * `./do test release` starts the built binary instead — embedded frontend,
 * production Content-Security-Policy — and runs only this file.
 *
 * One page load is enough: if the bundle is missing, the fonts are inlined as
 * data: URIs, or the SPA fallback is broken, the workbench never mounts and
 * the console says why.
 */
test('serves the embedded frontend', async ({ page }) => {
  const problems: string[] = [];
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
  page.on('console', (message) => {
    if (message.type() === 'error') {
      problems.push(`console: ${message.text()}`);
    }
  });

  await openWorkbench(page);
  await expect(page.getByRole('heading', { name: 'yagit', level: 1 })).toBeVisible();

  expect(problems).toEqual([]);
});
