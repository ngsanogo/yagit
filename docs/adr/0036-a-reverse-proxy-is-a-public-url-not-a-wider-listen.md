# 0036 — A reverse proxy is a public URL, not a wider listen address

**Status:** new. Adds a second way to browse from another machine beside the
one [0001](0001-a-local-daemon-and-a-browser.md) describes, and a refusal in the
shape [0014](0014-a-breaking-configuration-change-announces-itself.md) settled.
Applied.

## What was decided before

There was one way to reach yagit from a browser on another machine: name the
machine in `YAGIT_PUBLIC_HOST`, acknowledge the exposure with
`YAGIT_LISTEN_ALL=1`, and let `./do` bind `0.0.0.0:7420`. Everything a browser
was told or allowed was then built from the daemon's own socket — the origins
it accepts, the URL on the card, the scheme that decides the session cookie's
`Secure` attribute — and Vite's hot-reload client was pinned to the daemon's
port, because the daemon's port was the page's.

## The problem

A development VM that serves each of its applications under a host name of its
own, through a reverse proxy: `https://yagit.<machine>`. The chain, measured:
the browser speaks TLS to a proxy on the VM's host, which speaks plain HTTP to
a proxy on the VM, which connects to `127.0.0.1:7420` with the host name
preserved. The daemon receives `Host: yagit.<machine>`,
`X-Forwarded-Proto: https`, and the browser's `Origin: https://yagit.<machine>`.
WebSocket upgrades pass through.

A name per application is not decoration. Browsers scope cookies by host and
not by port, and HSTS the same way, so applications sharing one name on
different ports overwrite each other's sessions, and one that sends HSTS forces
HTTPS on all the others. The same machine also ran another project's Vite,
which wanted 5173 as yagit's did, and with `strictPort` whichever started
second failed.

Behind that proxy yagit broke in four places, and none of them said why.

- **Every write was refused.** The allowlist held `http://127.0.0.1:7420`, its
  two other loopback spellings, and `http://<public host>:7420`. The browser
  presents `https://yagit.<machine>` — the proxy's scheme and the proxy's
  port, neither of which the daemon's socket predicts. The door page's own
  form POST was answered `403 origin rejected`, and so was every
  cookie-authenticated write after a session got in some other way.
- **The session cookie lacked `Secure`.** It followed the daemon's scheme,
  plain HTTP, while the browser's side of the chain was HTTPS.
- **Hot reload was dead.** `hmr.clientPort` sent the client to
  `wss://yagit.<machine>:7420`: around the proxy, to a port nothing outside
  the VM reaches. One console line said so.
- **Every address printed was the daemon's, not the proxy's,** and naming a
  host at all meant widening. The card and the daemon's startup line could only
  say `http://<host>:7420/`, and getting there took `0.0.0.0` for a proxy that
  connects over the loopback.

## The decision

**`YAGIT_PUBLIC_URL` in `.env` is the address a reverse proxy on this machine
serves yagit at.** With it set:

- The daemon stays on `127.0.0.1:7420`. The proxy is a local client like any
  other, so nothing listens on the network that was not listening before.
- `./do` checks the value before anything starts and hands it to the daemon as
  `YAGIT_PUBLIC_URL` — always, and empty when `.env` names none, so a value
  exported in the shell cannot add an origin the card does not print.
- The daemon learns `-public-url` (and reads `YAGIT_PUBLIC_URL`): it accepts
  the URL's origin on cookie-authenticated writes and announces the URL at
  startup, in place of the loopback address a browser elsewhere cannot open.
- The card `./do up` prints shows the public URL.
- Beside a non-loopback `YAGIT_PUBLIC_HOST`, or beside `YAGIT_LISTEN_ALL=1`, it
  is refused. The refusal names what asked for the widening, the line or lines
  to comment out to keep the proxy, and `comment YAGIT_PUBLIC_URL out` for the
  other way. A `.env` that says both is most often the old configuration with
  the new line pasted under it, and guessing either way is wrong for somebody:
  widening exposes a daemon its owner put behind a proxy, and staying on the
  loopback leaves dead a direct address its owner meant to use.
- `./do up` and `./do dev` check the listen address before they install or
  stop anything. That migration is typed as `./do up --restart`, and a refusal
  left to the supervisor arrived after the working stack had been stopped, in
  the tail of a log. The older refusal — a remote host without
  `YAGIT_LISTEN_ALL=1` — moves forward with it.

**The daemon is told the URL, not only the origin.** Folding the origin into
`YAGIT_ALLOW_ORIGINS`, merged with whatever the shell exports, was the first
design and it works for the allowlist. It does not work for the startup line,
which would then name an address that opens nothing — the same line the
refusal of an unknown origin sends its reader to. Told the URL once, the daemon
derives the origin itself, and the shell's `YAGIT_ALLOW_ORIGINS` is left alone
by construction rather than by a merge. A released binary behind a proxy gets
the same flag.

