import { useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import type { BlameLine } from '../api/types';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { Tooltip } from '../components/Tooltip';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { formatRelativeTime, shortenSha } from '../lib/format';
import { MAX_DRAWN_LINES } from './DiffView';

/**
 * Who last touched each line of a path at a revision.
 *
 * Opens from a file header on a commit's patch. A line's commit is a button
 * into the commit panel; the line number opens that line's history.
 */

export function blameQuery(repositoryId: string, path: string, revision: string) {
  return {
    queryKey: ['blame', repositoryId, path, revision],
    queryFn: () => api.blame(repositoryId, path, revision),
  };
}

export function BlamePanel({
  repositoryId,
  path,
  revision,
  onClose,
  onSelectCommit,
  onLineHistory,
}: {
  repositoryId: string;
  path: string;
  revision: string;
  onClose: () => void;
  onSelectCommit: (sha: string) => void;
  onLineHistory: (line: number) => void;
}) {
  const blame = useQuery(blameQuery(repositoryId, path, revision));

  return (
    <Panel
      title={`Blame — ${path}`}
      className="h-full min-h-0"
      flush
      actions={<CloseButton label="Close the blame" onClose={onClose} />}
    >
      {blame.isPending && (
        <div className="flex h-full items-center justify-center">
          <Spinner label={`Reading blame of ${path}`} />
        </div>
      )}

      {blame.error !== null && (
        <EmptyState
          title={refusalHeading(blame.error) ?? `Could not blame ${path}`}
          description=""
          detail={errorDescription(blame.error)}
          className="py-8"
        />
      )}

      {blame.data !== undefined && blame.data.lines.length === 0 && (
        <EmptyState
          title="This file is empty"
          description="There are no lines to attribute."
          className="py-8"
        />
      )}

      {blame.data !== undefined && blame.data.lines.length > 0 && (
        <div className="h-full overflow-auto font-mono text-xs">
          <table className="w-full border-collapse">
            <tbody>
              {blame.data.lines.slice(0, MAX_DRAWN_LINES).map((line) => (
                <BlameRow
                  key={line.number}
                  line={line}
                  onSelectCommit={onSelectCommit}
                  onLineHistory={onLineHistory}
                />
              ))}
            </tbody>
          </table>
          {blame.data.lines.length > MAX_DRAWN_LINES && (
            <p className="border-t border-line px-3 py-2 text-2xs text-ink-subtle">
              Showing the first {MAX_DRAWN_LINES} of {blame.data.lines.length} lines.
            </p>
          )}
        </div>
      )}
    </Panel>
  );
}

function BlameRow({
  line,
  onSelectCommit,
  onLineHistory,
}: {
  line: BlameLine;
  onSelectCommit: (sha: string) => void;
  onLineHistory: (line: number) => void;
}) {
  const when = formatRelativeTime(new Date(line.date), new Date());
  const label = `${line.author}, ${when} — ${line.subject}`;

  return (
    <tr className="hover:bg-hover">
      <td className="w-0 whitespace-nowrap border-r border-line px-2 py-0.5 text-right align-top">
        <Tooltip label={`History of line ${line.number}`}>
          <button
            type="button"
            onClick={() => onLineHistory(line.number)}
            aria-label={`History of line ${line.number}`}
            className="rounded-sm text-2xs text-ink-subtle outline-none hover:text-ink focus-visible:focus-ring"
          >
            {line.number}
          </button>
        </Tooltip>
      </td>
      <td className="w-0 whitespace-nowrap border-r border-line px-2 py-0.5 align-top">
        <Tooltip label={label}>
          <button
            type="button"
            onClick={() => onSelectCommit(line.sha)}
            className="rounded-sm text-2xs text-ink-muted outline-none hover:text-ink focus-visible:focus-ring"
          >
            {shortenSha(line.sha)}
          </button>
        </Tooltip>
      </td>
      <td className="w-0 max-w-[8rem] truncate border-r border-line px-2 py-0.5 text-2xs text-ink-subtle align-top">
        {line.author}
      </td>
      <td className="whitespace-pre-wrap px-2 py-0.5 text-ink align-top">{line.text}</td>
    </tr>
  );
}
