// Captures a yagit page into a PNG file.
//
// Called by `./do shot`. It exists to see what the interface really renders
// without a human in front of a screen — useful while building the design
// system and the views, and the only eye available on a headless machine.

// chromium comes from @playwright/test, a development dependency of the
// frontend, so it is the version pinned in web/package.json — the same one
// that runs the end-to-end tests.
import { chromium } from '@playwright/test';

import { captureViewport, pageLoadTimeout } from './capture.mjs';

const [url, outputPath] = process.argv.slice(2);

if (!url || !outputPath) {
  console.error('usage: node web/scripts/shot.mjs <url> <file.png>');
  process.exit(2);
}

// Every daemon route requires the token. Without it the capture would be a 401
// page, which on disk is indistinguishable from a successful screenshot. The
// deprecated ?token= query is what a caller like this one is left with: a
// browser gets a door page to post the token through, and a headless capture
// has nobody to fill the form in.
const target = new URL(url);
if (process.env.YAGIT_TOKEN && !target.searchParams.has('token')) {
  target.searchParams.set('token', process.env.YAGIT_TOKEN);
}

const browser = await chromium.launch();
const page = await browser.newPage(captureViewport);

try {
  await page.goto(target.href, { waitUntil: 'networkidle', timeout: pageLoadTimeout });
} catch (error) {
  await browser.close();
  console.error(`cannot load ${target.origin}${target.pathname}`);
  console.error(String(error.message ?? error));
  console.error('\nIs the stack running? `./do shot` needs `./do up` first.');
  process.exit(1);
}

await page.screenshot({ path: outputPath, fullPage: true });
await browser.close();

console.log(`screenshot written to ${outputPath}`);
