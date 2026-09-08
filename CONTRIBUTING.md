# Contributing

## Getting set up

One prerequisite: [mise](https://mise.jdx.dev). Nothing else — `./do` is a
POSIX `sh` shim over a Go program, and on Windows `do.cmd` beside it does the
same job. Full setup, the command table, and the git version bars are in
[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).

```sh
curl https://mise.run | sh
git clone https://github.com/ngsanogo/yagit
cd yagit
./do up
```

`./do` installs the pinned toolchain and the frontend dependencies on its own —
there is no separate setup step to remember. All of it goes into `.yagit/`
inside the checkout: mise's tools, Go's build and module caches, npm's cache,
Playwright's browsers. Nothing lands in your home directory, and `rm -rf` on
the clone takes the lot with it.

Documentation, accessibility reports and well-written issues are contributions.
You do not need to touch the Go or the frontend to land one.

Changing a tool version means editing `mise.toml` and then running `mise lock`,
which refreshes the checksums in `mise.lock` for every platform. The next
`mise install` locks your own platform by itself, so skipping `mise lock` leaves
the other five behind — and CI, on a sixth, goes red on a working tree that is
no longer clean. That red is the point: a lockfile covering only the machine it
was edited on is not a lockfile.

## Every command goes through `./do`

The full list is in [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). `./do help`
prints it from the program itself, and `./do <command> --help` prints one
command's usage without running it.

This is not a style preference. `./do` is what CI runs, so anything you do
outside it is unverified. If you find yourself typing `go test` or `npm run` by
hand, either the command belongs in `./do` or you are about to be surprised.

`./do lint` and `./do test` are the gates. Both must pass before a pull request
is ready.

`./do audit` is not a gate, and the difference is worth understanding. lint and
test are hermetic: the same code gives the same answer, offline, forever. The
audit queries vulnerability databases, so its answer changes without the code
changing — green this morning and red this afternoon is a correct result, not a
flake. Gating on it would make merging depend on the weather. It runs weekly in
CI instead, and you can run it any time.

## What the review will look for

The project's principles are in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
Four rules decide most reviews:

**Comments explain why, never what.** A comment restating the line below it is
noise. A comment naming the trap avoided, the alternative rejected, or what
breaks without the line is the reason this codebase is readable. When you fix
something subtle, the fix and its reason ship together.

**Errors never pass silently.** No empty `catch`, no `_ = err`. When a git
command fails, the exact command, the exit code and the raw stderr reach the
user — never "Something went wrong". Silencing an error requires a comment
saying why.

**One obvious way to do it.** One action, one path in the UI. One value, one
place it is defined. A second button "for discoverability", or a port hard-coded
in a third config file, will be declined.

**Dependencies point one way.** `git` (exec and parsing) ← `repo` (state, the
security boundary) ← `api` (HTTP) ← `web` (frontend). Nothing points back up.

## Tests

Parsing and the graph lane assignment are the two places bugs are silent, so
they carry unit tests from the moment they exist. Parsing also carries fuzz
targets: a hand-written test only covers what its author thought of, and a
record terminator that can appear inside a commit subject is exactly that
kind of miss. `./do test fuzz` runs the search; `./do test go` runs every
target's seed corpus as ordinary tests, so a crash found once is a regression
test forever after. The git layer is also tested against the real `git`
binary: a perfect parser fed a wrong format string is still wrong, and only a
real repository catches that.

End-to-end tests start the daemon and Vite themselves. You never have to have
anything running first. They build their scratch repositories under
`.yagit/e2e/` in the checkout, and keep them between runs: the daemon knows a
repository by its git directory, so deleting one it has open is the case it
refuses with a 409. The commit count is therefore part of every fixture's name,
and the run begins by removing the generations whose name no test builds any
more — named on stdout, one line each, and nothing outside that directory. One
is kept back: a fixture the daemon reports as open, which a long-lived `./do
dev` still can be, is left where it is and said to be, and goes on the first
run after that daemon stops.

`./do test soak` runs that end-to-end suite several times with every CPU busy,
to shake out timing races that only pass on an idle machine. It is not a gate —
a failure needs its report read, not a revert — and the default is three runs.

`./do test release` runs after `./do build`. It starts the binary for this
machine from `dist/`, probes `/api/health` and the embedded frontend, then runs
`web/e2e/release.spec.ts` in a browser. The ordinary end-to-end suite goes
through Vite; only this gate exercises what ships. CI runs it in the `Build`
job, immediately after `./do build`.

`./do test coverage` prints a coverage baseline for Go and the frontend. It is
not a gate — there is no threshold — but it is the number to compare against
when you add tests.

Accessibility is asserted, not reviewed. `web/e2e/accessibility.spec.ts` runs
axe-core's WCAG 2.1 AA rules against the rendered page in both themes and with a
dialog open, and a violation fails the suite like any other bug. The tests
beside it in `design-system.spec.ts` each pin one contract that was once broken;
those keep a fix fixed, and this one looks for the next.

[ACCESSIBILITY.md](ACCESSIBILITY.md) is the statement that matches those
tests, and the place to report a barrier the suite did not catch.

## Commits and pull requests

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org):

