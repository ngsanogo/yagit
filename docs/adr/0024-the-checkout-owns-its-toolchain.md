# 0024 — The checkout owns its toolchain

**Status:** new. Amends the closing note of
[0009](0009-the-entry-point-is-go.md), which said the two shims hold no
decision. They now hold exactly one. Applied.

## What was decided before

mise installed the pinned tools where mise installs them:
`~/.local/share/mise`. Go used `~/.cache/go-build` and `~/go/pkg/mod`, npm used
`~/.npm`, Playwright used `~/.cache/ms-playwright`. Four caches in a home
directory, none of them named after this project.

## What it cost

Nothing dramatic, and that is why it lasted. Two checkouts of yagit shared one
mise data directory, so the tools a branch pinned were the tools every branch
got until the next `mise install`. Deleting the clone left several hundred
megabytes behind in four places, and nothing said where. On a machine with more
than one Go project, "which Go compiled this" had no local answer.

## The decision

The `./do` shim points every tool at a directory under `.yagit/` in the
checkout, before it runs anything:

```sh
export MISE_DATA_DIR="$YAGIT_STATE/mise"
export MISE_CACHE_DIR="$YAGIT_STATE/mise-cache"
export MISE_STATE_DIR="$YAGIT_STATE/mise-state"
export GOCACHE="$YAGIT_STATE/go-cache"
export GOMODCACHE="$YAGIT_STATE/go-mod-cache"
export npm_config_cache="$YAGIT_STATE/npm-cache"
export PLAYWRIGHT_BROWSERS_PATH="$YAGIT_STATE/browsers"
```

All three of mise's directories, not just the data one. A claim that is almost
true — "everything lives under `.yagit/`, except the cache and the state" — is
worse than no claim, because nobody remembers the exception.

`do.cmd` sets the same variables for Windows. Those two files are the only
place the directory names appear. `cmd/do` does not name them and does not
create them: by the time it runs they exist and the tools already point at
them, so a copy in Go would be a second spelling that could differ from the
first without anything noticing.

## What it costs, and what had to be true first

**`rm -rf` on the clone had to keep working.** Go writes its module cache
read-only — files *and* directories — and a directory without write permission
cannot have its entries unlinked. With the module cache inside the checkout,
"removing the clone removes all of it" would have ended in a screen of
`Permission denied`, and `git clean -xfd` would have failed the same way. The
shim sets `GOFLAGS=-modcacherw`, which is the whole of the fix and the reason
the promise can be made at all.

**CI had to be told.** Three cache steps named `~/.npm` and
`~/.cache/ms-playwright`, and `jdx/mise-action` cached its own default
directory. Every one of them kept working and every one of them became useless
on the same commit: they restored data that nothing afterwards read, while each
job re-downloaded the toolchain and a 150 MB Chromium. The workflows now cache
`.yagit/mise`, `.yagit/npm-cache`, `.yagit/browsers` and the two Go caches,
and the action installs only the mise binary — the tools are the shim's job, on
a runner as much as on a laptop.

**Disk.** A second checkout is a second toolchain: roughly 600 MB rather than a
shared one. That is the price of a checkout that cannot be affected by, and
cannot affect, anything outside itself.
