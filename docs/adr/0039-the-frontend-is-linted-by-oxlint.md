# 0039 — The frontend is linted by oxlint, on TypeScript 7

**Status:** reverses "TypeScript stays on 6", which the index recorded as an
alternative considered without a full record, and ESLint, which had none.

## The problem

The linter was deciding which compiler the project could use.

TypeScript 7 is the native compiler. `typescript-eslint` declares
`typescript >=4.8.4 <6.1.0` as a peer dependency, in every release including
its canary, so the lint stack could not be installed beside it. The index said
so in one line, `.github/dependabot.yml` carried an ignore rule to stop the
bump being proposed, and the project stayed a major behind on its compiler for
the sake of a tool that reads it.

That stack was also five packages that constrained each other through peer
dependencies — `eslint`, `@eslint/js`, `typescript-eslint`,
`eslint-plugin-react-hooks`, `globals` — and it had already produced one tree
that could not be resolved at all, when `@eslint/js` 10 arrived while `eslint`
was still 9. And none of it read a type: the `recommended` set of
`typescript-eslint` is the one that does not need the type checker, so a
promise nobody awaited passed the lint.

## Decision

`web/` is linted by oxlint, with `oxlint-tsgolint` for the rules that need
types, and compiled by TypeScript 7.

- **[`web/.oxlintrc.json`](../../web/.oxlintrc.json) is the whole
  configuration.** Two plugins beside the core rules: `typescript` and `react`.
- **Every rule ESLint enforced is carried, at the level it had.** The two
  configurations were compared mechanically — `eslint --print-config` for a
  `.tsx`, a `.ts`, an end-to-end test, a script and `public/theme.js` against
  `oxlint --print-config` — rather than read side by side: 85 rules were in
  force on TypeScript and 83 on plain JavaScript. The options that changed an
  answer came across too: `_`-prefixed arguments are ignored in TypeScript
  and nowhere else, `while (true)` is allowed, `no-undef` runs on JavaScript
  only and knows Node's globals under `scripts/` and the browser's under
  `public/`.
- **oxlint's `correctness` tier is on, as errors, with types.** That is where
  the type-aware rules are, and it is the tier oxlint itself enables by
  default. The other tiers stay off; a rule from one of them runs because it
  is named in the file.
- **`oxlint --deny-warnings`.** Three React rules are warnings, as they were.
  ESLint let a warning stand and exit 0; none was standing, and now none can.
- **A suppression that suppresses nothing is reported**, as it was.

### What could not be carried

Four rules have no counterpart in oxlint 1.86.0.

| Rule | What is left of it |
| --- | --- |
| `no-octal` | A legacy octal literal is a syntax error in a module, and `tsc` rejects it. `public/theme.js`, the one classic script, loses the check. |
| `no-dupe-args` | A syntax error in a module. In `public/theme.js`, `no-redeclare` reports the duplicate (probed). Nothing is lost. |
| `react-hooks/config` | Validates the React Compiler's configuration. The compiler is not enabled here, so it had nothing to read. |
| `react-hooks/gating` | The same, for the compiler's gating mode. |

### What the first run found

Ten diagnostics on code that ESLint passed.

- **Seven `react/purity`.** oxlint's port of the rule reports `new Date()`
  during render; the ESLint plugin did not. Every one is a relative time —
  "3 minutes ago" — read against the clock as the row renders, on purpose:
  `formatRelativeTime` takes `now` as a parameter so that it stays pure. Each
  site carries a suppression that points at that function, where the reason
  is written once.
- **Three `typescript/no-misused-spread`**, a rule that needs types and that
  ESLint was not running. One was a real trap: `request()` merged a caller's
  `headers` with a spread, and the type allowed a `Headers` object or a list
  of pairs, either of which spreads into nothing useful without a word. No
  caller passed one. The type now says a plain record. The other two spread a
  string to walk its code points, which is what they mean to do, and say so.

## What it costs

- **The React rules are ports.** They follow the React Compiler's own checks,
  not the ESLint plugin's release of them, and the seven findings above are
  what that difference looks like. A port can also be behind.
- **Two of the carried rules are in oxlint's `nursery` tier** — `no-undef`
  and `no-useless-assignment` — which oxlint describes as still under
  development. Their answers may change between releases.
- **Three packages still move together.** oxlint declares `oxlint-tsgolint`
  as a peer dependency with a floor, and `oxlint-tsgolint` brings its own
  TypeScript 7 to read types with. The Dependabot group keeps them, and
  `typescript`, in one pull request.
- **`typescript` 7 is a compiler and not a library.** The package ships `tsc`
  and no `tsserver`, and what it exports by name is its version: the
  programming interface is under paths marked `unstable`. A tool that imports
  the interface TypeScript 6 had cannot use it. Nothing in this tree does.
- **`public/theme.js` is no longer checked for a legacy octal literal.**

## What was decided against

- **Staying on ESLint and TypeScript 6** until the lint ecosystem follows.
  That is waiting on somebody else's schedule to use the compiler the project
  would otherwise choose.
- **Carrying the old rule list and nothing more.** With every tier off,
  `oxlint-tsgolint` would be installed and run no rule: the type-aware rules
  are in the `correctness` tier.
- **Turning on more tiers, or more plugins.** `pedantic`, `style` and the rest
  mix findings with house style this project has not adopted. oxlint's
  `jsx-a11y` rules exist and are off: [ACCESSIBILITY.md](../../ACCESSIBILITY.md)
  says why the rendered page is what is asserted. Enabling them today reports
  24 findings; that is a decision of its own, not a side effect of this one.
- **Restructuring the seven clock reads** behind a hook that holds the time
  in state. It would satisfy the rule and change when a relative time
  updates, which is a behaviour change that deserves its own pull request.

## References

- [`web/.oxlintrc.json`](../../web/.oxlintrc.json)
- [`web/src/lib/format.ts`](../../web/src/lib/format.ts) — `formatRelativeTime`, where the clock reads are explained
- [0038 — The frontend is installed by pnpm](0038-the-frontend-is-installed-by-pnpm.md)