**One rule reads the URL, for both programs,** in `internal/publicurl`, as
`internal/session` is the one place for the token. It returns the origin a
browser presents, because the allowlist is compared as exact strings and a
browser never writes an upper-case host or a default port:
`https://Yagit.example.com:443/` kept as typed would match nothing, and the
first sign would be a 403 naming an origin that looks identical to the one
configured. A path is refused rather than dropped — the interface requests
`/api/…` from the root of its host, so yagit cannot be served under a prefix —
and so are a query, a fragment and credentials.

**The cookie's `Secure` follows the browser's scheme.** It is set when the
daemon serves TLS itself, when the request arrived over TLS, or when the
leftmost value of `X-Forwarded-Proto` is `https`. That header is believed from
anyone, and it is safe for this one use: the attribute goes on a cookie sent
back to whoever sent the header, and `Secure` can only make that cookie
stricter. No page can make somebody else's browser send the header without a
CORS preflight, which yagit never grants. Nothing else in the daemon reads it,
and nothing else should. A session on plain HTTP over the loopback — the
end-to-end suite, `./do shot` — sends no such header and keeps its cookie.

**Hot reload follows the page.** Vite's client is told no port, and opens its
socket on the page's own host and port: `ws://127.0.0.1:7420/` when the browser
comes straight to the daemon, `wss://yagit.<machine>/` through the proxy. The
daemon passes the upgrade to Vite like any other request outside `/api`, as it
already did. Read in Vite 8's client rather than assumed: with no port
configured it joins the page's hostname and `location.port`, and an HTTPS page
on the default port gives `wss://host:/`, which the URL parser reads as
`wss://host/`. `YAGIT_DAEMON_PORT` had no other reader and is gone.

**Vite listens on 7421.** Beside the daemon's port, where no other project has
a reason to be, rather than on Vite's default, which every Vite project on the
machine also wants.

## What was decided against

- **Keeping the widening, and adding the proxy's origin to it.** It would have
  worked, with the daemon listening on every interface for the benefit of a
  proxy on the loopback.
- **Both ways in at once.** Two URLs for the card, a listen address wider than
  the proxy needs, and nobody asking for it. The refusal is cheaper to lift
  later than an accepted ambiguity is to take away.
- **Deriving accepted origins from `Host` or `X-Forwarded-Host`.** Accepting
  whatever origin matches the host a request names makes the allowlist a
  function of the request. A page rebinding its own DNS name to the loopback
  presents a matching pair by construction, which is the attack a fixed list
  exists to refuse.
- **Reading `Forwarded` (RFC 7239) as well.** The proxies in the chain send
  `X-Forwarded-Proto`. A parser for quoted pairs in a header nothing here sends
  would be tested against no proxy at all.
- **Keying the credential budget on `X-Forwarded-For`.** A budget keyed on a
  header the client writes is one the client resets at will. Behind a proxy
  every browser shares the proxy's address, as every local client already
  shares the loopback's.
- **Ignoring `YAGIT_LISTEN_ALL=1` beside a public URL instead of refusing it.**
  The flag acknowledges an exposure. Acknowledging one that does not happen
  reads, to the person who set it, as direct access that is not there.

## What it costs

A fourth key in `.env`, a second refusal whose wording is under test, and a
small package both programs import.

The proxy becomes part of what the daemon's safety rests on. The loopback bind
no longer limits who can reach the daemon — the proxy carries requests from its
own network to it — so the session token does, exactly as after widening.
SECURITY.md says so, and says to give yagit a host name of its own: under a
shared one, the session cookie travels to every other application on it.

`./do up` on a stack that is already running prints its card from the `.env`
on disk, not the one the stack started with. Editing `YAGIT_PUBLIC_URL` takes
`./do up --restart` to reach the daemon; until then the card can name an
address whose origin the daemon does not accept yet.

With no port pinned, Vite's client falls back to Vite's own address,
`127.0.0.1:7421`, when its first socket fails to open. That names the loopback
of whatever machine the browser runs on, so from anywhere else it normally
fails as well and adds a console line. It is only ever tried after the socket
on the page's own origin has failed.

The tests that hold the decision: `TestListenAddressStaysOnTheLoopbackBehindAProxy`
and `TestListenAddressRefusesAProxyBesideTheWidening` in `cmd/do/do_test.go`;
`TestDaemonEnvironmentHandsThePublicURLDown` and the ambient-environment
assertions in `cmd/do/project_test.go`; `TestAcceptedOriginsTakeThePublicURLAsTheProxyServesIt`
in `cmd/yagit/main_test.go`; `TestTheSecureAttributeFollowsTheScheme` and
`TestAProxysOriginIsAcceptedOnceAllowed` in `internal/api/auth_test.go`; and
`internal/publicurl`'s own.

## References

- `listenAddress`, `publicURLBesideWidening` and `daemonEnvironment` in
  `cmd/do/project.go`; `browserURL` in `cmd/do/up.go`
- `internal/publicurl`
- `acceptedOrigins` and `announcedURL` in `cmd/yagit/main.go`
- `sessionCookie` and `reachedOverTLS` in `internal/api/auth.go`
- `web/vite.config.ts`
- [`.env.example`](../../.env.example), and
  [SECURITY.md](../../SECURITY.md), "Design consequences"
