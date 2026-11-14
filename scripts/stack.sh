#!/bin/sh
# Runs the development stack (`mise run dev`) in the background.
#   scripts/stack.sh up | down | status | logs
# On Windows, run `do.cmd dev` in a terminal instead.
set -eu
cd "$(dirname "$0")/.."

state=.yagit
pidfile="$state/stack.pid"
logfile="$state/stack.log"
url=http://127.0.0.1:7420

running() {
  [ -f "$pidfile" ] && kill -0 "$(cat "$pidfile")" 2>/dev/null
}

# The address the daemon announced, which is the one a browser opens: behind a
# reverse proxy it is the proxy's, by a public host name it is that name's.
# The loopback answers the readiness check below either way, and is what is
# left to print when the log no longer holds an announcement. Anything may
# precede it on the line: mise names the task each line came from.
announced() {
  announcement=$(sed -n 's/^.*Open yagit: //p' "$logfile" 2>/dev/null | tail -n 1)
  echo "${announcement:-$url/}"
}

card() {
  echo "yagit is running: $(announced)"
  echo "session token:    $(cat .yagit/session-token)"
}

case "${1:-}" in
  up)
    if running; then card; exit 0; fi
    if curl --silent --output /dev/null "$url/"; then
      echo "error: something already answers on $url (a stack started another way?); stop it first" >&2
      exit 1
    fi
    mkdir -p "$state"
    mise run dev >"$logfile" 2>&1 </dev/null &
    echo $! >"$pidfile"
    for _ in $(seq 120); do
      if curl --silent --output /dev/null "$url/"; then card; exit 0; fi
      running || break
      sleep 1
    done
    echo "error: the stack did not start; see $logfile" >&2
    tail -n 30 "$logfile" >&2
    exit 1
    ;;
  down)
    if running; then
      # mise stops the tasks it runs (air with the daemon, and Vite).
      kill -TERM "$(cat "$pidfile")" 2>/dev/null || true
      echo "stopped"
    else
      echo "not running"
    fi
    rm -f "$pidfile"
    ;;
  status)
    if running; then card; else echo "not running"; exit 1; fi
    ;;
  logs)
    exec tail -n 100 -f "$logfile"
    ;;
  *)
    echo "usage: scripts/stack.sh up|down|status|logs" >&2
    exit 2
    ;;
esac