```
feat(git): parse for-each-ref upstream tracking
fix(api): the ResponseWriter wrapper hid Hijacker and Flusher
docs: the design system and the single-origin architecture
build: ./do starts the daemon and Vite together
test(e2e): the full path through a real browser
```

The subject says what changed. The body says why, and what would have gone wrong
otherwise.

Keep pull requests to one idea. A refactor and a feature in the same diff cannot
be reviewed, only trusted.

## Releases

There is no CHANGELOG.md, on purpose. A release's notes are generated from the
pull requests merged since the previous tag, and this project already asks every
pull request for a title that says what changed and a body that says why — so a
hand-written changelog would be a second copy of that, and the copy nothing
checks is the one that goes stale. `.github/release.yml` sorts dependency bumps
into their own section so they do not bury the rest.

**Nobody tags anything.** Merging to main is the whole of the release process.

`./do version` reads the Conventional Commits since the last release tag and
answers with the version they add up to: a `feat` moves the minor, a `fix` or a
`perf` moves the patch, a `!` or a `BREAKING CHANGE:` footer moves the major —
except below 1.0, where it moves the minor, because semver already says anything
may change while the major is zero. Everything else — `docs`, `test`, `ci`,
`build`, `chore`, `refactor` — moves nothing, so a branch of housekeeping
produces no release. That is the point: a release nobody can tell apart from the
one before it is noise.

Which means the commit message is the release note *and* the version. A `fix:`
that is really a refactor ships a version that promises something it did not
change, and the rule cannot know the difference. Run `./do version` before
merging if you want to see what your branch will produce.

A change that stops accepting a configuration someone may already have must
announce itself in the program first: refuse, and name the variable and both
ways out. That is the only announcement that arrives while the person is looking
for it. The commit then carries `!` or a `BREAKING CHANGE:` footer so the
version moves, and the pull request gets the `breaking-change` label so
`.github/release.yml` prints it above everything else. See
[ADR 0014](docs/adr/0014-a-breaking-configuration-change-announces-itself.md).

The release workflow then runs the gates again, builds the six binaries,
attaches a Sigstore provenance attestation to each and an SPDX bill of materials
to the release. `SECURITY.md` explains how to check them. Its `workflow_dispatch`
is there for the case where you want the release now rather than at the next
merge; it applies the same rule, so it cannot produce a different version.

## Repository settings

These live in the GitHub ruleset named `main`, not in the checkout, but they
are part of how this project runs. A solo maintainer still benefits from them:
they are guardrails against merging by accident, not bureaucracy. The posture
and the Scorecard alerts it intentionally leaves open are spelled out in
`SECURITY.md`.

On `main` the ruleset requires:

- **A pull request** before merging (no direct pushes, no administrator
  bypass).
- **Status checks** against an up-to-date branch: `Lint`, `Test`, `Build`,
  `Analyze go`, `Analyze javascript-typescript`, `Dependency review`,
  `Go on macOS`, and `Go on Windows` — the job names in
  `.github/workflows/ci.yml`, `codeql.yml` and `supply-chain.yml` are stable
  on purpose so this list does not drift.
- **CODEOWNERS review** on other people's changes, resolved review threads,
  and dismissal of stale reviews on new pushes.
- **No minimum approval count** while there is one maintainer — raise it to 1
  when a second person reviews regularly.

Also worth having enabled on the repository:

- Dependabot security updates (`.github/dependabot.yml` is already present).
- Private vulnerability reporting (see `SECURITY.md`).
- Secret scanning and push protection.
- Two labels nothing creates for you. `.github/release.yml` gives
  `breaking-change` its own section at the top of the release notes, and
  `.github/ISSUE_TEMPLATE/accessibility.yml` applies `accessibility` to every
  report it opens — but an unapplied label and a label that does not exist
  produce the same silence. A maintainer creates both once:

  ```sh
  gh label create breaking-change \
    --description "Stops accepting a configuration that already worked" \
    --color b60205

  gh label create accessibility \
    --description "A barrier for a disabled or assistive-technology user" \
    --color 0e8a16
  ```

- Signed commits are optional; release integrity is handled by Sigstore
  attestations on the binaries instead.


## Language

The repository is in English — code, comments, documentation, commit messages,
user-facing strings — so nobody has to guess which half they are reading.

## Agent configuration

Instructions for AI coding agents live in [`agent/`](agent/). That directory is
the only place to edit them. The `cursor/` and `claude/` directories at the
repository root are generated shims — run `./do agent sync` after changing
`agent/`, and `./do agent check` (part of `./do lint`) verifies they match.

See [`agent/README.md`](agent/README.md) for the layout.
