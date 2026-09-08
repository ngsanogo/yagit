import { useQuery } from '@tanstack/react-query';

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

  return (
    <Panel
      title={`History — ${path}`}
      className="h-full min-h-0"
      flush
      actions={<CloseButton label="Close the file history" onClose={onClose} />}
    >
      {history.isPending && (
        <div className="flex h-full items-center justify-center">
          <Spinner label={`Reading history of ${path}`} />
        </div>
      )}

      {/* The shared failure state, so this read looks like every other read
          that failed — and so it gets the Retry the hand-rolled version had no
          room for. A path's history is walked with `git log --follow`, which is
          exactly the command a rebase in another terminal interrupts. */}
      {history.error !== null && (
        <QueryErrorState
          title={refusalHeading(history.error) ?? `Could not read history of ${path}`}
          error={history.error}
          retry={history}
        />
      )}

      {history.data !== undefined && history.data.commits.length === 0 && (
        <EmptyState
          title="No commits touch this path"
          description="Nothing in this walk changed the file — it may be untracked, or the revision does not reach it."
        />
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
            <li className="px-3 py-2 text-2xs text-ink-subtle">
              Showing the newest {history.data.limit} commits that touched this path.
            </li>
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
          <span title={formatExactTime(when)}>{formatRelativeTime(when, new Date())}</span>
        </span>
      </button>
    </li>
  );
}
