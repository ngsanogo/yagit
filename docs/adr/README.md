# Architecture decision records

A record here says what was decided, what it was decided *against*, and what
the decision costs. It is history, not law: a record explains why something is
the way it is, and none of them forbids changing it. Reversing one means adding
a record that says so and why — not asking permission from the old one.

| | Decision | Verdict |
| --- | --- | --- |
| [0001](0001-a-local-daemon-and-a-browser.md) | A local daemon and a browser, not a desktop application | kept |
| [0002](0002-drive-the-git-binary.md) | Drive the `git` binary, not a library | kept |
| [0003](0003-the-commit-graph-is-svg.md) | The commit graph is virtualised SVG | **reversed** — it was canvas |
| [0004](0004-diffs-are-codemirror.md) | Diffs are CodeMirror 6 | **reversed by [0034](0034-the-diff-is-drawn-not-edited.md)** — it had reversed Monaco |
| [0005](0005-server-state-and-virtualisation.md) | Server state is TanStack Query, keyed by page | new |
| [0006](0006-no-router.md) | No router library | kept |
| [0007](0007-one-event-stream.md) | One SSE stream per session, not one per repository | refined |
| [0008](0008-file-watching-through-fsnotify.md) | File watching through fsnotify, the first Go dependency | **reversed** — it was raw inotify |
| [0009](0009-the-entry-point-is-go.md) | The entry point is Go, not bash | **reversed** — it was bash |
| [0010](0010-every-shipped-platform-is-a-gate.md) | Every platform we ship a binary for runs a gate | new |
| [0011](0011-tailwind-generates-the-utilities.md) | Tailwind generates the utilities from `tokens.css` | kept |
| [0012](0012-lanes-are-assigned-in-the-daemon.md) | Lanes are assigned in the daemon, and the history is held there | new |
| [0013](0013-agent-config-is-tool-agnostic.md) | Agent configuration is tool-agnostic, under `agent/` | new |
| [0014](0014-a-breaking-configuration-change-announces-itself.md) | A breaking configuration change announces itself, starting with the refusal | new |
| [0015](0015-the-work-tree-is-polled.md) | The repository is watched, the work tree is polled | completes 0008 |
| [0016](0016-the-graph-draws-what-is-checked-out.md) | The graph draws what is checked out, and every ref is a choice | new |
| [0017](0017-yagit-edits-files.md) | yagit edits files, and that is not a git operation | new |
| [0018](0018-overlays-are-the-platforms.md) | Overlays are the platform's top layer, not a library's | new |
| [0019](0019-the-manifest-carries-what-agents-may-not-read.md) | The manifest carries what agents may not read | completes 0013 |
| [0020](0020-the-network-is-gits-and-so-are-the-credentials.md) | The network is git's, and so are the credentials | new |
| [0021](0021-a-shown-command-is-not-a-setting.md) | A command the interface shows is a command configuration cannot change | new |
| [0022](0022-a-merge-is-checked-before-it-runs.md) | A merge is checked against the repository before it runs | completes 0021 |
| [0023](0023-a-rebase-is-read-in-both-directions.md) | A rebase is read in both directions, and says what it takes away | extends 0022 |
| [0024](0024-the-checkout-owns-its-toolchain.md) | The checkout owns its toolchain, under `.yagit/` | amends 0009 |
| [0025](0025-the-stack-runs-in-the-background.md) | The stack runs in the background, and what it started is recorded | new |
| [0026](0026-the-session-token-outlives-the-run.md) | The session token outlives the run that minted it | extends 0025 |
| [0027](0027-the-stack-is-asked-not-a-file.md) | Whether a stack is up is asked of the port, not of a file | **reverses** the second token file of 0026 |
| [0028](0028-a-stash-is-a-position-and-a-name.md) | A stash is named by a position and checked by an object name | extends 0022 |
| [0029](0029-a-rebase-plan-is-written-not-edited.md) | A rebase plan is written by the daemon, not edited by a person | extends 0021 |
| [0030](0030-progress-rides-the-request.md) | Progress for a long command rides the request that started it | completes 0020 |
| [0031](0031-undo-reads-the-head-reflog.md) | Undo reads the HEAD reflog, and leases by object names | extends 0022, 0028 |
| [0032](0032-a-deleted-branch-is-remembered-not-recalled.md) | A deleted branch is remembered before the delete, not recalled after it | corrects 0031 |
| [0033](0033-a-chosen-set-of-refs-is-a-history.md) | A chosen set of refs is a history, not a filter over one | extends 0016 |
| [0034](0034-the-diff-is-drawn-not-edited.md) | The diff is drawn, not edited | **reverses [0004](0004-diffs-are-codemirror.md)** |
| [0035](0035-secrets-get-an-owner-only-acl-on-windows.md) | Secrets get an owner-only ACL on Windows | new |
| [0036](0036-a-reverse-proxy-is-a-public-url-not-a-wider-listen.md) | A reverse proxy is a public URL, not a wider listen address | new |

## Alternatives considered without a full record

These were examined and left as they are. They get a line rather than a page,
because nothing about the answer is subtle.

- **Go for the daemon.** One static binary per platform, no runtime to install,
  `embed.FS` for the frontend, and a standard library that already covers HTTP,
  subprocesses and crypto. Rust would trade months of development for a
  performance margin that a program whose hot path is `fork`/`exec` of `git`
  cannot spend. Node would reintroduce the runtime yagit is trying not to ask
  anyone to install.
- **mise, with a lockfile.** One prerequisite, user space, no sudo, a checksum
  per tool per platform. Nix is stronger and asks far more of a contributor;
  Docker moves the problem into a daemon that is itself a prerequisite.
- **Every third party pinned by content.** `mise.lock`, `package-lock.json`,
  and a commit digest for every GitHub Action. A tag is a mutable pointer
  someone else controls.
- **Vite, Vitest, Playwright, React 19.** Boring, current, and each is what the
  rest of the ecosystem assumes.
- **TypeScript stays on 6.** 7 is the native compiler and is out, but every
  release of `typescript-eslint`, canary included, declares
  `typescript >=4.8.4 <6.1.0` as a peer dependency, so npm refuses the
  combination outright — the bump cannot install, let alone be reviewed. The
  ignore rule in `.github/dependabot.yml` says so, and it is still true. Check
  it with `npm view typescript-eslint peerDependencies`.
