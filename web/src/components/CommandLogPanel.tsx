import type { GitExecution } from '../api/types';
import { cx } from '../lib/cx';
import { Badge } from './Badge';
import { EmptyState } from './EmptyState';
import { Panel } from './Panel';

interface CommandLogPanelProps {
  executions: readonly GitExecution[];
  className?: string;
}

/**
 * The log of the git commands that actually ran.
 *
 * This panel is not a debugging tool: it is a promise the project makes. The
 * user has to be able to learn git by watching yagit work, so nothing is
 * hidden — not the read-only commands, not the failures, not git's raw
 * stderr.
 *
 * Titled "Command log" and not "Git log", which is what it used to say. The
 * promise above is the reason: somebody learning git from this panel would
 * have learned that `git log` names the transcript of commands rather than the
 * command that prints history — and this application's central screen IS that
 * history, two panels away. Every other name the project has given this thing
 * already said command log.
 */
export function CommandLogPanel({ executions, className }: CommandLogPanelProps) {
  return (
    <Panel title="Command log" className={className} flush>
      {executions.length === 0 ? (
        <EmptyState
          title="No commands yet"
          description="Every git command yagit runs will appear here, exactly as it was executed."
        />
      ) : (
        <ol className="divide-y divide-line">
          {executions.map((execution) => (
            <ExecutionRow key={execution.id} execution={execution} />
          ))}
        </ol>
      )}
    </Panel>
  );
}

function ExecutionRow({ execution }: { execution: GitExecution }) {
  const failed = execution.exit_code !== 0;

  return (
    <li className="flex flex-col gap-1 px-3 py-2 transition-colors transition-instant hover:bg-hover">
      <div className="flex items-baseline gap-2">
        <span aria-hidden="true" className="font-mono text-xs text-ink-subtle">
          $
        </span>
        <code
          className={cx(
            'min-w-0 flex-1 font-mono text-xs break-all',
            failed ? 'text-danger' : 'text-ink',
          )}
        >
          {execution.command}
        </code>
        <span className="shrink-0 text-2xs text-ink-subtle tabular">
          {execution.duration_ms} ms
        </span>
        {failed && <Badge tone="danger">exit {execution.exit_code}</Badge>}
      </div>

      {/*
        Shown whenever git wrote something, not only when it failed. git exits
        0 and still warns — a broken ref, a detached HEAD, an ambiguous name —
        and hiding those behind a non-zero exit code is exactly the silent
        error this panel exists to prevent. The color says whether it was
        fatal; the presence says git spoke.
      */}
      {execution.stderr.trim() !== '' && (
        <pre
          className={cx(
            'ml-4 overflow-x-auto font-mono text-2xs whitespace-pre-wrap',
            failed ? 'text-danger' : 'text-ink-muted',
          )}
        >
          {execution.stderr}
        </pre>
      )}
    </li>
  );
}
