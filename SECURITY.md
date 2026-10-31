# Security policy

## Reporting a vulnerability

Report privately through
[GitHub's advisory form](https://github.com/ngsanogo/yagit/security/advisories/new),
never in a public issue. Say what an attacker gains and give the smallest
sequence that demonstrates it. Expect a first reply within a week. There is one
maintainer: fixes are prepared on a private fork through the advisory workflow,
and published with the advisory and a mention in the release notes.

yagit is pre-1.0. Only `main` is supported; there is nothing to backport to.

## What yagit is, in security terms

yagit is a local daemon with **read and write access to every git repository
you open with it**. It runs as you, with your credentials. Treat its session
token the way you would treat a shell on your own machine.

Two properties matter more than any other, and both are enforced in code.

**The daemon listens on the loopback by default.** The built binary binds
`127.0.0.1:7420`; only `-addr` (or `YAGIT_ADDR`) moves it, and the binary never
reads `.env` (in development `./do` derives the address from it).
`YAGIT_PUBLIC_URL`, the address a reverse proxy on the same machine serves yagit
at, keeps the daemon on the loopback and is refused beside either setting that
widens it. Otherwise `YAGIT_PUBLIC_HOST` decides: a loopback name keeps the
loopback; a non-loopback name widens to `0.0.0.0` only when `YAGIT_LISTEN_ALL=1`
is also set. Both exist to reach a headless development machine from your own
browser, not to put yagit on a network you do not control. There is no
authentication beyond the session token: do not expose the daemon to an
untrusted network without TLS, its own or a proxy's.

**Every route requires the session token**, exchanged for an HttpOnly cookie via
`POST /api/session` rather than embedded in the startup URL. It is 256 bits of
entropy, generated at startup — or, under `./do`, minted once into
`.yagit/session-token` and passed through the `YAGIT_TOKEN` environment variable,
never a command-line flag (`ps` would show it to every user). `./do token --new`
replaces it and ends every session holding the old one. That file is the only
token file: whether a daemon is running is asked of the port, never of a file the
daemon left behind.

The rest of the model follows from those two:

- **A repository is opened by path, then addressed by opaque id.** The only
  place a client-supplied path is accepted is the open call, which resolves
  symlinks and rejects anything outside `YAGIT_ROOT`. Afterwards the client only
  names ids, so a hostile page cannot designate a file.
- **The one route that writes a file works through an `os.Root` on the work
  tree.** `PUT /api/repos/{id}/file` builds no absolute path: the kernel walks
  each component and refuses one that leaves the work tree, which a check
  followed by an open cannot promise (a symlink swapped between the two beats
  it). Anything under a git directory is refused, and so is a `.git` at any
  depth, directory or pointer file: `.git/config` names programs git runs
  (`core.pager`, `core.fsmonitor`), so writing it is arbitrary execution on the
  next command. A save must carry the fingerprint of the content it started
  from, so a stale buffer cannot overwrite newer work.
- **Cookie-authenticated mutating requests must carry a known `Origin`.** A
  request presenting the token explicitly (header or query string) proves it
  knows the secret; a cookie is attached by the browser on its own and is the
  only case a third-party site could trigger. Known origins are the daemon's own
  address (loopback in its three spellings, the public host), the public URL a
  proxy serves it at, and anything `-allow-origins` adds, compared as exact
  strings. The cookie is `HttpOnly` and `SameSite=Strict`, and `Secure` whenever
  the browser reached yagit over TLS. Every response carries baseline security
  headers (CSP, `X-Frame-Options`, `Referrer-Policy`, and others).
- **Presenting a credential is rate limited; using one is not.** Six hundred
  attempts a minute per client address, counted on `POST /api/session` and on
  every request without a valid token. Requests that carry the token are not
  counted: on the loopback every legitimate client shares one address, so a
  shared budget would throttle the user and not the probe. The bound keeps a
  runaway loop from occupying the daemon; what makes the token hard to guess is
  its 256 bits. `X-Forwarded-For` is not read: a budget keyed on a header the
  client writes is one it resets at will.
- **git is never invoked through a shell.** Arguments are an array, so a branch
  name containing `;` triggers nothing. The one exception is git's doing:
  `GIT_SEQUENCE_EDITOR`, which an interactive rebase sets, is defined by git as a
  shell command. yagit quotes its own parts (executable and temporary-file
  paths), and nothing user-supplied appears in it: the plan travels as a file of
  object names and verbs the daemon checked.
- **The daemon builds git's environment from an allowlist** rather than
  inheriting yours, so a stray `GIT_DIR` or `GIT_SSH_COMMAND` has no effect. It
  pins `LC_ALL=C`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`,
  `GIT_PAGER=cat` and passes `PATH`, `HOME` and, when set, `GIT_CONFIG_GLOBAL`
  and `GIT_CONFIG_SYSTEM`. `HOME` is deliberate: yagit runs *your* git with your
  configuration.

## Verifying a release

Every binary published by the release workflow carries a Sigstore-signed
provenance attestation: what was built, from which commit, by which workflow
run. Check it before running one:

```sh
gh attestation verify yagit-linux-amd64 --repo ngsanogo/yagit
```

`SHA256SUMS` is published alongside, but a checksum only says the bytes did not
change in transit, and it is served by whoever served the binaries. The
attestation is signed by a certificate that existed only for that workflow run,
so it holds even if this repository does not.

An SPDX bill of materials is attached too. The Go side has one direct
dependency (fsnotify, and the `golang.org/x/sys` it brings), but `./do build`
embeds the built frontend into every binary, and that frontend is compiled from
several hundred npm packages.

Dependabot (npm, Go modules, GitHub Actions, seven-day cooldown) and
`./do audit` (which also covers the Go standard library) watch the dependencies.
`./do audit` is not a merge gate: its answer changes without the code changing,
so it runs weekly in CI instead.

## Design consequences

- **Plain HTTP by default.** yagit serves TLS only when both `YAGIT_TLS_CERT`
  and `YAGIT_TLS_KEY` are set (or `-tls-cert` / `-tls-key`). On plain HTTP the
  cookie omits `Secure` on purpose: a browser never sends a Secure cookie over
  `http://`, so setting it would end the session rather than harden it. The
  attribute follows the browser's scheme: a request that arrived over TLS, or
  carries `X-Forwarded-Proto: https`, gets a Secure cookie (the header is
  believed from anyone because all it can do is make the sender's own cookie
  stricter). Use TLS whenever the daemon is reachable beyond the loopback.
- **Network exposure is explicit.** A non-loopback address or `0.0.0.0` requires
  `-listen-all` or `YAGIT_LISTEN_ALL=1`. `./do` widens the listen address only
  when both a non-loopback `YAGIT_PUBLIC_HOST` and `YAGIT_LISTEN_ALL=1` are set,
  and never beside `YAGIT_PUBLIC_URL`.
- **A reverse proxy exposes the daemon as far as the proxy reaches.** The listen
  address stays on the loopback, but the session token is then the only thing
  limiting who can knock. Put yagit behind a proxy only where you would have
  widened, and give it a host name of its own: cookies are scoped by host, not
  port, so under a shared name the cookie goes to every application on it.
- **The token grants everything** — no scopes, no per-repository permissions.
  Anyone who reads `.yagit/session-token`, or the process environment of a stack
  `./do` started, has your access.
- **The token does not rotate on its own.** One that leaked into a screenshot or
  a log stays valid until `./do token --new`, which stops the stack, replaces the
  file and restarts on the new token. Deleting the file by hand revokes nothing
  until the next `./do up`. A released binary generates a token per start.
- **The token never belongs in an API URL.** The announced address has no
  `?token=`. A browser exchanges the token through `POST /api/session` (or the
  form the unauthorized page serves); `?token=` is accepted on `GET /` alone and
  is swapped for the cookie and stripped from the address before the page loads.
  Every `/api/` route takes the `X-Yagit-Token` header, so the secret never
  reaches history, a `Referer` or a proxy log.
- **Anyone with your user account has that access already**, so the token is not
  a boundary against a local attacker who is already you. `.yagit/` holds the
  token and the throwaway stack's log (every git command run, every repository
  path touched), so it is mode `0700` / `0600` on Unix and owner-only ACL on
  Windows.
- **On a multi-user host the loopback is shared.** `./do` asks the port whether a
  stack is up, first without a credential (a yagit daemon refuses, a stranger
  answers, so the stored token is never sent to something that answers) and then
  with it. Another account that binds the port while your stack is down, refuses
  the bare request and accepts any token would see the secret on that second
  probe. Do not run yagit on a shared host you do not trust, and use
  `./do token --new` if the token may have been exposed.
