# yagit

A graphical Git client in your browser. A local daemon serves the UI on
loopback, drives the `git` binary already on your machine, and puts the
commit graph at the center of the screen.

[![CI](https://github.com/ngsanogo/yagit/actions/workflows/ci.yml/badge.svg)](https://github.com/ngsanogo/yagit/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Pre-1.0.** Everything listed below is built and covered by the gates, on
Linux, macOS and Windows.

## What you get

- The commit graph as the main object — searchable history, branches, remotes,
  tags, and a choice of which refs the picture draws
- Stage by file, hunk, or line; commit and amend; discard that names what it
  destroys
- Merge, rebase (including interactive), stash, cherry-pick, revert, reset,
  undo
- Conflict resolution in the interface, worktrees, submodules, LFS pointers
  named in diffs
- Every destructive step shows the exact `git` command before it runs, and a
  log panel records what actually ran

yagit authenticates nothing and never asks for a password: network access and
credentials stay with git and your environment.

## Requirements

- **git** on your PATH — **2.31** or newer (**2.38+** for rebase)
- Linux, macOS, or Windows

## Run from source

Prerequisite: [mise](https://mise.jdx.dev) (`curl https://mise.run | sh`, or
`winget install jdx.mise` on Windows).

```sh
git clone https://github.com/ngsanogo/yagit
cd yagit
./do up
```

On Windows use `do.cmd` with the same subcommands. `./do` installs the pinned
toolchain under `.yagit/` on first run, starts the stack, and prints the URL
and token. Open that URL and paste the session token on first visit. Treat the
token like a shell on this machine — see [SECURITY.md](SECURITY.md).

By default the daemon may open repositories under your home directory. Set
`YAGIT_ROOT` to narrow that boundary.

Full command table and gates: [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). How
to contribute: [CONTRIBUTING.md](CONTRIBUTING.md).

## Install from a release

The installers download the binary for this machine, check it against
`SHA256SUMS`, and put a `yagit` launcher on your PATH (usually `~/.local/bin`
on Unix, `%USERPROFILE%\.local\bin` on Windows).

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/install.ps1 | iex"
```

Pin a tag with `YAGIT_VERSION=<tag>` before the install command. Then:

```sh
yagit
```

A checksum says the bytes arrived intact. It says nothing about where they came
from, and it is published by whoever published the binaries. Every release
binary also carries a Sigstore attestation of the workflow run that built it,
which anyone can check without trusting this repository:

```sh
gh attestation verify yagit-linux-amd64 --repo ngsanogo/yagit
```

## Uninstall

macOS / Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/uninstall.sh | sh
```

Windows (PowerShell):

```powershell
powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/ngsanogo/yagit/main/scripts/uninstall.ps1 | iex"
```

Removes the launcher and the binary under `YAGIT_HOME`. The session token is
left where it is; the script prints the path so you can delete it yourself.

## Documentation

| Document | For |
| --- | --- |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | Install from source, `./do`, toolchain |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | How the pieces fit |
| [docs/API.md](docs/API.md) | HTTP routes |
| [docs/ROADMAP.md](docs/ROADMAP.md) | What each phase landed |
| [docs/adr/](docs/adr/README.md) | Why things are built this way |
| [SECURITY.md](SECURITY.md) | Threat model and the session token |
| [ACCESSIBILITY.md](ACCESSIBILITY.md) | Accessibility |

## License

[MIT](LICENSE).
