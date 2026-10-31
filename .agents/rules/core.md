# Core conventions

## Entry point

`./do` is the only supported way to develop, build, test, lint, and run yagit.
CI runs `./do lint` and `./do test`. 

On Windows use `do.cmd` with the same subcommands.

## Toolchain

- Tool versions: `mise.toml` + `mise.lock` (checksum per platform)
- Frontend dependencies: `web/package.json` + `web/pnpm-lock.yaml`, installed by pnpm
- Go packages: `./cmd/...` and `./internal/...` only (not `./...`)

## Error handling

- Never swallow errors: no empty `catch`, no `_ = err`
- Silencing an error requires a comment naming why
- User-facing git failures include command, exit code, and raw stderr
- `golangci-lint` runs with `errcheck` including `check-blank`

## Comments

Comments explain **why** — the trap avoided, the alternative rejected, what
breaks without the line. A comment that restates the code is noise.

## One value, one place

- Ports: `mise.toml` (the `dev:*` tasks) and `web/playwright.config.ts`
- Tool versions: `mise.toml`
- Repository root allowlist: `.env` (`YAGIT_ROOT`)
- Visual values: `web/src/design/tokens.css`

## Agent configuration

`AGENTS.md` at the repository root is the entry point. Rules are this
directory. Skills are `.agents/skills/`. Edit those files. Nothing links them
and nothing generates them.
