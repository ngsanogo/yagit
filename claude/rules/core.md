---
description: Core project principles and workflow
---

# Core conventions

## Entry point

`./do` is the only supported way to develop, build, test, lint, and run yagit.
CI runs `./do lint` and `./do test`. Anything done outside `./do` is unverified.

On Windows use `do.cmd` with the same subcommands.

## Toolchain

- Tool versions: `mise.toml` + `mise.lock` (checksum per platform)
- Frontend dependencies: `web/package-lock.json`
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

- Ports: `cmd/do/main.go`
- Tool versions: `mise.toml`
- Repository root allowlist: `.env` (`YAGIT_ROOT`)
- Visual values: `web/src/design/tokens.css`

## ADRs

Architecture Decision Records in `docs/adr/` explain past choices. They are
**historical context**, not permission slips. Changing a decision means writing
a new ADR that says so.

## Agent configuration

Edit `agent/` only. Run `./do agent sync` after changes. CI runs
`./do agent check`.
