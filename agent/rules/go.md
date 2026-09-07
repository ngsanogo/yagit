# Go conventions

## Package layout

```
cmd/do/       project commands
cmd/yagit/   daemon entry
internal/git/       exec git, parse output — only subprocess boundary
internal/repo/      open repos, YAGIT_ROOT security check
internal/graph/     lane assignment — pure functions, heavily tested
internal/history/   assigned history held per repo
internal/edit/      work-tree files read and written — runs no git
internal/api/       HTTP, auth, error JSON shape
internal/assets/    embedded frontend via //go:embed
internal/session/   the session token — minted and checked here, by both cmd/
internal/protect/   owner-only lockdown for secrets — chmod on Unix, ACL on Windows
```

Dependencies flow downward. `internal/git` knows nothing about HTTP;
`internal/api` never spawns subprocesses directly.

## Subprocesses

`exec.Command` with argument slices — never shell string interpolation.

Git environment is built from an allowlist in `internal/git/environment.go`, not
full inheritance. Add env vars one at a time with a comment explaining why.

## Tests

- Unit tests for parsing and graph lane assignment (bugs are silent here)
- Fuzz targets for parsers — `./do test fuzz`
- Integration tests against real `git` binary
- Package list for tests: `./cmd/... ./internal/...`

## Formatting and lint

Run `./do fmt` and `./do lint` — do not invoke `gofmt` or `golangci-lint`
directly.

## Header docs

If a module cannot be explained in three sentences in its package comment, it
needs restructuring.
