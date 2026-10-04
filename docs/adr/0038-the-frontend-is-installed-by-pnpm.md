# 0038 — The frontend is installed by pnpm

**Status:** reverses npm, which was never a recorded decision — it was what
Node shipped. Amends [0024](0024-the-checkout-owns-its-toolchain.md): the npm
cache under `.yagit/` becomes pnpm's store, cache and state.

## The problem

npm had no advocate here. It was the tool that came in the Node tarball, which
is why nothing was ever written down about it — and why it was the one tool in
the project that broke the project's own rules without anyone having decided
that it should.

- **Its version was chosen by nobody.** Every other tool is a line in
  `mise.toml` with a checksum in `mise.lock`. npm was whichever one that Node
  release bundled: moving Node moved the package manager with it, and no file
  in the repository said so.
- **It ran any dependency's install script.** A package several levels down
  could execute code on the machine at install time, with nothing in the
  repository naming which ones did.
- **It let the code import what `package.json` never declared.** npm lays
  every transitive package flat in `node_modules`, so an import of somebody
  else's dependency resolves, works, and disappears in an unrelated bump.
- **A reproducible install cost a full reinstall.** `npm ci` deletes
  `node_modules` before it starts.

## Decision

`web/` is installed by pnpm, and by nothing else.

- **pnpm comes from `mise.toml`, like every other tool.** Node stopped shipping
  corepack with 25, so there is no second way to obtain it that the project
  would have to forbid. `mise.lock` holds its checksum for every platform.
- **`web/package.json` names it in `packageManager`**, and states the floors in
  `engines`. That field is the one tool version `mise.toml` does not hold
  alone. pnpm compares itself against it, and
  [`web/pnpm-workspace.yaml`](../../web/pnpm-workspace.yaml) sets
  `pmOnFail: error`: a mismatch stops the command. pnpm's default is to
  download the declared version and run that instead — a binary no lockfile of
  ours ever checked.
- **`web/pnpm-lock.yaml` replaces `web/package-lock.json`.** It was produced by
  `pnpm import` from the npm lockfile, so the switch changed the tool and not
  one resolved version.
- **`./do` installs with `pnpm install --frozen-lockfile`**, where it ran
  `npm ci`. Same contract: what the lockfile says, or a failure that names the
  dependency `package.json` disagrees about.
- **`verifyDepsBeforeRun: error`.** `pnpm run` checks `node_modules` against
  the lockfile before a script, and by default repairs a mismatch by
  installing — which rewrites the lockfile to agree with `package.json`. That
  is the quiet rewrite the frozen install exists to prevent, arriving by
  another route. Here it stops and says which file moved. Touching a file
  without changing it does not trip it (measured): it compares content.
- **The store lives in the checkout.** The shim sets `pnpm_config_store_dir`,
  `pnpm_config_cache_dir` and `pnpm_config_state_dir` to directories under
  `.yagit/`, which keeps 0024's promise — removing the clone removes
  everything it downloaded — and puts the store on the same filesystem as
  `web/node_modules`, which the hard links pnpm installs with require.

## What it buys

- **Only declared dependencies resolve.** pnpm links into `node_modules`
  exactly what `package.json` names. The frontend built and passed unchanged,
  so there was no undeclared import to find — and now there cannot be one.
- **A dependency's install script does not run unless it is allowed by name.**
  None of this tree needs one: the install completed with no `allowBuilds`
  entry at all.
- **A release less than a day old is not resolved.** pnpm's default, and the
  same instinct as the seven-day cooldown in `.github/dependabot.yml`.
- **An install that is already there costs nothing.** Packages are hard-linked
  out of the store, and a frozen install over an up-to-date tree returns in
  milliseconds instead of deleting it first.
- **Stopping the stack.** `npm run dev` did not forward SIGTERM to Vite, which
  is why `./do` signals process groups. pnpm does forward it, and reaps the
  script if pnpm itself is killed (measured on 12.9.0). The group signal stays:
  air still needs it.

## What it costs

- **A second place a version is written.** `packageManager` repeats the pnpm
  version in `mise.toml`. It cannot drift silently — `pmOnFail: error` — but
  it is two edits where every other tool is one.
- **A lockfile fewer tools read.** `pnpm-lock.yaml` under pnpm 12 is two YAML
  documents: the package manager's own entry, then the project's. Anything
  that parses only the first sees pnpm and nothing else. The bill of materials
  attached to a release was checked and reads both; a tool added later has to
  be checked the same way.
- **mise has to know the binary.** pnpm is installed from its standalone
  release, and which platforms mise will lock it for depends on the registry
  inside that mise: 2026.8.14 left Intel macOS out, 2026.10.1 included it.
  `mise.lock` carries all seven. A lockfile written by an older mise can lack
  an entry a newer one fills in on install — and CI's clean-tree check then
  reports a dirty `mise.lock`.

## What was decided against

- **Staying on npm.** It works, and each item under "The problem" would have
  stayed true.
- **pnpm through `npm install -g`, or through the mise `npm:` backend.** Both
  work without a standalone binary, and neither gets a checksum in
  `mise.lock`: the package would be fetched by npm and trusted on its name.
- **Leaving pnpm's defaults alone.** `pmOnFail: download` and
  `verifyDepsBeforeRun: install` are both conveniences that act without
  saying so, and this project's rule is that errors do not pass silently.
- **A catalog, or a workspace of more than one package.** There is one
  `package.json`. `pnpm-workspace.yaml` exists because that is where pnpm
  reads its settings from, not because there is a workspace.

## References

- [0024 — The checkout owns its toolchain](0024-the-checkout-owns-its-toolchain.md)
- [`web/pnpm-workspace.yaml`](../../web/pnpm-workspace.yaml)
- [pnpm 11 release notes](https://github.com/pnpm/pnpm/releases/tag/v11.0.0) — settings leave `.npmrc`, `pmOnFail`, build scripts denied by default
- [pnpm 12 release notes](https://github.com/pnpm/pnpm/releases/tag/v12.0.0) — an unrecognised setting fails the command
