// A REPL that drives a running yagit in a headless Chromium.
//
// Called by `./do drive`, which installs the browser, reads the session token
// and hands both of them down. `./do shot` takes one picture and leaves; this
// holds the page open and reads commands from stdin, so an agent on a machine
// with no screen can click a button, read what came back, and photograph the
// result.
//
// One line in, one reply, one `ready` marker — always, including for a line it
// skipped. That invariant is the whole protocol: a caller under tmux polls for
// the marker instead of sleeping, and a marker that sometimes fails to appear
// would have it read the previous command's answer as this one's.

// chromium comes from @playwright/test, a development dependency of the
// frontend, so it is the version pinned in web/package.json — the same one
// that runs the end-to-end tests.
import { chromium } from '@playwright/test';
import { createInterface } from 'node:readline';
import { mkdirSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';

import { captureViewport, pageLoadTimeout } from './capture.mjs';
import { take } from './driver-line.mjs';

const [baseURL, shotDirectory] = process.argv.slice(2);
const token = process.env.YAGIT_TOKEN;

if (!baseURL || !shotDirectory || !token) {
  console.error('usage: ./do drive [url]');
  console.error('This script is the implementation; ./do drive is the way in — it installs');
  console.error('Chromium, checks the stack answers the session token, and passes both down.');
  process.exit(2);
}

const browser = await chromium.launch();
const context = await browser.newContext({ baseURL, ...captureViewport });
const page = await context.newPage();

// Kept rather than printed as they arrive: a console error that lands in the
// middle of another command's output reads like that command's failure.
const consoleErrors = [];
page.on('console', (message) => {
  if (message.type() === 'error') consoleErrors.push(message.text());
});
page.on('pageerror', (error) => consoleErrors.push(String(error)));

// Every route needs the token. The interface exchanges it for an HttpOnly
// cookie through this route; doing the same here means the driver's page is
// authenticated exactly the way a person's browser is, rather than through the
// deprecated ?token= query `./do shot` is reduced to.
async function establishSession() {
  const response = await context.request.post('/api/session', { form: { token } });
  if (!response.ok()) {
    throw new Error(`session exchange failed: ${response.status()} ${await response.text()}`);
  }
}

// The API through the browser's context, so it carries the same cookie the
// page does and a call cannot succeed for the driver while the interface is
// locked out.
//
// The header goes on every call even though the cookie is already there: a
// request authenticated by cookie alone must also carry an Origin the daemon
// allows, and a request made outside a page has none. POST and DELETE would
// come back 403 "origin rejected". The header is the explicit credential, and
// explicit credentials skip that check.
async function api(method, path, body) {
  const response = await context.request.fetch(path, {
    method,
    headers: { 'X-Yagit-Token': token },
    ...(body === undefined ? {} : { data: JSON.parse(body) }),
  });
  return `${response.status()} ${await response.text()}`;
}

// yagit's forms are labelled, not id'd: the path box is reachable as "the
// field labelled Repository path" and by nothing shorter. Playwright has no
// public label engine, only getByLabel, so the prefix is translated here —
// role= and text= it understands on its own.
function locate(selector) {
  if (selector.startsWith('label=')) return page.getByLabel(selector.slice('label='.length));
  return page.locator(selector);
}

// An argument the line did not supply is a mistake in the caller's script, not
// something to guess at: it fails the way any other failed command does, with
// the usage that would have worked.
function need(value, usage) {
  if (value === '') throw new Error(`usage: ${usage}`);
  return value;
}

async function visit(path) {
  try {
    await page.goto(path, { timeout: pageLoadTimeout });
  } catch (cause) {
    throw new Error(`cannot load ${baseURL}${path}. Is the stack running? ./do dev`, { cause });
  }
}

// The number continues the sequence already on disk rather than restarting at
// one. A second run of the driver used to overwrite the first run's pictures,
// which loses precisely the before half of a before-and-after pair.
function nextShotNumber() {
  const numbers = readdirSync(shotDirectory).map((name) => Number.parseInt(name, 10));
  return Math.max(0, ...numbers.filter(Number.isInteger)) + 1;
}

async function screenshot(name) {
  mkdirSync(shotDirectory, { recursive: true });
  const label = (name === '' ? 'shot' : name).replace(/[^\w.-]/g, '-');
  const file = resolve(shotDirectory, `${String(nextShotNumber()).padStart(2, '0')}-${label}.png`);
  await page.screenshot({ path: file, fullPage: true });
  return file;
}

// Playwright's fill on a React input that a query re-seeds can leave the old
// value in place: the fill lands, the query resolves, the box is written over.
// Filling twice and reading the value back turns that race into a message
// instead of a screenshot of the wrong screen.
async function fill(selector, value) {
  const box = locate(selector).first();
  await box.fill('');
  await box.fill(value);

  const settled = await box.inputValue();
  if (settled === value) return `filled ${selector}`;

  await box.fill(value);
  const second = await box.inputValue();
  if (second !== value) return `filled ${selector}, but it holds ${JSON.stringify(second)}`;
  return `filled ${selector}`;
}

// Each command is handed the rest of its line, and says for itself how much of
// that is a token and how much is a value. See driver-line.mjs for the rule.
const commands = {
  async help() {
    // quit is handled by the loop rather than by a command, and would be
    // missing from a list built only from this table.
    return [...Object.keys(commands), 'quit'].join(' ');
  },

  // The workbench is behind the session exchange, so open does both: no
  // command sequence starts with a 401 page that photographs like a real one.
  //
  // The heading match is exact because the door page's own heading — "yagit
  // needs its session token" — contains the workbench's, and Playwright
  // matches a name by substring unless told not to. Without exact, the guard
  // passes on the very page it exists to catch.
  async open() {
    await establishSession();
    await visit('/');
    await page.getByRole('heading', { name: 'yagit', level: 1, exact: true }).waitFor();
    return `opened ${baseURL}`;
  },

  async nav(rest) {
    const [path] = take(rest, 1);
    await visit(path === '' ? '/' : path);
    return `at ${page.url()}`;
  },

  async click(rest) {
    const [selector] = take(rest, 1);
    await locate(need(selector, 'click <selector>')).first().click();
    return `clicked ${selector}`;
  },

  async fill(rest) {
    const [selector, value] = take(rest, 1);
    return await fill(need(selector, 'fill <selector> <value>'), value);
  },

  async press(rest) {
    const [key, selector] = take(rest, 2);
    need(key, 'press <key> [selector]');
    if (selector === '') await page.keyboard.press(key);
    else await locate(selector).first().press(key);
    return `pressed ${key}`;
  },

  async wait(rest) {
    const [selector] = take(rest, 1);
    await locate(need(selector, 'wait <selector>')).first().waitFor({ state: 'visible' });
    return `visible: ${selector}`;
  },

  async text(rest) {
    const [selector] = take(rest, 1);
    return (await locate(need(selector, 'text <selector>')).first().innerText()).trim();
  },

  async count(rest) {
    const [selector] = take(rest, 1);
    return String(await locate(need(selector, 'count <selector>')).count());
  },

  async shot(rest) {
    const [name] = take(rest, 1);
    return await screenshot(name);
  },

  // The body is the rest of the line, untouched: JSON is quotes all the way
  // down, and every one of them has to reach JSON.parse as written.
  async api(rest) {
    const [method, path, body] = take(rest, 2);
    if (method === '' || path === '') throw new Error('usage: api <METHOD> <path> [json]');
    return await api(method.toUpperCase(), path, body === '' ? undefined : body);
  },

  // Opening a repository is the first thing every session needs and the one
  // step with a security boundary behind it, so it gets a command of its own
  // rather than an /api/repos body the caller has to remember to JSON-encode.
  // The path is a value, not a token: it may contain spaces.
  async repo(rest) {
    const [action, argument] = take(rest, 1);
    if (action === 'list') return await api('GET', '/api/repos');
    if (action === 'open') {
      return await api(
        'POST',
        '/api/repos',
        JSON.stringify({ path: need(argument, 'repo open <path>') }),
      );
    }
    if (action === 'close') {
      return await api(
        'DELETE',
        `/api/repos/${encodeURIComponent(need(argument, 'repo close <id>'))}`,
      );
    }
    throw new Error('usage: repo open <path> | repo close <id> | repo list');
  },

  async errors() {
    return consoleErrors.length === 0 ? 'no console errors' : consoleErrors.join('\n');
  },

  async eval(rest) {
    return JSON.stringify(await page.evaluate(need(rest, 'eval <javascript>')));
  },
};

const input = createInterface({ input: process.stdin });
console.log(`driver ready on ${baseURL}`);

for await (const line of input) {
  const [name, rest] = take(line, 1);

  if (name === 'quit' || name === 'exit') {
    console.log('ready');
    break;
  }

  // A blank line and a comment still answer, because the protocol is one line
  // in, one marker out: a caller polling for the marker after a comment would
  // otherwise match the marker of the command before it.
  if (name === '' || name.startsWith('#')) {
    console.log('ready');
    continue;
  }

  const command = commands[name];
  if (command === undefined) {
    // A misspelled command did not do what the script asked, and a script that
    // ends 0 having done nothing is worse than one that fails.
    process.exitCode = 1;
    console.log(`unknown command: ${name}. Try help`);
    console.log('ready');
    continue;
  }

  try {
    console.log(await command(rest));
  } catch (error) {
    // A failed command must not end the session: the next line is usually the
    // one that diagnoses it — a screenshot, or the console errors. The exit
    // status still records that something failed, for a caller that branches
    // on it rather than reading the transcript.
    process.exitCode = 1;
    console.log(`error: ${error.message ?? error}`);
  }
  console.log('ready');
}

await browser.close();
