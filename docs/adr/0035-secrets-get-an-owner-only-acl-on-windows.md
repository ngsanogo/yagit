# 0035 — Secrets get an owner-only ACL on Windows

**Status:** new. Applied.

## Context

Unix file modes (`0600` / `0700`) lock a secret down on Unix. On Windows, Go's
`os.Chmod` and `WriteFile` mode arguments only map onto the read-only
attribute — access is governed by the ACL. Asking for `0600` there does not
produce an owner-only file.

`golang.org/x/sys` is already in the module graph (fsnotify uses it for its
Windows backend). Setting an explicit ACL means promoting that dependency and
carrying a small platform file, not taking a first dependency.

## The decision

One function, `protect.OwnerOnly`, is what every write of a secret calls. On
Unix it is chmod. On Windows it replaces the DACL with a single entry for the
current user and marks the DACL protected, so inherited ACEs from a parent
cannot sit beside it.

The places that mint the session token — `./do`'s `.yagit/` and
`.yagit/session-token`, the daemon's `-token-file`, the stack logs under
`.yagit/` — all go through that call. `protect.Check` is the matching
assertion on every platform.

## What was decided against

- **Relying on the inherited ACL alone.** It depends on where the checkout
  sits. An explicit lockdown does not.
- **Also naming Administrators and SYSTEM in the ACL.** Matching Unix `0600`
  means the current user alone. An administrator on Windows can take ownership
  anyway.
- **Locking down the parent of `-token-file`.** That flag may point at a file
  under a home directory the user already shares. The file itself is locked;
  the directory is not.
- **A third-party ACL helper.** `golang.org/x/sys/windows` is the API the
  platform documents.

## Consequences

- `golang.org/x/sys` is a direct dependency.
- Privacy tests assert the lockdown on every platform through `protect.Check`.
