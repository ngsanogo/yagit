import { describe, expect, it } from 'vitest';

import { propose, type PlanRequest, type ProposalTarget, type WorkbenchDialog } from './dialogSlot';

/**
 * What a proposal does with each of the three things a plan can answer.
 *
 * The failing one is why this file exists. Every confirmation in the workbench
 * reads a plan before it opens, and while that was written out at each of the
 * twenty call sites, two of them forgot the failure path entirely: asking what
 * adding a worktree or a submodule would run, and being refused, left the
 * dialog sitting there with nothing said. A button that reads a command and
 * then does nothing is a button somebody presses again.
 */

/** A plan request that answers however the test says, at once. */
function answering<Answer>(answer: Answer): PlanRequest<null, Answer> {
  return { mutate: (_input, handlers) => handlers.onSuccess(answer) };
}

function failing(error: Error): PlanRequest<null, never> {
  return { mutate: (_input, handlers) => handlers.onError(error) };
}

/** A slot that records what it was asked to open, and what it was told. */
function recorder() {
  const opened: WorkbenchDialog[] = [];
  const reported: { title: string; error: Error }[] = [];
  const target: ProposalTarget = {
    claim: () => (next) => opened.push(next),
    report: (title, error) => reported.push({ title, error }),
  };
  return { opened, reported, target };
}

describe('propose', () => {
  it('opens the dialog its answer describes', () => {
    const slot = recorder();

    propose(
      slot.target,
      answering('git branch -D topic'),
      null,
      'what deleting topic would run',
      (command) => ({
        kind: 'delete',
        pending: { name: 'topic', force: true, command },
      }),
    );

    expect(slot.opened).toEqual([
      { kind: 'delete', pending: { name: 'topic', force: true, command: 'git branch -D topic' } },
    ]);
    expect(slot.reported).toEqual([]);
  });

  it('opens nothing when the answer has nothing to confirm', () => {
    const slot = recorder();

    // What a cherry-pick of a commit the branch already holds answers with:
    // there is no command that succeeds as a no-op, so the plan says so its own
    // way and no question is put.
    propose(
      slot.target,
      answering('up-to-date'),
      null,
      'what cherry-picking abc would do',
      () => undefined,
    );

    expect(slot.opened).toEqual([]);
    expect(slot.reported).toEqual([]);
  });

  it('reports a plan it could not read, and opens nothing', () => {
    const slot = recorder();
    const refused = new Error('fatal: not a valid object name');

    propose(slot.target, failing(refused), null, 'what removing web/vendor would run', () => {
      throw new Error('the answer handler must not run for a failed plan');
    });

    expect(slot.opened).toEqual([]);
    expect(slot.reported).toEqual([
      { title: 'Could not read what removing web/vendor would run', error: refused },
    ]);
  });

  it('claims the slot before the request goes out, not after it', () => {
    // The order matters and nothing else can check it: a claim taken after the
    // answer would be a claim nothing could invalidate, so a plan that came
    // back seconds late would open under the hands of somebody who had moved
    // on — with the next Enter on the button that runs it.
    const order: string[] = [];
    const target: ProposalTarget = {
      claim: () => {
        order.push('claim');
        return () => order.push('open');
      },
      report: () => order.push('report'),
    };

    propose(
      target,
      {
        mutate: (_input, handlers) => {
          order.push('ask');
          handlers.onSuccess('done');
        },
      },
      null,
      'what it would do',
      () => ({ kind: 'create' }),
    );

    expect(order).toEqual(['claim', 'ask', 'open']);
  });
});
