import type {
  CherryPickPlan,
  ClonePlan,
  InitPlan,
  CommitPage,
  CommitResult,
  ConflictSide,
  DiffSide,
  DiscardPlan,
  DiscoverResult,
  FileDiff,
  FileHistory,
  Blame,
  LineHistory,
  SearchField,
  Submodule,
  SearchResult as SearchResultPayload,
  UndoPlan,
  Worktree,
  WorktreeRequest,
  UndoStatus,
  GitExecution,
  GitFailure,
  HistoryScope,
  InteractiveRebasePlan,
  InteractiveRebaseResult,
  LFSSupport,
  LineSelection,
  LocatedCommit,
  MergePlan,
  Operation,
  OperationAction,
  OperationPlan,
  PreparedMessage,
  PullStrategy,
  PushPlan,
  UpstreamPlan,
  RebasePlan,
  RebaseStep,
  RefsPayload,
  Remote,
  Repository,
  ResetMode,
  ResetPlan,
  RevertPlan,
  SaveResult,
  Stash,
  StashApplyMode,
  StashApplyPlan,
  StashDetail,
  StashDropPlan,
  StashHandle,
  StashPushPlan,
  TagPushPlan,
  WorkFile,
  WorkingDirectory,
} from './types';

import {
  NETWORK_IDLE_MS,
  NETWORK_TIMEOUT_MS,
  idleDeadline,
  REQUEST_TIMEOUT_MS,
  requestTimeoutSignal,
  waitedFor,
} from '../lib/fetchTimeout';

/**
 * The one place the daemon is spoken to.
 *
 * Every route requires the session token, and the browser already holds it as
 * an HttpOnly cookie by the time this module runs: a request without one is
 * answered by the daemon's own page, which posts the token to /api/session
 * and takes the cookie back. So nothing here handles credentials —
 * `credentials: 'same-origin'` is the whole of it, and the token stays
 * somewhere JavaScript cannot reach.
 *
 * Single origin, so the paths are relative. The daemon serves this application
 * and proxies to Vite in development, which means there is no base URL to
 * configure and no CORS to arrange — see the note in README on why development
 * and production go through the same door.
 */

/**
 * An error carrying everything the daemon said about it.
 *
 * The project's central promise is that a git failure reaches the user whole:
 * the exact command, its exit code, its raw stderr. Collapsing that into a
 * message would be the "Something went wrong" this codebase exists to avoid,
 * so the detail travels on the error itself and the interface decides how much
 * to show.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly git?: GitFailure;

  constructor(status: number, message: string, git?: GitFailure, options?: ErrorOptions) {
    super(message, options);
    this.name = 'ApiError';
    this.status = status;
    if (git !== undefined) {
      this.git = git;
    }
  }
}

function withRequestTimeout(caller: AbortSignal | null | undefined, afterMs: number): AbortSignal {
  const timeout = requestTimeoutSignal(afterMs);
  return caller === null || caller === undefined ? timeout : AbortSignal.any([caller, timeout]);
}

interface RequestOptions extends RequestInit {
  /**
   * How long to wait, when the ordinary deadline is the wrong one.
   *
   * Set by the routes that cross a network (fetch, pull, push, clone) and by
   * nothing else. It is named at the call site rather than derived from the
   * path, because a timeout that guessed from a URL would be a rule to keep
   * in step with the routing table.
   */
  timeoutMs?: number;
}

/**
 * The chosen references as query parameters, or nothing.
 *
 * Repeated `ref=` rather than one comma-separated value, because a ref name
 * may hold a comma — git forbids a short list of bytes and that is not among
 * them — and a separator a name can contain is one that eventually splits a
 * reference into two that do not exist (docs/adr/0033).
 *
 * Empty under the two scopes that do not read refs, and the daemon refuses a
 * `ref=` sent with either: a client narrowing a walk that ignores the
 * parameter would get the whole repository back with no sign of it. Dropping
 * them here is what makes that refusal unreachable from this interface rather
 * than something it has to remember not to trip.
 */
function refQuery(scope: HistoryScope, refs: readonly string[]): string {
  if (scope !== 'refs') {
    return '';
  }
  return refs.map((ref) => `&ref=${encodeURIComponent(ref)}`).join('');
}

