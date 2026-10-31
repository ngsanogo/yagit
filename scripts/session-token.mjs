// Prints this checkout's session token, minting one into .yagit/session-token
// the first time (or with --new). The development daemon is given this token,
// so a restart does not log the browser out.
import { randomBytes } from 'node:crypto';
import { chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';

const file = join(dirname(import.meta.dirname), '.yagit', 'session-token');
const renew = process.argv.includes('--new');

let token = !renew && existsSync(file) ? readFileSync(file, 'utf8').trim() : '';
if (!/^[A-Za-z0-9_-]{43}$/.test(token)) {
  token = randomBytes(32).toString('base64url');
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, token + '\n', { mode: 0o600 });
  chmodSync(file, 0o600);
}
process.stdout.write(token + '\n');
