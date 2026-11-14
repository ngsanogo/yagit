# Open-source & legal-safety policy

**Goal:** use, ship, and recommend **100% open-source** building blocks —
[OSI-approved](https://opensource.org/licenses) licenses — so contributors and
operators do **not** take on avoidable copyright / license litigation risk.

This is a **hard product rule**, not a slogan.

## Rules (MUST)

1. **Product license** — yagit itself is **MIT** only.
2. **Dependencies** — libraries we vendor, bundle, or require at build/runtime
   MUST be under an OSI-approved license compatible with MIT distribution of
   this project.
3. **No source-available traps** — do **not** add code or recommended servers
   under non-OSI “open” licenses, including (non-exhaustive):
   **SSPL**, **BSL**, **SUL**, **Fair Source**, **Commons Clause**,
   **Elastic License**, **Server Side Public License**, proprietary SDKs,
   or “free for non-commercial / evaluation only” terms.
4. **No paid feature locks** in our code — capabilities are open or on the
   public roadmap (`docs/ROADMAP.md`).
5. **Refuse to guess** — if a license is unclear, treat it as **blocked** until
   verified against https://opensource.org/licenses.

## Allowed exception — interoperability

Driving the operator's own **git** binary, credential helpers, SSH agent, and
GPG agent is the product's purpose. Those tools stay on the machine; yagit does
not redistribute them. Connecting **to** a proprietary system the operator
already runs is allowed **only** via an **OSI-licensed** path we ship. We do
**not** redistribute proprietary servers or drivers with non-OSI EULAs.

## Official default stack (OSI)

| Piece     | Choice                                              | License            |
| --------- | --------------------------------------------------- | ------------------ |
| App       | yagit                                               | MIT                |
| Language  | Go (toolchain pinned in `mise.toml`)                | BSD-3-Clause       |
| Frontend  | React / TypeScript / Vite (see `web/package.json`)  | MIT (typical)      |
| Toolchain | mise                                                | MIT                |
| VCS       | The operator's `git` on PATH                        | GPL-2.0 (upstream) |

**Prefer** free, redistributable tooling — not a proprietary desktop runtime as
a **requirement**. Documented alternatives are fine.

## Explicitly out of the default path

| Product / pattern                                          | Why                         |
| ---------------------------------------------------------- | --------------------------- |
| Non-OSI “community” editions with SSPL/BSL traps           | Legal surface               |
| Proprietary SDKs or commercial “Pro” libraries             | Proprietary                 |
| License servers, phone-home entitlement checks             | Paid-lock pattern           |
| Telemetry on by default                                    | Privacy; SDKs must be OSI and **opt-in** if present |

## How to add a dependency or stack piece

1. Confirm the license is on the [OSI list](https://opensource.org/licenses).
2. Confirm MIT-compatible redistribution for anything we ship in binaries or
   release assets.
3. Prefer well-known permissive (MIT, Apache-2.0, BSD) licenses.
4. Document the license next to the component (`NOTICE.md`, or this file).
5. If unclear: **do not add it**. Open an issue and wait.

Track removals or replacements in `docs/ROADMAP.md` when a dependency fails
this policy.
