import { useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { QueryErrorState } from '../components/PanelState';
import { Spinner } from '../components/Spinner';
import { refusalHeading } from '../lib/errorDisplay';
import { HistoryRow } from './FileHistory';

/**
 * Commits that changed one line of one path, newest first.
 *
 * Opens from a line number on the blame panel. A row click hands the SHA to
 * the commit panel that already exists.
 */

export function lineHistoryQuery(
  repositoryId: string,
  path: string,
  line: number,
  revision: string,
) {
  return {
    queryKey: ['line-history', repositoryId, path, line, revision],
    queryFn: () => api.lineHistory(repositoryId, path, line, revision),
  };
}

export function LineHistoryPanel({
  repositoryId,
  path,
  line,
  revision,
  onClose,
  onSelectCommit,
}: {
  repositoryId: string;
  path: string;
  line: number;
  revision: string;
  onClose: () => void;
  onSelectCommit: (sha: string) => void;
}) {
  const history = useQuery(lineHistoryQuery(repositoryId, path, line, revision));

  return (
    <Panel
      title={`History — ${path}:${line}`}
      className="h-full min-h-0"
      flush
      actions={<CloseButton label="Close the line history" onClose={onClose} />}
    >
      {history.isPending && (
        <div className="flex h-full items-center justify-center">
          <Spinner label={`Reading history of ${path} line ${line}`} />
        </div>
      )}

      {/* The shared failure state, and the way out with it. `git log -L` is
          the most easily refused read in the application — a line number past
          the end of the file at that revision answers with git's own error —
          so this is the panel most likely to be looking at one. */}
      {history.error !== null && (
        <QueryErrorState
          title={refusalHeading(history.error) ?? `Could not read history of ${path}:${line}`}
          error={history.error}
          retry={history}
        />
      )}

      {history.data !== undefined && history.data.commits.length === 0 && (
        <EmptyState
          title="No commits changed this line"
          description="Nothing in this walk touched that line — it may be past the end of the file at this revision."
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
              Showing the newest {history.data.limit} commits that changed this line.
            </li>
          )}
        </ul>
      )}
    </Panel>
  );
}
