---
description: Test, fuzz, and end-to-end conventions
paths:
  - "**/*_test.go"
  - "web/**/*.test.ts"
  - "web/**/*.test.tsx"
  - "web/e2e/**"
---

# Testing conventions

## Gates

`./do lint` and `./do test` must pass before merge.

## Go

```sh
./do test go      # unit + integration + fuzz seeds
./do test fuzz    # fuzz search (longer)
./do test bench   # benchmarks, no -race, not a gate
./do test coverage  # baseline report, not a threshold gate
```

Priority packages: `internal/git` (parsing), `internal/graph` (lane assignment).
Fuzz anything that parses git output — hand-written tests miss edge cases.

## Frontend

```sh
./do test web     # Vitest unit tests
./do test e2e     # Playwright — starts daemon and Vite automatically
./do test release # built binary from dist/ — needs ./do build first; CI Build job
./do test soak [runs]  # the e2e suite N times, every CPU busy; not a gate
```

E2E tests never assume `./do dev` is already running — they start their own
stack or reuse an existing one. The release test is different: it starts the
built binary with the embedded frontend and runs `web/e2e/release.spec.ts`
under `web/playwright.release.config.ts`, which has no global setup — the
development one sweeps fixtures, and a release daemon holds none to protect.
It runs on a token minted for that run and passed through the environment,
never the checkout's, so a `./do up` in another terminal keeps its session.

Soaking hunts timing races that only pass on an idle machine. Its load is
goroutines inside `./do`, never background shell loops: a killed shell leaves
those spinning forever, and no trap runs on SIGKILL. A soak failure is a report
to read, not a revert — starving the suite of CPU can exceed its own timeouts.

## Accessibility

`web/e2e/accessibility.spec.ts` runs axe-core WCAG 2.1 AA in both themes.
Violations fail the suite like any other bug.

## When to add tests

Add tests when they cover real behavior — parsing, graph invariants, API
contracts, regressions that broke before. Do not add tests that only assert
the obvious.

## Platform gates

Go tests run on Linux, Windows, and macOS in CI via `./do test go`. The entry
point must work on all three (`./do` / `do.cmd`).

## Filling a box a query feeds

`fill` selects what is in a box and inserts over the selection. Several of
yagit's inputs take their value from a TanStack query that answers later — the
commit box from the message an amend would replace, "Scan in" from the root the
daemon last scanned. A value written into the element between the select and
the insert **collapses the selection**, so the insert appends instead of
replacing.

Wait for the box to hold its settled value first:

```ts
await expect(box).toHaveValue(before);
await box.fill(after);
await expect(box).toHaveValue(after);
```

Every intermittent end-to-end failure investigated so far was this, and none of
them looked like it: an amend committed one subject reading
`first: the committed statefirst: reworded from the interface`, and a scan ran
against two paths glued together. Both read as the feature being broken.