async function request<T>(path: string, options?: RequestOptions): Promise<T> {
  // Pulled out of what reaches fetch: it is this module's option, not one of
  // the browser's, and passing it through would leave a field in the request
  // init that nothing reads.
  const { timeoutMs = REQUEST_TIMEOUT_MS, ...init } = options ?? {};

  let response: Response;
  try {
    response = await fetch(path, {
      ...init,
      credentials: 'same-origin',
      headers: { Accept: 'application/json', ...init?.headers },
      // A caller's own signal is added to the timeout rather than put in its
      // place: a request nobody cancels still has to give up on a daemon that
      // has stopped answering, and one the interface has superseded still has
      // to stop.
      signal: withRequestTimeout(init.signal, timeoutMs),
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'TimeoutError') {
      throw new ApiError(0, `the daemon did not answer within ${waitedFor(timeoutMs)}`, undefined, {
        cause,
      });
    }
    // fetch rejects only when the request never completed: the daemon stopped,
    // the machine went to sleep, the network moved. "Failed to fetch" on its
    // own tells the user nothing about which of those it was, so the message
    // says what is actually unreachable — and the original is kept as the
    // cause rather than dropped, because the console is where the last clue
    // lives when the message turns out not to be enough.
    throw new ApiError(0, `the daemon is not answering on ${window.location.host}`, undefined, {
      cause,
    });
  }

  if (response.status === 401) {
    // The session ended — the token was rotated, or this daemon was never
    // started by the ./do that minted the cookie's. Reloading lands on the
    // daemon's own page, which explains how to get back in, rather than
    // leaving a dead interface on screen.
    window.location.reload();
    throw new ApiError(401, 'the session expired');
  }

  const body: unknown = await response.json().catch(() => null);

  if (!response.ok) {
    const detail =
      body !== null && typeof body === 'object' && 'error' in body
        ? (body as { error: { message?: string; git?: GitFailure } }).error
        : undefined;

    throw new ApiError(
      response.status,
      detail?.message ?? `${response.status} from ${path}`,
      detail?.git,
    );
  }

  return body as T;
}

export const api = {
  listRepositories: () =>
    request<{ repos: Repository[] }>('/api/repos').then((payload) => payload.repos),

  /**
   * The git repositories the daemon can find on disk.
   *
   * `dir` is omitted rather than sent empty when nobody has chosen one: the
   * daemon then scans the root it was started with, and says in the answer
   * which directory that was. That is the only way the interface learns it —
   * /api/health does not report the root, and this is the request that has a
   * use for it.
   */
  discoverRepositories: (
    options: {
      dir?: string;
      depth?: number;
      includeWorktrees?: boolean;
      includeSubmodules?: boolean;
      /**
       * Abandons the scan. Walking a home directory is the most expensive
       * thing the daemon does, and the one request the interface still wants
       * has no reason to queue behind the four it does not: the daemon stops
       * its walk as soon as the connection closes.
       */
      signal?: AbortSignal;
    } = {},
  ) => {
    const query = new URLSearchParams();
    if (options.dir !== undefined && options.dir !== '') {
      query.set('dir', options.dir);
    }
    if (options.depth !== undefined) {
      query.set('depth', String(options.depth));
    }
    if (options.includeWorktrees === true) {
      query.set('include_worktrees', 'true');
    }
    if (options.includeSubmodules === true) {
      query.set('include_submodules', 'true');
    }
    const suffix = query.size > 0 ? `?${query.toString()}` : '';
    return request<DiscoverResult>(`/api/repos/discover${suffix}`, { signal: options.signal });
  },

  openRepository: (path: string) =>
    request<Repository>('/api/repos', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),

  /**
   * What cloning would run, without running it.
   *
   * Asked before the confirmation opens: the dialog shows the exact command,
   * with credentials stripped from the URL, and a path the daemon has already
   * checked against the root.
   */
  /**
   * What making an empty repository would run.
   *
   * Sending an empty branch asks the daemon for the machine's own
   * `init.defaultBranch`, which comes back on the plan.
   */
  initPlan: (path: string, branch: string) =>
    request<InitPlan>('/api/repos/init/plan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, branch }),
    }),

  /** Makes an empty repository at path, then opens it. */
  init: (path: string, branch: string) =>
    request<Repository>('/api/repos/init', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, branch }),
    }),

  clonePlan: (url: string, path: string) =>
    request<ClonePlan>('/api/repos/clone/plan', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, path }),
    }),

  /**
   * Clones into path, streaming progress lines, then opens the result.
   *
   * The response is NDJSON on success of the request setup (ADR 0030). A
   * refusal before git starts — path outside the root, destination taken —
   * is still an ordinary JSON error. `onProgress` receives each stderr
   * segment as git writes it.
   */
  clone: async (
    url: string,
    path: string,
    onProgress: (line: string) => void,
  ): Promise<Repository> => {
    const deadline = idleDeadline();

    let response: Response;
    try {
      response = await fetch('/api/repos/clone', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
          Accept: 'application/x-ndjson, application/json',
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ url, path }),
        // Idle rather than elapsed: a clone of a large repository is an
        // hour of legitimate work, and what says a connection is dead is
        // silence. deadline.alive() below pushes it back on every line.
        signal: deadline.signal,
      });
    } catch (cause) {
      if (cause instanceof DOMException && cause.name === 'TimeoutError') {
        throw new ApiError(
          0,
          `git reported nothing for ${waitedFor(NETWORK_IDLE_MS)}, so the transfer was given up on`,
          undefined,
          { cause },
        );
      }
      throw new ApiError(0, `the daemon is not answering on ${window.location.host}`, undefined, {
        cause,
      });
    }

    if (response.status === 401) {
      window.location.reload();
      throw new ApiError(401, 'the session expired');
    }

    const contentType = response.headers.get('Content-Type') ?? '';
    if (!contentType.includes('ndjson')) {
      const body: unknown = await response.json().catch(() => null);
      const detail =
        body !== null && typeof body === 'object' && 'error' in body
          ? (body as { error: { message?: string; git?: GitFailure } }).error
          : undefined;
      throw new ApiError(
        response.status,
        detail?.message ?? `${response.status} from /api/repos/clone`,
        detail?.git,
      );
    }

    if (response.body === null) {
      throw new ApiError(0, 'the clone response had no body to read');
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let repository: Repository | undefined;

    const handleLine = (line: string) => {
      const trimmed = line.trim();
      if (trimmed === '') {
        return;
      }
      const event = JSON.parse(trimmed) as {
        type: string;
        line?: string;
        repository?: Repository;
        error?: { message?: string; git?: GitFailure };
      };
      if (event.type === 'progress' && event.line !== undefined) {
        onProgress(event.line);
        return;
      }
      if (event.type === 'done' && event.repository !== undefined) {
        repository = event.repository;
        return;
      }
      if (event.type === 'error') {
        throw new ApiError(
          response.status,
          event.error?.message ?? 'clone failed',
          event.error?.git,
        );
      }
    };

    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        // Still talking, so it is still alive.
        deadline.alive();
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split('\n');
        buffer = parts.pop() ?? '';
        for (const part of parts) {
          handleLine(part);
        }
      }
    } finally {
      // A timer left armed aborts nothing, but it keeps the page awake for
      // five minutes after the work it was watching finished. handleLine
      // throws on an error event, so this belongs in a finally.
      deadline.settled();
    }
    buffer += decoder.decode();
    if (buffer.trim() !== '') {
      handleLine(buffer);
    }

    if (repository === undefined) {
      throw new ApiError(0, 'clone ended without a repository');
    }
    return repository;
  },

  closeRepository: (id: string) => request<void>(`/api/repos/${id}`, { method: 'DELETE' }),

  /**
   * One page of a repository's history, graph included.
   *
   * The page size is not sent: the daemon owns it and reports it in the
   * answer, so there is one definition of it rather than two that have to
   * agree.
   *
   * The scope always is. It has a default on the daemon's side and the
   * interface never leans on it: the choice is on screen, so the request says
   * which of the three it is rather than letting an omission stand for one.
   */
  commits: (id: string, page: number, scope: HistoryScope, refs: readonly string[] = []) =>
    request<CommitPage>(
      `/api/repos/${id}/commits?page=${page}&scope=${scope}${refQuery(scope, refs)}`,
    ),

  refs: (id: string) => request<RefsPayload>(`/api/repos/${id}/refs`),

  /**
   * Moves HEAD: onto a branch, or onto the commit a reference names.
   *
   * `detach` is not a variation on one operation. `git switch main` and `git
   * switch --detach main` leave the repository in two different places, and
   * only one of them is what checking out main means — so the caller says
   * which it wants rather than letting the daemon guess from the string.
   *
   * Answers with the references and where HEAD now sits, which is what the
   * sidebar redraws from. Everything else the checkout changed — the history,
   * the working directory, the diff on screen — is dropped by the caller
   * rather than sent back here: it would cost a second read of each, and the
   * interface asks for those on its own.
   */
  switchTo: (id: string, ref: string, detach: boolean) =>
    request<RefsPayload>(`/api/repos/${id}/switch`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ref, detach }),
    }),

  /**
   * Makes a local branch, and stands on it when asked to.
   *
   * `start` empty means HEAD, which the daemon leaves to git rather than
   * resolving — so the line in the log panel is the one a person would have
   * typed.
   */
  createBranch: (id: string, name: string, start: string, switchTo: boolean) =>
    request<RefsPayload>(`/api/repos/${id}/branches`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, start, switch: switchTo }),
    }),

  /** Renames a local branch. git carries HEAD across when it is the one on it. */
  renameBranch: (id: string, from: string, to: string) =>
    request<RefsPayload>(`/api/repos/${id}/branches/rename`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from, to }),
    }),

  /**
   * What deleting would run, without running it.
   *
   * Asked before the confirmation opens, because the confirmation's whole
   * content is that command — and a command assembled in the browser is a
   * second definition of the line the daemon will actually hand to git.
   */
  planDeleteBranch: (id: string, name: string, force: boolean) =>
    request<{ command: string }>(`/api/repos/${id}/branches/delete/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, force }),
    }),

  /** Deletes a local branch. `force` is `-D`, and the user was asked first. */
  deleteBranch: (id: string, name: string, force: boolean) =>
    request<RefsPayload>(`/api/repos/${id}/branches/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, force }),
    }),

  /**
   * Records a tag. `target` empty means HEAD, left to git.
   *
   * Annotated (the default) needs a message. Lightweight needs only a name.
   */
  createTag: (id: string, name: string, message: string, target: string, annotated = true) =>
    request<RefsPayload>(`/api/repos/${id}/tags`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, message, target, annotated }),
    }),

  /** What deleting a tag would run, without running it. */
  planDeleteTag: (id: string, name: string) =>
    request<{ command: string }>(`/api/repos/${id}/tags/delete/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),

  /** Deletes a local tag. */
  deleteTag: (id: string, name: string) =>
    request<RefsPayload>(`/api/repos/${id}/tags/delete`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),

  /**
   * What pushing a tag would run, without running it.
   *
   * The remote is required: tags do not follow an upstream, so the destination
   * is a choice the dialog makes rather than a fact the daemon reads.
   */
  planPushTag: (id: string, name: string, remote: string) =>
    request<TagPushPlan>(`/api/repos/${id}/tags/push/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, remote }),
    }),

  /** Sends a local tag to a remote under the same name. */
  pushTag: (id: string, name: string, remote: string) =>
    request<RefsPayload>(`/api/repos/${id}/tags/push`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, remote }),
      timeoutMs: NETWORK_TIMEOUT_MS,
    }),

  /**
   * What merging would run, without running it.
   *
   * Asked before the confirmation opens, because the confirmation's whole
   * content is that command — and a command assembled in the browser is a
   * second definition of the line the daemon will actually hand to git.
   */
  planMerge: (id: string, branch: string, mergeCommit = false) =>
    request<MergePlan>(`/api/repos/${id}/merge/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch, merge_commit: mergeCommit }),
    }),

  /**
   * Brings another local branch into the one HEAD is on.
   *
   * Takes the plan back rather than the branch alone. `into` and `outcome` are
   * the two facts the confirmation showed, and the daemon checks the first
   * against where HEAD actually is and builds the command from the second — so
   * a merge cannot land in a branch the dialog never named, and cannot turn
   * into a merge commit nobody was shown.
   *
   * A conflict is not hidden: git stops, writes the markers and exits
   * non-zero, and that refusal travels whole. Answers with the references,
   * because that is what moved.
   */
  merge: (id: string, { branch, into, outcome }: MergePlan) =>
    request<RefsPayload>(`/api/repos/${id}/merge`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch, into, outcome }),
    }),

  /**
   * What rebasing onto another branch would run, without running it.
   */
  planRebase: (id: string, onto: string) =>
    request<RebasePlan>(`/api/repos/${id}/rebase/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ onto }),
    }),

  /**
   * Replays the current branch onto another.
   *
   * Takes the plan back rather than the branch alone. `from` and `outcome` are
   * the two facts the confirmation showed, and the daemon checks the first
   * against where HEAD actually is and builds the command from the second.
   *
   * A conflict is not hidden: git stops, writes the markers and exits
   * non-zero, and that refusal travels whole. Answers with the references.
   */
  rebase: (id: string, { onto, from, outcome }: RebasePlan) =>
    request<RefsPayload>(`/api/repos/${id}/rebase`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ onto, from, outcome }),
    }),

  /**
   * What cherry-picking a commit onto the current branch would run, without
   * running it.
   *
   * Asked before the confirmation opens. `up-to-date` answers with an empty
   * command — there is no cherry-pick that succeeds as a no-op — and the
   * interface toasts rather than opening a dialog that would have nothing to
   * show.
   */
  planCherryPick: (id: string, commit: string) =>
    request<CherryPickPlan>(`/api/repos/${id}/cherry-pick/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit }),
    }),

  /**
   * Applies one commit onto the branch HEAD is on.
   *
   * Takes the plan back rather than the commit alone. `into` and `outcome` are
   * the two facts the confirmation showed, and the daemon checks the first
   * against where HEAD actually is and builds the command from the second.
   *
   * A conflict is not hidden: git stops, writes the markers and exits
   * non-zero, and that refusal travels whole. Answers with the references.
   */
  cherryPick: (id: string, { commit, into, outcome }: CherryPickPlan) =>
    request<RefsPayload>(`/api/repos/${id}/cherry-pick`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit, into, outcome }),
    }),

  /**
   * What reverting a commit on the current branch would run, without running
   * it.
   *
   * Asked before the confirmation opens. A merge, a root, or a commit not on
   * the branch is refused rather than answered with a command that cannot
   * succeed.
   */
  planRevert: (id: string, commit: string) =>
    request<RevertPlan>(`/api/repos/${id}/revert/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit }),
    }),

  /**
   * Applies the inverse of one commit onto the branch HEAD is on.
   *
   * Takes the plan back rather than the commit alone. `into` and `outcome` are
   * the two facts the confirmation showed, and the daemon checks the first
   * against where HEAD actually is and builds the command from the second.
   *
   * A conflict is not hidden: git stops, writes the markers and exits
   * non-zero, and that refusal travels whole. Answers with the references.
   */
  revert: (id: string, { commit, into, outcome }: RevertPlan) =>
    request<RefsPayload>(`/api/repos/${id}/revert`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit, into, outcome }),
    }),

  /**
   * What resetting the current branch to a commit would run, without running
   * it.
   *
   * The mode is required: soft, mixed and hard are three different promises
   * about the three trees, and a button cannot leave that to git's default.
   * A commit not on the branch is refused rather than answered with a command
   * that would move onto foreign history.
   */
  planReset: (id: string, commit: string, mode: ResetMode) =>
    request<ResetPlan>(`/api/repos/${id}/reset/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit, mode }),
    }),

  /**
   * Moves the branch HEAD is on to the named commit, in the named mode.
   *
   * Takes the plan back rather than the commit alone. `into` and `mode` are
   * the two facts the confirmation showed, and the daemon checks the first
   * against where HEAD actually is and builds the command from the second.
   */
  reset: (id: string, { commit, into, mode }: ResetPlan) =>
    request<RefsPayload>(`/api/repos/${id}/reset`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit, into, mode }),
    }),

  /**
   * What Git LFS can do here, and what it is doing.
   *
   * One request for two facts — the machine's git-lfs and this repository's
   * .gitattributes — because the panel cannot say anything useful about either
   * alone: patterns with no git-lfs is a repository that checks out pointers,
   * and git-lfs with no patterns is a program with nothing to do.
   */
  lfs: (id: string) => request<LFSSupport>(`/api/repos/${id}/lfs`),

  /** What tracking or untracking a pattern would run. */
  planLFS: (id: string, action: 'track' | 'untrack', pattern: string) =>
    request<{ command: string }>(`/api/repos/${id}/lfs/${action}/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pattern }),
    }),

  /**
   * Routes a pattern through LFS, or takes it back out.
   *
   * Answers with the whole state, for the reason every write here does: the
   * panel that ran the command is the panel that shows the result, and a
   * second request to learn what just happened is a window in which the two
   * disagree.
   */
  trackLFS: (id: string, action: 'track' | 'untrack', pattern: string) =>
    request<LFSSupport>(`/api/repos/${id}/lfs/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pattern }),
    }),

  /** The repositories this one pins, in index order. */
  submodules: (id: string) => request<{ submodules: Submodule[] }>(`/api/repos/${id}/submodules`),

  /** What adding one would run, with the URL redacted. */
  planAddSubmodule: (id: string, url: string, path: string) =>
    request<{ command: string }>(`/api/repos/${id}/submodules/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, path }),
    }),

  /** Pins another repository inside this one and clones it. */
  addSubmodule: (id: string, url: string, path: string) =>
    request<{ submodules: Submodule[] }>(`/api/repos/${id}/submodules`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ url, path }),
    }),

  /** Checks out what this repository records. An empty path means all of them. */
  updateSubmodules: (id: string, path: string) =>
    request<{ submodules: Submodule[] }>(`/api/repos/${id}/submodules/update`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),

  /** Copies the URLs from .gitmodules into the local config. */
  syncSubmodules: (id: string, path: string) =>
    request<{ submodules: Submodule[] }>(`/api/repos/${id}/submodules/sync`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path }),
    }),

  /**
   * BOTH commands a removal runs.
   *
   * git has no `submodule remove`: a deinit takes the checkout away and a
   * `git rm` takes the gitlink and the .gitmodules section. The confirmation
   * shows the pair rather than pretending it is one.
   */
  planRemoveSubmodule: (id: string, path: string, force: boolean) =>
    request<{ commands: string[] }>(`/api/repos/${id}/submodules/remove/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, force }),
    }),

  /** Unpins a repository from this one. */
  removeSubmodule: (id: string, path: string, force: boolean) =>
    request<{ submodules: Submodule[] }>(`/api/repos/${id}/submodules/remove`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, force }),
    }),

  /** Every checkout of the repository, the main one first. */
  worktrees: (id: string) => request<{ worktrees: Worktree[] }>(`/api/repos/${id}/worktrees`),

  /** What making another checkout would run. */
  planAddWorktree: (id: string, body: WorktreeRequest) =>
    request<{ command: string; path: string }>(`/api/repos/${id}/worktrees/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),

  /** Makes another checkout of the repository. */
  addWorktree: (id: string, body: WorktreeRequest) =>
    request<{ worktrees: Worktree[] }>(`/api/repos/${id}/worktrees`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),

  /** What removing one would run. */
  planRemoveWorktree: (id: string, path: string, force: boolean) =>
    request<{ command: string }>(`/api/repos/${id}/worktrees/remove/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, force }),
    }),

  /** Deletes a linked checkout. */
  removeWorktree: (id: string, path: string, force: boolean) =>
    request<{ worktrees: Worktree[] }>(`/api/repos/${id}/worktrees/remove`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, force }),
    }),

  /** Forgets the checkouts whose directories are gone. */
  pruneWorktrees: (id: string) =>
    request<{ worktrees: Worktree[] }>(`/api/repos/${id}/worktrees/prune`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    }),

  /**
   * Commits matching a query, in one of four places.
   *
   * A GET so the same question is the same request: the browser's cache and
   * the query key both depend on it.
   */
  search: (
    id: string,
    query: string,
    field: SearchField,
    scope: HistoryScope,
    refs: readonly string[] = [],
  ) =>
    request<SearchResultPayload>(
      `/api/repos/${id}/search?q=${encodeURIComponent(query)}` +
        `&in=${encodeURIComponent(field)}&scope=${encodeURIComponent(scope)}` +
        refQuery(scope, refs),
    ),

  /** Whether the tip can be undone, and a one-line summary when it can. */
  undoStatus: (id: string) => request<UndoStatus>(`/api/repos/${id}/undo`),

  /** What undoing the tip would run. */
  planUndo: (id: string) =>
    request<UndoPlan>(`/api/repos/${id}/undo/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: '{}',
    }),

  /** Carries out the undo plan. */
  undo: (id: string, plan: UndoPlan) =>
    request<RefsPayload>(`/api/repos/${id}/undo`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        kind: plan.kind,
        into: plan.into,
        head: plan.head,
        to: plan.to,
        to_ref: plan.to_ref,
        branch: plan.branch,
        detach: plan.detach,
      }),
    }),

  /**
   * The commits a rebase plan may be written over: everything after the one
   * clicked, oldest first.
   *
   * A range rather than an outcome, because an interactive rebase is a list
   * the user writes and there is nothing to predict until they have. The
   * refusals happen here: a merge inside the range, nothing after the commit
   * at all, a commit the branch never held, or more commits than one list can
   * be read as.
   */
  planInteractiveRebase: (id: string, commit: string) =>
    request<InteractiveRebasePlan>(`/api/repos/${id}/rebase/interactive/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ commit }),
    }),

  /**
   * Rewrites the commits after `base`, following the plan.
   *
   * Object names and verbs, never a todo list: the daemon writes that file,
   * from the subjects it read itself. It also reads the range again and
   * refuses any plan that is not exactly those commits, each once — so a
   * commit that landed on the branch while the dialog was open is a refusal
   * rather than a rewrite of a history nobody was shown.
   */
  interactiveRebase: (id: string, base: string, from: string, steps: RebaseStep[]) =>
    request<InteractiveRebaseResult>(`/api/repos/${id}/rebase/interactive`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ base, from, steps }),
    }),

  /**
   * What the repository is configured to talk to.
   *
   * The URLs come back with any credentials removed — a token pasted into a
   * remote URL is a password, and this list is drawn on a screen. They are for
   * reading, never for handing back to git: the daemon addresses a remote by
   * name.
   */
  remotes: (id: string) =>
    request<{ remotes: Remote[] }>(`/api/repos/${id}/remotes`).then((payload) => payload.remotes),

  /** Records a remote by name and URL. Answers with the remotes list. */
  addRemote: (id: string, name: string, url: string) =>
    request<{ remotes: Remote[] }>(`/api/repos/${id}/remotes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url }),
    }).then((payload) => payload.remotes),

  /** Renames a remote. git moves its tracking branches with it. */
  renameRemote: (id: string, from: string, to: string) =>
    request<{ remotes: Remote[] }>(`/api/repos/${id}/remotes/rename`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from, to }),
    }).then((payload) => payload.remotes),

  /** What removing a remote would run, without running it. */
  planRemoveRemote: (id: string, name: string) =>
    request<{ command: string }>(`/api/repos/${id}/remotes/remove/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }),

  /**
   * Forgets a remote and the remote-tracking branches under it.
   *
   * The confirmation showed the command first — removing origin deletes
   * every origin/… row from the sidebar.
   */
  removeRemote: (id: string, name: string) =>
    request<{ remotes: Remote[] }>(`/api/repos/${id}/remotes/remove`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    }).then((payload) => payload.remotes),

  /**
   * Brings the remote-tracking branches up to date.
   *
   * An empty remote means every one of them, which is what a fetch button with
   * no picker beside it has to mean. Progress rides the response as NDJSON
   * (ADR 0030); the done event carries the references the fetch moved.
   */
  fetchRemote: (id: string, remote: string, onProgress: (line: string) => void = () => undefined) =>
    networkWithProgress<RefsPayload>(`/api/repos/${id}/fetch`, { remote }, onProgress),

  /**
   * Fetches the upstream and integrates it into the current branch.
   *
   * The strategy is required and is not a preference: git decides between a
   * merge and a rebase from configuration, and a button cannot read a setting
   * and still say what it does. Which branch, and which remote, the daemon
   * reads for itself — the browser never assembles a refspec.
   *
   * Progress rides the response as NDJSON, for the same reason fetch's does.
   */
  pull: (
    id: string,
    strategy: PullStrategy,
    onProgress: (line: string) => void = () => undefined,
  ) => networkWithProgress<RefsPayload>(`/api/repos/${id}/pull`, { strategy }, onProgress),

  /**
   * Sends the current branch where it follows, or publishes it.
   *
   * `remote` is used only for the publish — a branch that already follows
   * something goes there whatever is sent, which is the daemon's decision and
   * not this one's. `force` is `--force-with-lease --force-if-includes`, and
   * the user was asked with the exact command first.
   *
   * Progress rides the response as NDJSON, for the same reason fetch's does.
   */
  push: (
    id: string,
    remote: string,
    force: boolean,
    onProgress: (line: string) => void = () => undefined,
    lease?: { local_branch: string; ref: string },
  ) =>
    networkWithProgress<RefsPayload>(
      `/api/repos/${id}/push`,
      {
        remote,
        force,
        ...(lease === undefined ? {} : { local_branch: lease.local_branch, ref: lease.ref }),
      },
      onProgress,
    ),

  /**
   * What pushing would run, without running it.
   *
   * Asked before a confirmation opens, for the reason planDeleteBranch is: the
   * dialog's whole content is that command, and a command assembled in the
   * browser is a second definition of the line the daemon will hand to git.
   * The destination comes back with it, because "publish feature to origin"
   * reads better above a button than a refspec does and both have to be one
   * reading of the repository.
   */
  pushPlan: (id: string, remote: string, force: boolean) =>
    request<PushPlan>(`/api/repos/${id}/push/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ remote, force }),
    }),

  /** Changes where a remote is fetched from. Answers with the remotes list. */
  setRemoteURL: (id: string, name: string, url: string) =>
    request<{ remotes: Remote[] }>(`/api/repos/${id}/remotes/set-url`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url }),
    }).then((payload) => payload.remotes),

  /** What set-url would run, with credentials stripped from the shown line. */
  planSetRemoteURL: (id: string, name: string, url: string) =>
    request<{ command: string }>(`/api/repos/${id}/remotes/set-url/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, url }),
    }),

  /**
   * Records that a local branch follows a remote-tracking one.
   *
   * Distinct from publishing: no push, only the follow. Empty `branch` means
   * the one HEAD is on.
   */
  setUpstream: (id: string, branch: string, remote: string, upstream: string) =>
    request<RefsPayload>(`/api/repos/${id}/upstream`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch, remote, upstream }),
    }),

  planSetUpstream: (id: string, branch: string, remote: string, upstream: string) =>
    request<UpstreamPlan>(`/api/repos/${id}/upstream/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch, remote, upstream }),
    }),

  /** Forgets what a local branch follows. */
  unsetUpstream: (id: string, branch: string) =>
    request<RefsPayload>(`/api/repos/${id}/upstream/unset`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch }),
    }),

  planUnsetUpstream: (id: string, branch: string) =>
    request<UpstreamPlan>(`/api/repos/${id}/upstream/unset/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ branch }),
    }),

  /**
   * Everything the daemon knows about one commit: what it says, what it
   * changed, and where it sits in the walk being drawn.
   *
   * The scope travels with it because the row does. A row is a position in one
   * walk, so the same commit sits elsewhere under the other scope and at no
   * row at all under one that does not reach it — which the daemon answers
   * with a 404 naming the walk, and the panel turns into an offer.
   *
   * Named after the command rather than the noun, because `commit` below is
   * the one that MAKES one. Two calls a letter apart, one of which writes to
   * the repository, is how a refactor ends up committing where it meant to
   * read.
   */
  showCommit: (id: string, sha: string, scope: HistoryScope, refs: readonly string[] = []) =>
    request<LocatedCommit>(
      `/api/repos/${id}/commits/${encodeURIComponent(sha)}?scope=${scope}${refQuery(scope, refs)}`,
    ),

  /**
   * Commits that touched a path, following renames.
   *
   * `revision` empty means HEAD. A full object name starts the walk at that
   * commit — what History from a commit's patch header asks for.
   */
  fileHistory: (id: string, path: string, revision = '') => {
    const query = new URLSearchParams({ path });
    if (revision !== '') {
      query.set('revision', revision);
    }
    return request<FileHistory>(`/api/repos/${id}/files/history?${query}`);
  },

  /**
   * Who last touched each line of a path at a revision.
   *
   * `revision` empty means HEAD. Same shape as file history for the query.
   */
  blame: (id: string, path: string, revision = '') => {
    const query = new URLSearchParams({ path });
    if (revision !== '') {
      query.set('revision', revision);
    }
    return request<Blame>(`/api/repos/${id}/files/blame?${query}`);
  },

  /**
   * Commits that changed one line of a path.
   *
   * `line` is 1-based. `revision` empty means HEAD.
   */
  lineHistory: (id: string, path: string, line: number, revision = '') => {
    const query = new URLSearchParams({ path, line: String(line) });
    if (revision !== '') {
      query.set('revision', revision);
    }
    return request<LineHistory>(`/api/repos/${id}/files/line-history?${query}`);
  },

  /** What differs, and where HEAD stands. */
  status: (id: string) => request<WorkingDirectory>(`/api/repos/${id}/status`),

  /**
   * One path's diff on one side.
   *
   * The side is never guessed here. A file can be staged and unstaged at
   * once, and the two diffs answer different questions; picking one for the
   * caller would show the wrong half of the file half the time.
   */
  diff: (id: string, path: string, side: DiffSide) =>
    request<FileDiff>(`/api/repos/${id}/diff?path=${encodeURIComponent(path)}&side=${side}`),

  /**
   * The four operations, all of the same shape.
   *
   * `lines` narrows one of them to part of a single file, and each answers
   * with the status that followed. Answering with the new status rather than
   * nothing is what keeps the panel from drawing one frame of the state it
   * just left.
   */
  stage: (id: string, paths: string[], lines?: LineSelection) =>
    worktree<WorkingDirectory>(id, 'stage', paths, lines),

  unstage: (id: string, paths: string[], lines?: LineSelection) =>
    worktree<WorkingDirectory>(id, 'unstage', paths, lines),

  discard: (id: string, paths: string[], lines?: LineSelection) =>
    worktree<WorkingDirectory>(id, 'discard', paths, lines),

  /**
   * What a discard would run, without running it.
   *
   * The same body as `discard` above, answered with the commands rather than
   * the status. The confirmation dialog asks for it because it has to show the
   * exact command, and the exact command is the daemon's to know — see
   * DiscardPlan.
   */
  discardPlan: (id: string, paths: string[], lines?: LineSelection) =>
    worktree<DiscardPlan>(id, 'discard/plan', paths, lines),

  /**
   * The message git would open an editor on for this commit.
   *
   * `amend` travels because it changes the answer: git starts an amend from
   * the commit being replaced, and everything else from whatever a stopped
   * merge left behind.
   */
  preparedMessage: (id: string, amend: boolean) =>
    request<PreparedMessage>(`/api/repos/${id}/prepared-message?amend=${String(amend)}`),

  /**
   * One work-tree file, for editing.
   *
   * The file on DISK — not a blob from the index or from a commit. That is the
   * one a merge left conflict markers in, and the only one saving can put
   * back.
   */
  readFile: (id: string, path: string) =>
    request<WorkFile>(`/api/repos/${id}/file?path=${encodeURIComponent(path)}`),

  /**
   * Writes a file back, refusing if it moved since it was read.
   *
   * `base` is the fingerprint the read answered with. Required rather than
   * optional: a save with nothing to compare against cannot tell an edit from
   * an overwrite, and the moment this pane is used most — the middle of a
   * merge — is when a checkout is most likely to have run underneath.
   *
   * Saving does not stage. What is on disk and what is in the index are two
   * different things on every other screen here, and an editor that quietly
   * staged would be deciding which of the two the user meant.
   */
  saveFile: (id: string, path: string, text: string, base: string) =>
    request<SaveResult>(`/api/repos/${id}/file`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, text, base }),
    }),

  /**
   * What finishing or calling off the operation in progress would run.
   *
   * Asked before the confirmation opens, for the reason planDeleteBranch is.
   * The operation comes back with the command because the daemon read it a
   * moment ago and the banner that drew the button may not have: a dialog
   * built from a two-second-old status could promise `git rebase --abort` over
   * a merge.
   */
  planOperation: (id: string, action: OperationAction) =>
    request<OperationPlan>(`/api/repos/${id}/operation/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action }),
    }),

  /**
   * Finishes, or calls off, what the repository is in the middle of.
   *
   * The operation travels with the instruction and is not a hint: the daemon
   * reads the real state and refuses with 409 when the two disagree. That is
   * what keeps an Abort drawn over a rebase from aborting the merge that
   * started while it sat on screen.
   *
   * Answers with the status, because that is what it changed and what the
   * banner is drawn from.
   */
  actOnOperation: (id: string, action: OperationAction, operation: Operation, identity: string) =>
    request<WorkingDirectory>(`/api/repos/${id}/operation`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, operation, identity }),
    }),

  /**
   * Takes one side of a conflict whole, and marks it resolved.
   *
   * Answers with the status, like the four staging operations: resolving moves
   * a file out of the conflicted list and into the staged one, and every other
   * row's counts change with it.
   */
  resolve: (id: string, paths: string[], side: ConflictSide) =>
    request<WorkingDirectory>(`/api/repos/${id}/resolve`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ paths, side }),
    }),

  commit: (id: string, message: string, amend: boolean) =>
    request<CommitResult>(`/api/repos/${id}/commit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message, amend }),
    }),

  /**
   * The stash stack, newest first.
   *
   * Every write below answers with this same list, so nothing that changes it
   * has to be followed by a second request to find out what it did.
   */
  stashes: (id: string) =>
    request<{ stashes: Stash[] }>(`/api/repos/${id}/stashes`).then((payload) => payload.stashes),

  /**
   * What one stash holds.
   *
   * By position, and it is the one stash call that does not also send the
   * object name back. Reading is not writing: if the stack shifted between the
   * click and the answer, the worst outcome is looking at a different stash —
   * and the answer carries the one it read, so the panel titles itself from
   * that rather than from the row that was clicked.
   */
  stash: (id: string, index: number) => request<StashDetail>(`/api/repos/${id}/stashes/${index}`),

  /**
   * What stashing the work tree would save, without saving it.
   *
   * Asked when the dialog opens and again when the untracked box changes,
   * because that box decides both counts AND whether there is anything to save
   * at all: a work tree holding nothing but untracked files is a command that
   * exits 0 having done nothing.
   *
   * The message is not part of it. It is typed, and a plan per keystroke to
   * keep a command on screen current would be a request per keystroke — which
   * is why this family's create dialog shows no command. See StashPushPlan.
   */
  planStash: (id: string, untracked: boolean) =>
    request<StashPushPlan>(`/api/repos/${id}/stash/push/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ untracked }),
    }),

  /** Sets the work tree aside. Answers with the stack it made. */
  stashPush: (id: string, { message, untracked }: { message: string; untracked: boolean }) =>
    request<{ stashes: Stash[] }>(`/api/repos/${id}/stash/push`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message, untracked }),
    }).then((payload) => payload.stashes),

  /**
   * What applying or popping one stash would run, without running it.
   *
   * Both halves of the stash's identity go out: the position git takes, and
   * the object name that position held when the row was drawn.
   */
  planStashApply: (id: string, stash: StashHandle, mode: StashApplyMode) =>
    request<StashApplyPlan>(`/api/repos/${id}/stash/apply/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index: stash.index, sha: stash.sha, mode }),
    }),

  /**
   * Puts one stash back into the work tree.
   *
   * Takes the plan back rather than the stash alone. The daemon reads the
   * position again and refuses one that has come to hold a different stash, so
   * a stack that moved while the confirmation was open cannot restore somebody
   * else's work.
   */
  stashApply: (id: string, { index, sha, mode }: StashApplyPlan) =>
    request<{ stashes: Stash[] }>(`/api/repos/${id}/stash/apply`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index, sha, mode }),
    }).then((payload) => payload.stashes),

  /** What dropping one stash would run, and how much goes with it. */
  planStashDrop: (id: string, stash: StashHandle) =>
    request<StashDropPlan>(`/api/repos/${id}/stash/drop/plan`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index: stash.index, sha: stash.sha }),
    }),

  /** Throws one stash away. Same position-and-name agreement as the apply. */
  stashDrop: (id: string, { index, sha }: StashDropPlan) =>
    request<{ stashes: Stash[] }>(`/api/repos/${id}/stash/drop`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ index, sha }),
    }).then((payload) => payload.stashes),

  /**
   * The git commands the daemon still remembers.
   *
   * The backlog only. What happens next arrives on the event stream, and the
   * two are kept apart so neither has to pretend to be the other.
   */
  log: () =>
    request<{ executions: GitExecution[] }>('/api/log').then((payload) => payload.executions),
};

/**
 * POST that streams NDJSON progress, then a done event carrying T's fields.
 *
 * Validation refusals before git starts are ordinary JSON errors. Once the
 * stream begins, a git failure arrives as an error event on a 200 — the same
 * shape clone uses (ADR 0030).
 */
async function networkWithProgress<T>(
  path: string,
  body: unknown,
  onProgress: (line: string) => void,
): Promise<T> {
  const deadline = idleDeadline();

  let response: Response;
  try {
    response = await fetch(path, {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        Accept: 'application/x-ndjson, application/json',
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
      // Idle rather than elapsed — see the clone above.
      signal: deadline.signal,
    });
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'TimeoutError') {
      throw new ApiError(
        0,
        `git reported nothing for ${waitedFor(NETWORK_IDLE_MS)}, so the transfer was given up on`,
        undefined,
        { cause },
      );
    }
    throw new ApiError(0, `the daemon is not answering on ${window.location.host}`, undefined, {
      cause,
    });
  }

  if (response.status === 401) {
    window.location.reload();
    throw new ApiError(401, 'the session expired');
  }

  const contentType = response.headers.get('Content-Type') ?? '';
  if (!contentType.includes('ndjson')) {
    const parsed: unknown = await response.json().catch(() => null);
    const detail =
      parsed !== null && typeof parsed === 'object' && 'error' in parsed
        ? (parsed as { error: { message?: string; git?: GitFailure } }).error
        : undefined;
    throw new ApiError(
      response.status,
      detail?.message ?? `${response.status} from ${path}`,
      detail?.git,
    );
  }

  if (response.body === null) {
    throw new ApiError(0, `${path} response had no body to read`);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let result: T | undefined;

  const handleLine = (line: string) => {
    const trimmed = line.trim();
    if (trimmed === '') {
      return;
    }
    const event = JSON.parse(trimmed) as {
      type: string;
      line?: string;
      error?: { message?: string; git?: GitFailure };
    } & Partial<T>;
    if (event.type === 'progress' && event.line !== undefined) {
      onProgress(event.line);
      return;
    }
    if (event.type === 'done') {
      const { type, ...payload } = event;
      void type;
      result = payload as T;
      return;
    }
    if (event.type === 'error') {
      throw new ApiError(
        response.status,
        event.error?.message ?? `${path} failed`,
        event.error?.git,
      );
    }
  };

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      // Still talking, so it is still alive.
      deadline.alive();
      buffer += decoder.decode(value, { stream: true });
      const parts = buffer.split('\n');
      buffer = parts.pop() ?? '';
      for (const part of parts) {
        handleLine(part);
      }
    }
  } finally {
    // See the clone above.
    deadline.settled();
  }
  buffer += decoder.decode();
  if (buffer.trim() !== '') {
    handleLine(buffer);
  }

  if (result === undefined) {
    throw new ApiError(0, `${path} ended without a result`);
  }
  return result;
}

function worktree<T>(
  id: string,
  operation: 'stage' | 'unstage' | 'discard' | 'discard/plan',
  paths: string[],
  lines?: LineSelection,
): Promise<T> {
  return request<T>(`/api/repos/${id}/${operation}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    // `lines` is omitted rather than sent as null: the daemon refuses fields
    // it does not know, and null is a field.
    body: JSON.stringify(lines === undefined ? { paths } : { paths, lines }),
  });
}
