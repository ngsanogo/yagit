import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type {
  ConflictSide,
  DiffSide,
  FileStatus,
  LineSelection,
  RepositoryState,
  WorkingDirectory,
} from '../api/types';
import { countDrawnLines } from './DiffView';
import { overCap } from './drawnLines';

/**
 * Reading and changing the working directory.
 *
 * One query for the status, one per open diff, and four mutations that all
 * answer with the status that followed — so a stage never leaves the panel
 * drawing the state it just left.
 */

/**
 * How often the status is re-read while the window has focus.
 *
 * The event stream covers everything that touches the git directory: a commit
 * in the user's terminal, a checkout, a rebase. It does NOT cover a file being
 * saved in an editor, because yagit watches a bounded set of paths and never
 * the work tree (ADR 0008) — and watching a work tree recursively is how a
 * client ends up holding a descriptor per node_modules entry.
 *
 * So the work tree is polled and the repository is watched. Two seconds is
 * below the threshold where a saved file feels unnoticed, and `git status` on
 * a warm repository costs a few milliseconds. Polling stops when the window
 * loses focus: a laptop with yagit open in a background tab must not keep a
 * git process busy all day.
 */
const STATUS_POLL_MS = 2_000;

/**
 * `enabled` is false for a bare repository, which has no work tree at all: the
 * daemon refuses the question with a 409 and logs every one of them, and a
 * poll would ask it every two seconds for as long as the tab is open. Not
 * asking is the difference between a screen that offers what applies and one
 * that shouts into a log nobody reads.
 */
export function useWorkingDirectory(repositoryId: string, enabled: boolean) {
  return useQuery({
    queryKey: ['status', repositoryId],
    queryFn: () => api.status(repositoryId),
    enabled,
    refetchInterval: STATUS_POLL_MS,
    refetchIntervalInBackground: false,
  });
}

/**
 * One file's diff on one side.
 *
 * Kept fresh by the same two mechanisms as the status: the event stream
 * invalidates it when the repository moves, and it is refetched alongside the
 * poll. A diff drawn against a file that has changed is not merely stale — it
 * is a line selection the daemon will refuse, which is the right refusal but a
 * poor way to find out.
 *
 * The poll stops at the point the view stops drawing. Over the cap the
 * pane shows the first two thousand lines and says so, so a re-read buys
 * nothing that reaches the screen while costing a diff of up to the daemon's
 * ten-megabyte cap, transferred and parsed on the main thread, every two
 * seconds for as long as the row stays selected — the tab freeze the cap
 * exists to prevent, arriving on a timer instead of on a click. What is lost
 * is that the drawn lines can go stale, and staging one of them is then
 * refused with a 409 naming the file and saying to read it again. A refusal
 * anyone can act on beats a tab nobody can use.
 *
 * The first read is still the whole diff, and nothing here can change that:
 * GET /diff takes a path and a side and has no way to be asked for a range.
 * Bounding what crosses the wire means the daemon growing that parameter, and
 * the parser producing the hunks that fall inside it — which is where the next
 * reader of this comment should start.
 */
export function useFileDiff(
  repositoryId: string,
  path: string | undefined,
  side: DiffSide,
  /**
   * False while nothing is showing the diff.
   *
   * A conflicted path has no diff to ask for — git answers one with a combined
   * diff, which the daemon refuses by name — so the pane shows the editor
   * instead. Asking anyway would spend a request every two seconds on a 409
   * nothing renders, in the daemon's log, on the user's own screen in the
   * console, for as long as the row stays open.
   */
  enabled = true,
) {
  return useQuery({
    queryKey: ['diff', repositoryId, path, side],
    queryFn: () => api.diff(repositoryId, path ?? '', side),
    enabled: enabled && path !== undefined,
    refetchInterval: (query) => {
      if (!enabled) {
        return false;
      }
      const drawn = query.state.data;
      if (drawn !== undefined && overCap(countDrawnLines(drawn))) {
        return false;
      }
      return STATUS_POLL_MS;
    },
    refetchIntervalInBackground: false,
  });
}

