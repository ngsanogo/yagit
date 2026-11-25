import type { ReflogEntry } from '../api/types';
import { Button } from '../components/Button';
import { HistoryIcon } from '../components/Icons';
import { Panel } from '../components/Panel';
import { QueryErrorState, type RetryableQuery } from '../components/PanelState';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { formatExactTime, formatRelativeTime, shortenSha } from '../lib/format';
import { REVEALED_ON_ATTENTION } from '../lib/reveal';

/**
 * HEAD's reflog beside the other things the repository holds.
 *
 * Newest first. Restoring is a plan of its own — soft, mixed or hard — so the
 * row only opens that question; it does not move HEAD on the click.
 */
export function ReflogPanel({
  entries,
  loading,
  error,
  retry,
  onRestore,
  restoring,
}: {
  entries: ReflogEntry[] | undefined;
  loading: boolean;
  error?: Error;
  retry?: RetryableQuery;
  onRestore?: (entry: ReflogEntry) => void;
  restoring?: string;
}) {
  return (
    <Panel
      icon={<HistoryIcon />}
      title={`Reflog${entries === undefined || entries.length === 0 ? '' : ` — ${entries.length}`}`}
      className="max-h-56 shrink-0"
      flush
    >
      <div className="h-full overflow-auto">
        {loading && (
          <div className="grid place-items-center p-4">
            <Spinner label="Reading the reflog" />
          </div>
        )}

        {error !== undefined && entries === undefined && (
          <QueryErrorState title="Could not read the reflog" error={error} compact retry={retry} />
        )}

        {!loading && entries !== undefined && entries.length === 0 && (
          <p className="px-3 py-4 text-center text-sm text-ink-muted">
            HEAD has not moved yet, so there is nothing in the reflog.
          </p>
        )}

        {entries !== undefined && entries.length > 0 && (
          <ul className="divide-y divide-line">
            {entries.map((entry) => {
              const when = new Date(entry.date);
              // The clock, read at render on purpose: see formatRelativeTime.
              const now = new Date();
              return (
                <li
                  key={`${entry.selector}:${entry.sha}`}
                  className="group/row flex items-start gap-2 px-3 py-2 text-sm"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                      <span className="font-mono text-2xs text-ink-subtle">{entry.selector}</span>
                      <span className="font-mono text-2xs text-ink-muted">
                        {shortenSha(entry.sha)}
                      </span>
                      <span className="text-2xs text-ink-subtle" title={formatExactTime(when)}>
                        {formatRelativeTime(when, now)}
                      </span>
                    </div>
                    <p className="truncate text-ink" title={entry.subject}>
                      {entry.subject}
                    </p>
                  </div>
                  {onRestore !== undefined && (
                    // The action, not the row, waits for attention: the row
                    // is the reading and has to be on screen without a hover.
                    <span className={cx('shrink-0', REVEALED_ON_ATTENTION)}>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={restoring !== undefined}
                        loading={restoring === entry.selector}
                        onClick={() => onRestore(entry)}
                        aria-label={`Restore to ${entry.selector}`}
                      >
                        Restore…
                      </Button>
                    </span>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </Panel>
  );
}
