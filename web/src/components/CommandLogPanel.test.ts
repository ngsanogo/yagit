import { describe, expect, it } from 'vitest';

import type { GitExecution } from '../api/types';
import { collapseRuns } from './CommandLogPanel';

function execution(id: number, command: string, exit_code = 0, stderr = ''): GitExecution {
  return {
    id: String(id),
    command,
    exit_code,
    duration_ms: id,
    stderr,
    started_at: '2026-03-01T10:00:00Z',
  };
}

const STATUS = 'git status --porcelain=v2 --branch --untracked-files=all -z';

describe('collapseRuns', () => {
  it('folds a run of identical commands into one row that counts them', () => {
    const runs = collapseRuns([
      execution(1, STATUS),
      execution(2, STATUS),
      execution(3, STATUS),
      execution(4, 'git fetch --all --prune'),
    ]);

    expect(runs.map((run) => [run.last.command, run.repeats])).toEqual([
      ['git fetch --all --prune', 1],
      [STATUS, 3],
    ]);
  });

  it('keeps the newest execution of a run, so the duration shown is the latest', () => {
    const [run] = collapseRuns([execution(1, STATUS), execution(2, STATUS)]);
    expect(run?.last.id).toBe('2');
  });

  it('ends a run where the outcome changes, so a poll that starts failing is its own row', () => {
    const runs = collapseRuns([
      execution(1, STATUS),
      execution(2, STATUS, 128, 'fatal: index.lock'),
      execution(3, STATUS),
    ]);

    expect(runs.map((run) => [run.last.exit_code, run.repeats])).toEqual([
      [0, 1],
      [128, 1],
      [0, 1],
    ]);
  });

  it('does not fold the same command run twice with another between', () => {
    const runs = collapseRuns([
      execution(1, STATUS),
      execution(2, 'git fetch --all --prune'),
      execution(3, STATUS),
    ]);

    expect(runs).toHaveLength(3);
  });

  it('answers newest first, which is the order the panel reads in', () => {
    const runs = collapseRuns([execution(1, 'git a'), execution(2, 'git b')]);
    expect(runs.map((run) => run.last.command)).toEqual(['git b', 'git a']);
  });

  it('loses nothing: the counts add up to the executions', () => {
    const executions = [
      execution(1, STATUS),
      execution(2, STATUS),
      execution(3, 'git fetch --all --prune'),
      execution(4, STATUS),
    ];
    const total = collapseRuns(executions).reduce((sum, run) => sum + run.repeats, 0);
    expect(total).toBe(executions.length);
  });
});