/**
 * One work-tree file, for the editing pane.
 *
 * Never polled, unlike the status and the diff beside it, and that is the
 * whole difference: those two are things the interface SHOWS, and this is a
 * thing the user is TYPING IN. A re-read on a timer would replace a
 * half-written resolution with whatever is on disk, every two seconds, which
 * is the one behaviour an editor may not have.
 *
 * Staleness is caught at the other end instead. The save carries the
 * fingerprint of the content it started from and the daemon refuses it if the
 * file has moved — so the cost of not polling is a refusal at the moment of
 * saving, naming the file and saying to read it again, rather than work
 * silently overwritten while it was being done.
 */
export function useWorkFile(repositoryId: string, path: string | undefined) {
  return useQuery({
    queryKey: ['file', repositoryId, path],
    queryFn: () => api.readFile(repositoryId, path ?? ''),
    enabled: path !== undefined,
    // The answer is only ever replaced deliberately: by a save, or by the
    // Reload the refusal offers.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
}

/**
 * The message git would start this commit from.
 *
 * Keyed by everything that changes the answer, and the step is one of them. A
 * rebase stops on each commit it cannot replay and rewrites MERGE_MSG every
 * time; `operation` is 'rebase' for the whole of that, so a key without the
 * step reuses the first stop's message for the third stop's commit — offering
 * "fix the parser" for a commit that is not it, from a cache that never
 * expires.
 *
 * Nothing that moves on a timer is in the key. This is what goes in the
 * message box, and a box that reloads under somebody mid-sentence is worse
 * than one that never offers anything.
 */
export function usePreparedMessage(repositoryId: string, amend: boolean, state: RepositoryState) {
  return useQuery({
    queryKey: ['prepared-message', repositoryId, amend, state.operation, state.step ?? 0],
    queryFn: () => api.preparedMessage(repositoryId, amend),
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    // A repository with no commit yet has no HEAD to amend, and git says so.
    // Asking three times over does not change the answer.
    retry: false,
  });
}

/** What a working-directory operation is asked to act on. */
export interface WorktreeRequest {
  paths: string[];
  lines?: LineSelection;
}

/**
 * The four operations, sharing one cache update.
 *
 * Each answers with the status that followed, and that answer is written
 * straight into the query cache rather than triggering a refetch: the daemon
 * has just read the status, and asking it again for the same answer is a
 * second `git status` per click.
 *
 * The diffs are invalidated rather than written, because the operation
 * changed at least one of them and the daemon did not send it back. Anything
 * else would leave a diff on screen describing a file as it was before the
 * click that changed it.
 */
export function useWorktreeOperations(repositoryId: string) {
  const queryClient = useQueryClient();

  const settle = async (status: WorkingDirectory) => {
    // The status is polled, so there is usually a `git status` already in
    // flight. Writing the cache does not stop it, and when it lands TanStack
    // writes it over the top — putting a discarded file back in the list for
    // up to two seconds after the user was told it was gone.
    await queryClient.cancelQueries({ queryKey: ['status', repositoryId] });
    queryClient.setQueryData(['status', repositoryId], status);
    void queryClient.invalidateQueries({ queryKey: ['diff', repositoryId] });
    // The index moved, so `git log` did not — but a commit does move it, and
    // discarding can leave a repository whose refs are unchanged. Invalidating
    // the history here would refetch it on every stage, for nothing: the
    // event stream is what announces a moved ref, and it is already watching.
  };

  const stage = useMutation({
    mutationFn: ({ paths, lines }: WorktreeRequest) => api.stage(repositoryId, paths, lines),
    onSuccess: settle,
  });

  const unstage = useMutation({
    mutationFn: ({ paths, lines }: WorktreeRequest) => api.unstage(repositoryId, paths, lines),
    onSuccess: settle,
  });

  const discard = useMutation({
    mutationFn: ({ paths, lines }: WorktreeRequest) => api.discard(repositoryId, paths, lines),
    onSuccess: settle,
  });

  // Changes nothing, which is why it settles nothing: it asks the daemon what
  // the discard beside it would run, so the confirmation can show it. A
  // mutation rather than a query because it is asked once, when the question
  // is put, and never kept fresh — the answer is only true for the selection
  // that produced it.
  const discardPlan = useMutation({
    mutationFn: ({ paths, lines }: WorktreeRequest) => api.discardPlan(repositoryId, paths, lines),
  });

  /**
   * Writes a file back, and settles both halves of what it changed.
   *
   * The saved file goes into its own cache entry — the fingerprint has moved,
   * and the next save of the same buffer is refused as stale without it — and
   * the status that came back is written beside it, because saving a file is
   * what turns a clean row into a modified one.
   */
  const save = useMutation({
    mutationFn: ({ path, text, base }: { path: string; text: string; base: string }) =>
      api.saveFile(repositoryId, path, text, base),
    onSuccess: async (result) => {
      queryClient.setQueryData(['file', repositoryId, result.file.path], result.file);
      await settle(result.status);
    },
  });

  /**
   * Takes one side of a conflict whole.
   *
   * The file on disk is replaced, so its cache entry is dropped rather than
   * updated: what the editor was holding described a file with markers in it,
   * and the daemon did not send back what replaced it.
   */
  const resolve = useMutation({
    mutationFn: ({ paths, side }: { paths: string[]; side: ConflictSide }) =>
      api.resolve(repositoryId, paths, side),
    onSuccess: async (status, { paths }) => {
      for (const path of paths) {
        await queryClient.invalidateQueries({ queryKey: ['file', repositoryId, path] });
      }
      await settle(status);
    },
  });

  const commit = useMutation({
    mutationFn: ({ message, amend }: { message: string; amend: boolean }) =>
      api.commit(repositoryId, message, amend),
    onSuccess: () => {
      // A commit moves HEAD, so the history and the refs are both stale, and
      // the working directory is emptier than it was. The status is refetched
      // rather than assumed: a hook can change what a commit actually
      // recorded.
      void queryClient.invalidateQueries({ queryKey: ['status', repositoryId] });
      void queryClient.invalidateQueries({ queryKey: ['commits', repositoryId] });
      void queryClient.invalidateQueries({ queryKey: ['refs', repositoryId] });
      // The message git would start the NEXT commit from moved too, and its
      // query never goes stale on its own. Without this an amend refills the
      // box with the message of the commit it just replaced: the amend box
      // stays ticked, the draft is cleared, and the cached answer comes back —
      // so a second amend records the old wording over the new one, silently.
      void queryClient.invalidateQueries({ queryKey: ['prepared-message', repositoryId] });
    },
  });

  return { stage, unstage, discard, discardPlan, save, resolve, commit };
}

/**
 * Which side of a file the interface should show for a given row.
 *
 * A file can be staged and unstaged at once, so the row the user clicked is
 * what decides — not the file. This is the one place that mapping is made.
 */
export function sideOf(file: FileStatus, row: 'staged' | 'unstaged'): DiffSide {
  if (row === 'staged') {
    return 'staged';
  }
  return file.kind === 'untracked' ? 'untracked' : 'unstaged';
}

/**
 * Whether the file is still on disk, and so whether there is anything to open
 * in an editor.
 *
 * A deletion is a change like any other and sits in the list like any other,
 * which is exactly why this has to be asked: offering to edit a file somebody
 * deleted produces a 404 with no useful advice in it, in answer to a button
 * that should not have been there.
 *
 * The work-tree code is the authority, because the work tree is where the
 * file would be. `D` in it says the deletion is not staged and the file is
 * gone; `.` beside a staged `D` says the index and the disk agree that it is
 * gone. Anything else — including a file deleted and then written again, which
 * git reports as `DM`— means there is something to open.
 */
export function isOnDisk(file: FileStatus): boolean {
  if (file.kind === 'unmerged') {
    // The codes of an unmerged entry are not index-and-work-tree: they are us
    // and them, so neither of them describes the disk and the rule below reads
    // the wrong thing entirely. What git actually leaves in the work tree is a
    // file for every conflict but one — the merged text with markers for `UU`
    // and `AA`, the surviving side for `AU`, `UA`, `DU` and `UD`. Only `DD`,
    // where both sides deleted it, has nothing there to open.
    return !(file.index === 'D' && file.work_tree === 'D');
  }
  if (file.work_tree === 'D') {
    return false;
  }
  return !(file.index === 'D' && file.work_tree === '.');
}
