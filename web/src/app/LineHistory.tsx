import { useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import { HistoryListPanel } from './FileHistory';

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

  // `git log -L` is the most easily refused read in the application — a line
  // number past the end of the file at that revision answers with git's own
  // error.
  return (
    <HistoryListPanel
      title={`History — ${path}:${line}`}
      closeLabel="Close the line history"
      pendingLabel={`Reading history of ${path} line ${line}`}
      errorTitle={`Could not read history of ${path}:${line}`}
      emptyTitle="No commits changed this line"
      emptyDescription="Nothing in this walk touched that line — it may be past the end of the file at this revision."
      limitNote={(limit) => `Showing the newest ${limit} commits that changed this line.`}
      history={history}
      onClose={onClose}
      onSelectCommit={onSelectCommit}
    />
  );
}
