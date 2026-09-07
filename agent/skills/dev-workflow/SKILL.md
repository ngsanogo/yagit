---
name: dev-workflow
description: Run yagit development, build, test, and lint commands through ./do. Use when starting dev servers, running tests, linting, building, or when tempted to run go/npm commands directly.
---

# Development workflow

## Rule

Every project command goes through `./do`. CI verifies only `./do` paths.

## Common tasks

**Start development:**

```sh
./do up
```

Starts the stack in the background, prints the URL and token, and returns the
terminal. `./do down` stops it; `./do logs` follows output; `./do dev` runs
in the foreground when you want every line in the terminal.

The token is the checkout's, not the run's: stopping and starting the stack
leaves an open browser tab logged in. `./do up --new-token` replaces it, and
logs every browser out.

The toolchain (mise, Go caches, npm cache, Playwright browsers) lives under
`.yagit/` in the checkout — removing the clone removes all of it. `./do up`
bootstraps on first run; `./do bootstrap --browsers` adds Chromium for e2e.

Daemon hot-reloads Go; Vite serves the frontend proxied through the daemon.

**Run all tests:**

```sh
./do test
```

Runs go, web, and e2e. Target a subset:

```sh
./do test go
./do test web
./do test e2e
./do test fuzz
./do test coverage
```

**Lint and format:**

```sh
./do lint
./do fmt
```

**Build release binaries:**

```sh
./do build
```

**Query API:**

```sh
TOKEN=$(./do token)
curl -H "X-Yagit-Token: $TOKEN" http://127.0.0.1:7420/api/health
```

## After changing agent configuration

```sh
./do agent sync
./do agent check
```

## Windows

Use `do.cmd` with the same subcommands instead of `./do`.
