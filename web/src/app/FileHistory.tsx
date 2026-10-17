import { useQuery, type UseQueryResult } from '@tanstack/react-query';

import { api } from '../api/client';
import type { Commit } from '../api/types';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { QueryErrorState } from '../components/PanelState';
import { Spinner } from '../components/Spinner';
import { refusalHeading } from '../lib/errorDisplay';
import { formatExactTime, formatRelativeTime, shortenSha } from '../lib/format';

/**
 * Commits that touched one path, newest first, following renames.
 *
 * Opens from a file header on a commit's patch. A row click hands the SHA to
 * the commit panel that already exists — this list does not redraw a patch of
 * its own.
 */

export function fileHistoryQuery(repositoryId: string, path: string, revision: string) {
  return {
    queryKey: ['file-history', repositoryId, path, revision],
    queryFn: () => api.fileHistory(repositoryId, path, revision),
  };
}

export function FileHistoryPanel({
  repositoryId,
  path,
  revision,
  onClose,
  onSelectCommit,
}: {
  repositoryId: string;
  path: string;
  /** Empty means HEAD — the walk starts where the working tree does. */
  revision: string;
  onClose: () => void;
  onSelectCommit: (sha: string) => void;
}) {
  const history = useQuery(fileHistoryQuery(repositoryId, path, revision));

  // A path's history is walked with `git log --follow`, which is exactly the
  // command a rebase in another terminal interrupts.
  return (
    <HistoryListPanel
      title={`History — ${path}`}
      closeLabel="Close the file history"
      pendingLabel={`Reading history of ${path}`}
      errorTitle={`Could not read history of ${path}`}
      emptyTitle="No commits touch this path"
      emptyDescription="Nothing in this walk changed the file — it may be untracked, or the revision does not reach it."
      limitNote={(limit) => `Showing the newest ${limit} commits that touched this path.`}
      history={history}
      onClose={onClose}
      onSelectCommit={onSelectCommit}
    />
  );
}

/**
 * The loading, failure and list frame shared by a path's history and a line's.
 *
 * The two lists differ in the question they asked and in the sentence each
 * state speaks. What a row looks like, and how a failure is retried, do not.
 */
export function HistoryListPanel({
  title,
  closeLabel,
  pendingLabel,
  errorTitle,
  emptyTitle,
  emptyDescription,
  limitNote,
  history,
  onClose,
  onSelectCommit,
}: {
  title: string;
  closeLabel: string;
  pendingLabel: string;
  errorTitle: string;
  emptyTitle: string;
  emptyDescription: string;
  limitNote: (limit: number) => string;
  history: UseQueryResult<{ commits: Commit[]; limit: number }>;
  onClose: () => void;
  onSelectCommit: (sha: string) => void;
}) {
  return (
    <Panel
      title={title}
      className="h-full min-h-0"
      flush
      actions={<CloseButton label={closeLabel} onClose={onClose} />}
    >
      {history.isPending && (
        <div className="flex h-full items-center justify-center">
          <Spinner label={pendingLabel} />
        </div>
      )}

      {history.error !== null && (
        <QueryErrorState
          title={refusalHeading(history.error) ?? errorTitle}
          error={history.error}
          retry={history}
        />
      )}

      {history.data !== undefined && history.data.commits.length === 0 && (
        <EmptyState title={emptyTitle} description={emptyDescription} />
      )}

      {history.data !== undefined && history.data.commits.length > 0 && (
        <ul className="h-full overflow-auto">
          {history.data.commits.map((commit) => (
            <HistoryRow
              key={commit.sha}
              commit={commit}
              onSelect={() => onSelectCommit(commit.sha)}
            />
          ))}
          {history.data.commits.length >= history.data.limit && (
            <li className="px-3 py-2 text-2xs text-ink-subtle">{limitNote(history.data.limit)}</li>
          )}
        </ul>
      )}
    </Panel>
  );
}

/**
 * One commit in a path's or a line's history: subject, then the name, author
 * and age under it.
 *
 * Exported because the line-history panel draws the same row from the same
 * Commit — the two lists differ in the question they asked the daemon, not in
 * what a row of the answer looks like.
 */
export function HistoryRow({ commit, onSelect }: { commit: Commit; onSelect: () => void }) {
  const when = new Date(commit.date);

  return (
    <li className="border-b border-line last:border-b-0">
      <button
        type="button"
        onClick={onSelect}
        className="flex w-full flex-col gap-0.5 px-3 py-2 text-left transition-colors transition-instant hover:bg-hover focus-visible:focus-ring"
      >
        <span className="truncate text-sm text-ink">{commit.subject}</span>
        <span className="flex flex-wrap gap-x-2 font-mono text-2xs text-ink-subtle">
          <span title={commit.sha}>{shortenSha(commit.sha)}</span>
          <span>{commit.author}</span>
          {/* The clock, on the one channel this row has room for. The visible
              form is relative and past a week it is a bare date, so a list of
              commits made on one afternoon reads as one day and nothing
              orders them inside it — and this list, unlike the history, is
              short enough that the missing hour is the whole question a
              reader brought to it. */}
          {/* oxlint-disable-next-line react/purity -- the clock, read at render on purpose: see formatRelativeTime */}
          <span title={formatExactTime(when)}>{formatRelativeTime(when, new Date())}</span>
        </span>
      </button>
    </li>
  );
}
