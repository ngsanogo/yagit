#!/bin/sh
#
# ./do — yagit's single entry point.
#
#   ./do up [--restart] [--foreground] [--new-token]
#                                 start the stack in the background; print the URL and token
#   ./do down                     stop the background stack
#   ./do status                   report whether the stack is running
#   ./do logs [--no-follow]       follow the background stack's output
#   ./do dev [--new-token]        run the stack in the foreground, with logs in the terminal
#   ./do bootstrap [--browsers]   install dependencies into .yagit/ and web/node_modules/
#   ./do shell-hook [--write]     generate yagit-up, yagit-down and yagit-logs aliases
#   ./do build                    build the frontend, then the binaries into dist/
#   ./do test [go|web|e2e|release|bench|fuzz|soak|coverage]
#                                 run tests; no argument runs go, web and e2e
#   ./do lint                     static analysis of Go, the frontend, this shim, CI
#   ./do audit                    check the dependencies against the vulnerability databases
#   ./do fmt                      reformat the code
#   ./do shot [url] [out]         screenshot a page through Playwright
#   ./do drive [url]              drive the running interface from stdin
#   ./do token                    print the session token, to query the API with curl
#   ./do version                  print the version this checkout would be released as
#   ./do agent sync|check         regenerate the tool shims from agent/, or verify them
#
# `./do help` prints the same list from the program itself, and `./do <cmd>
# --help` prints one command's usage.
#
# This file does two things: keep the toolchain inside .yagit/, then hand over
# to cmd/do, which is where every project command actually lives.
#
# do.cmd sets the same variables for Windows. These two are the only places
# that name these directories — nothing in cmd/do does, because by the time it
# runs they already exist and the tools are already pointed at them.

set -eu

cd "$(dirname "$0")"

if ! command -v mise >/dev/null 2>&1; then
  echo "error: mise was not found, and it is yagit's only prerequisite." >&2
  echo "       Install it:  curl https://mise.run | sh" >&2
  echo "       Then run this command again." >&2
  exit 1
fi

YAGIT_STATE="$PWD/.yagit"
mkdir -p "$YAGIT_STATE/mise" "$YAGIT_STATE/mise-cache" "$YAGIT_STATE/mise-state" \
         "$YAGIT_STATE/go-cache" "$YAGIT_STATE/go-mod-cache" \
         "$YAGIT_STATE/npm-cache" "$YAGIT_STATE/browsers"
chmod 700 "$YAGIT_STATE"

# Everything this project downloads or compiles lands inside the checkout, so
# that removing the clone removes all of it and no two checkouts can disagree
# about a tool version. mise gets all three of its directories: leaving the
# cache and the state under $HOME would make "everything lives under .yagit/"
# almost true, which is worse than not claiming it.
export MISE_DATA_DIR="$YAGIT_STATE/mise"
export MISE_CACHE_DIR="$YAGIT_STATE/mise-cache"
export MISE_STATE_DIR="$YAGIT_STATE/mise-state"
export GOCACHE="$YAGIT_STATE/go-cache"
export GOMODCACHE="$YAGIT_STATE/go-mod-cache"
export npm_config_cache="$YAGIT_STATE/npm-cache"
export PLAYWRIGHT_BROWSERS_PATH="$YAGIT_STATE/browsers"

# -modcacherw is what keeps `rm -rf yagit/` from failing.
#
# Go writes the module cache read-only, directories included, and a directory
# without write permission cannot have its entries unlinked. With the cache
# inside the checkout, the promise above would otherwise end in a screen of
# "Permission denied" — and `git clean -xfd` would fail the same way.
export GOFLAGS="-modcacherw${GOFLAGS:+ $GOFLAGS}"

# `mise install` is idempotent and costs about 40 ms once everything is in
# place. Paying that every time saves having a separate install command that
# people must know about and remember to re-run after a `git pull`.
mise install --quiet

# Compiled, not `go run`. Both cost about the same once Go's build cache is
# warm, but `go run` stays in the process tree as a parent that deliberately
# ignores SIGINT — so `kill` aimed at this script would never reach the program,
# and `./do dev` would leave Vite and the daemon behind.
mise exec -- go build -o .yagit/do ./cmd/do

# exec, so the program replaces this shell rather than being supervised by it.
# Signals and the exit code then reach it exactly as the terminal sent them.
exec mise exec -- ./.yagit/do "$@"
