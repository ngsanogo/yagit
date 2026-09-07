# 0025 — The stack runs in the background, and what it started is recorded

**Status:** new. `./do dev` is kept, and is no longer the way in. Applied.

## What was decided before

`./do dev` ran the daemon and Vite in the foreground and held the terminal
until Ctrl-C. That is the right shape for watching output and the wrong one for
the other ninety per cent: opening the interface, running a command against the
API, coming back tomorrow.

## The decision

`./do up` starts the same two processes under a detached supervisor, prints the
URL and the token, and returns the terminal. `./do down` stops it, `./do
status` says whether it is up, `./do logs` follows its output. `./do dev` is
unchanged and is now the deliberate choice — every line in this terminal —
rather than the only one.

The supervisor is `./do __stack-supervise`, this same binary re-invoked. It
owns air and Vite exactly as `./do dev` does, and it exists so that something
traps the signal and stops both of them: `npm run dev` does not forward SIGTERM
to the Vite it spawned, and a background stack with no supervisor is a Vite
holding port 5173 with nobody left to ask it to stop.

## Two questions, not one

The mistake worth recording is one this design made and had to be corrected
for. "Is the stack running?" is two questions:

- **Is a supervisor of ours alive?** `.yagit/stack.pid`, and a signal 0.
- **Does the stack answer?** An HTTP probe of `/api/health` and `/`.

They differ for the whole of startup — air compiles, Vite builds the module
graph — and answering the first with the second is not a subtle bug. `./do
down` two seconds after `./do up` found no answer on the port, reported "not
running", and left the entire tree alive. A second `./do up` found no answer
either, started a second supervisor on top of the first, and overwrote the pid
file — leaving the first unreachable by `./do down` for as long as it lived,
holding both ports against its own replacement.

Every decision in `cmd/do/up.go` now names which of the two it is asking. A
supervisor that is alive and not answering is *starting*, and `./do up` waits
for it instead of starting a rival; `./do status` says so instead of "not
running"; `./do down` stops it because it is there, not because it replies.

## What is recorded, and why two files

`.yagit/stack.pid` holds the supervisor, written by `./do up`.
`.yagit/stack.children` holds the pids of air and Vite, written by whichever
command started them — `up`, `dev`, or the throwaway stack the end-to-end tests
bring up — because that is the one place that knows them.

The second file is what makes `./do down` work after the supervisor has been
killed outright, by `kill -9` or by the OOM killer. Its children are orphaned
and still holding both ports, and they are the reason the next `./do up` cannot
bind.

The alternative was asking the operating system who holds port 7420 — `lsof` on
Unix, `netstat -ano` on Windows. That was written first and then removed.
`lsof` is not pinned in `mise.toml` and is absent from most minimal Linux
images, so `./do down` became an error message on exactly the machines that
most need it; and "kill whatever holds this port" is a guess about somebody
else's process. The pids we recorded are not a guess. They are signalled as
process *groups*, so a number the system has since reused leads no group and
the signal finds nothing rather than a stranger.

## What it costs

A third state file to reason about, and two ways to start the same stack. The
supervisor also means a failure during startup has no terminal to be printed
to — so `./do up` opens `.yagit/dev.log` itself and hands it to the child as
both streams, rather than letting the child open it. That is not a detail: the
child opening its own log truncates it and then writes the one line worth
reading to a stream nobody is attached to, which is two minutes of silence
followed by an empty log.
