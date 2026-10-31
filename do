#!/bin/sh
# ./do <task> [args] — a shortcut for `mise run <task>`; `./do` alone lists the
# tasks. `./do test go` runs the `test:go` task. mise is the only prerequisite.
set -eu
cd "$(dirname "$0")"

if ! command -v mise >/dev/null 2>&1; then
  echo "error: mise is yagit's only prerequisite: curl https://mise.run | sh" >&2
  exit 1
fi
mise install --quiet

if [ $# -eq 0 ] || [ "$1" = help ]; then
  exec mise tasks
fi
task=$1
shift
if [ "$task" = test ] && [ $# -gt 0 ]; then
  task="test:$1"
  shift
fi
exec mise run "$task" "$@"
