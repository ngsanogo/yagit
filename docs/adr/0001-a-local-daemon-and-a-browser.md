# 0001 — A local daemon and a browser, not a desktop application

**Status:** kept.

## The decision

yagit is a Go daemon on the loopback plus a web interface. It is not an
Electron application, not a Tauri application, not a native one.

## What it is up against

Every established git client is a desktop application — some built on a
bundled browser engine, some native to the platform. That is not a coincidence,
and the reasons are real:

- **The browser owns the keyboard.** `Ctrl`/`Cmd` + `T`, `N`, `W`, `L`, `D`,
  `R`, `F`, `P` and `1`–`9` belong to the browser and cannot be taken. A git
  client is a keyboard application; that is a genuine amputation.
- **No operating system integration.** No drag-and-drop of a folder from
  Finder or Explorer, no "Open in yagit" in a context menu, no menu bar, no
  dock badge, no native notification, no keychain.
- **A tab is not an application.** It closes by accident, it is one of forty,
  and it does not survive a browser restart the way a window does.

## Why it is kept anyway

**It is the only thing yagit does that nothing else does.** Development has
moved onto machines that have no screen: a container, a VM, a cloud
workstation, a box reached over SSH. On all of them the whole desktop category
is unavailable and the choice is a terminal or nothing. A daemon reached at a
URL works there without so much as a flag — `YAGIT_PUBLIC_HOST`, and the
listen address follows. That is not a workaround for lacking a desktop
application; it is the reason to prefer one that does not exist yet.

**Distribution is otherwise most of the work.** A desktop application means
code signing on Windows, notarisation on macOS, an auto-update channel, an
installer per platform, and a certificate that costs money and expires. That
is months of work and a recurring bill, before a single feature. `./do build`
produces six static binaries in eight seconds, and there is nothing to sign.

**The architecture is the escape hatch.** Should a desktop shell become the
right answer, a WebView pointed at the daemon is a wrapper, not a rewrite:
every line of the daemon and every line of the interface survive it unchanged.
Deciding the other way round — starting in Electron and later extracting a
daemon — is the rewrite. This order is the cheap one.

## What it costs, and what follows from it

The keyboard amputation is real and is now a design rule rather than a
surprise: **no shortcut yagit defines may collide with one the browser owns.**
The shortcut map is built from what is left, and the leftovers are plentiful —
plain letters, `g`-prefixed sequences, `?`, `/`, and the arrow keys.

The rest is accepted. yagit gains a headless story nothing in its category
has, and gives up integration it can add later from the same code.
