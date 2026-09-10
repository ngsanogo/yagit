// defineConfig comes from vitest, not from vite: it is the same function,
// extended with the `test` field. Without it the test configuration below
// would be untyped and tsc would reject it.
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

/**
 * The project's ports are defined in `./do`, which passes Vite's down through
 * the environment. Restating it here would create a second source of truth,
 * and two sources of truth always end up diverging. The fallback is there for
 * whoever runs `vite` by hand.
 */
const port = (name: string, fallback: number): number => {
  const raw = process.env[name];
  if (raw === undefined || raw === '') return fallback;

  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed <= 0 || parsed > 65535) {
    throw new Error(`${name} is ${raw}, which is not a valid port number`);
  }
  return parsed;
};

const vitePort = port('YAGIT_VITE_PORT', 7421);

export default defineConfig({
  plugins: [react(), tailwindcss()],

  server: {
    // Vite listens on loopback only and is never reached directly: the
    // browser talks to the Go daemon, which proxies everything outside /api
    // to here. One origin, so one authentication model, in development as in
    // production.
    host: '127.0.0.1',
    port: vitePort,
    // Fail rather than drift onto another port: the daemon proxies to a fixed
    // address, and a Vite that moved elsewhere would turn every page into a
    // 502 with nothing pointing at the cause.
    strictPort: true,

    // No hmr.clientPort, deliberately. Left unset, the hot reload client
    // opens its WebSocket on the page's own host and port — the daemon's
    // when the browser comes straight to it, and a reverse proxy's when one
    // serves yagit under a name of its own — and the daemon passes the
    // upgrade on to here. Pinning it to the daemon's port sent the socket
    // around the proxy, to a port nothing outside the machine can reach, and
    // hot reload stopped with one console line to say so.
  },

  build: {
    // The built frontend is embedded in the Go binary by embed.FS, which
    // cannot reach above its own package: the output therefore goes straight
    // into internal/assets.
    outDir: '../internal/assets/dist',

    // Every asset stays a file on this origin; none is inlined as a data: URI.
    //
    // The daemon sends `default-src 'self'`, which forbids data: — and only
    // the released binary would ever show it, because in development the CSS
    // comes from Vite unbuilt. Vite's default inlines anything under 4 KB,
    // which caught one font subset: the browser refused it, drew a fallback
    // face, and said so in a console nobody has open. Widening the policy
    // would have bought that back one directive at a time, for every asset
    // that ever slips under the limit. `./do build` fails if this stops
    // holding.
    assetsInlineLimit: 0,

    // `./do build` is what empties this directory, not Vite. Vite would take
    // the checked-in .gitkeep with it, and without that file the assets
    // package's //go:embed does not compile on a fresh clone — the file would
    // then have to be restored after every build, and the working tree is
    // left dirty the day that restore silently fails.
    emptyOutDir: false,
  },

  test: {
    environment: 'node',
    // e2e/ holds one Vitest test among the Playwright specs: the sweep that
    // deletes stale fixtures from the daemon's root has to be tested where a
    // temporary directory can stand in for somebody's home directory, and
    // that is not something to find out about from a browser run.
    // scripts/ holds the tooling `./do` calls: the line parser the driver
    // reads its commands with is pure, and is where a driver silently types
    // the wrong thing into the right box.
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'e2e/**/*.test.ts', 'scripts/**/*.test.mjs'],

    // formatAbsoluteTime renders the day in the ambient timezone, which is
    // what a reader wants and what makes any assertion about it depend on
    // where it runs: two of these tests fail east of UTC+13, and nowhere the
    // suite is normally run. Pinned, so it means the same thing everywhere.
    env: { TZ: 'UTC' },

    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      reporter: ['text-summary'],
    },
  },
});
