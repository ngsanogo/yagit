import type { GitExecution } from '../api/types';
import { cx } from '../lib/cx';
import { Badge } from './Badge';
import { EmptyState } from './EmptyState';
import { TerminalIcon } from './Icons';
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
 *
 * Newest first, and the list scrolls. It was oldest first in a panel that did
 * not scroll, so the command somebody had just pressed a button to run was
 * the one line the panel could never show: it landed below the fold of a
 * drawer that opens two fifths of the window tall. The transcript reads
 * upwards now — the way a shell history does when it is asked for — and the
 * top row is always the last thing git did.
 *
 * A run of the same command is one row with a count. The work tree is polled
 * with `git status` every two seconds while the window has focus (ADR 0015),
 * and a log that drew each poll as a line of its own was thirty identical
 * lines between any two commands worth reading. Nothing is hidden by the
 * fold: the count says how many times, the duration is the latest, and a run
 * ends the moment the command, its exit code or what git wrote to stderr
 * differs — so a poll that starts failing is a new row, in red, where it can
 * be seen.
 */
export function CommandLogPanel({ executions, className }: CommandLogPanelProps) {
  const runs = collapseRuns(executions);

  return (
    <Panel title="Command log" icon={<TerminalIcon />} className={className} flush>
      {executions.length === 0 ? (
        <EmptyState
          title="No commands yet"
          description="Every git command yagit runs will appear here, exactly as it was executed."
        />
      ) : (
        // `relative`, and it is load-bearing. A row with a repeat count
        // carries an sr-only span, which is positioned absolutely; with no
        // positioned ancestor its containing block is the viewport, so it
        // escapes this scroller's clipping and sits at the row's offset in
        // PAGE coordinates — thirteen thousand pixels down, extending the
        // document that far and making the page scroll under a panel that
        // was already scrolling. Positioning the scroller makes it the
        // containing block, and the spans scroll and clip with their rows.
        <div className="relative h-full overflow-auto">
          <ol className="divide-y divide-line">
            {runs.map((run) => (
              <ExecutionRow key={run.last.id} run={run} />
            ))}
          </ol>
        </div>
      )}
    </Panel>
  );
}

/** One row: the newest execution of a run, and how long the run is. */
export interface ExecutionRun {
  last: GitExecution;
  /** How many identical executions in a row this row stands for. */
  repeats: number;
}

/**
 * Consecutive identical executions, folded — newest run first.
 *
 * Identical means the same command line, the same exit code and the same
 * stderr: the three things a row draws. Two executions that differ in any of
 * them are two rows, because the difference is the news.
 *
 * Exported for its test. The fold is arithmetic over the one list this
 * project promises never to hide anything from, and an off-by-one here is a
 * command that ran and is nowhere on screen.
 */
export function collapseRuns(executions: readonly GitExecution[]): ExecutionRun[] {
  const runs: ExecutionRun[] = [];
  for (const execution of executions) {
    const open = runs[runs.length - 1];
    if (open !== undefined && sameExecution(open.last, execution)) {
      open.last = execution;
      open.repeats += 1;
      continue;
    }
    runs.push({ last: execution, repeats: 1 });
  }
  return runs.reverse();
}

function sameExecution(one: GitExecution, other: GitExecution): boolean {
  return (
    one.command === other.command &&
    one.exit_code === other.exit_code &&
    one.stderr.trim() === other.stderr.trim()
  );
}

function ExecutionRow({ run }: { run: ExecutionRun }) {
  const { last: execution, repeats } = run;
  const failed = execution.exit_code !== 0;

  return (
    <li className="flex flex-col gap-1 px-3 py-2 transition-colors transition-instant hover:bg-hover">
      <div className="flex items-baseline gap-2">
        {/* The prompt carries the outcome: a green dollar sign for an exit
            of zero and a red one otherwise, so a failure is found by colour
            down the column before the badge beside it is read. */}
        <span
          aria-hidden="true"
          className={cx('font-mono text-xs font-semibold', failed ? 'text-danger' : 'text-success')}
        >
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
        {repeats > 1 && (
          <>
            <span aria-hidden="true" className="shrink-0 text-2xs text-ink-subtle tabular">
              ×{repeats}
            </span>
            <span className="sr-only">{repeats} times</span>
          </>
        )}
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
