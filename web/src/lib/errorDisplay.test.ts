import { describe, expect, it } from 'vitest';

import { ApiError } from '../api/client';
import { gitFailureLine, refusalHeading } from './errorDisplay';

/**
 * The one refusal the interface has to read out of a status code.
 *
 * Being wrong here produces no error: the user is told a click failed, in
 * words that name neither what was refused nor the one thing that would let
 * them get on — which is the "something went wrong" this codebase exists to
 * avoid.
 */
describe('refusalHeading', () => {
  it('separates a body over the cap from a diff over it', () => {
    // Two different 413s. The daemon's own refusal comes with the sentence
    // that explains it; the diff's comes wrapped in git's words about a
    // command that was stopped, which reads as a git failure and is not one.
    const selection = new ApiError(413, 'this selection is larger than the 65536 bytes …');
    const diff = new ApiError(413, 'git diff …: exit code -1: git wrote more than …', {
      command: 'git diff …',
      args: ['diff'],
      exit_code: -1,
      stderr: 'git wrote more than 10485760 bytes and was stopped',
    });

    expect(refusalHeading(selection)).toBe('That selection is too large to send');
    expect(refusalHeading(diff)).toBe('That diff is too large to read');
  });

  it('says nothing about a refusal whose message already says it', () => {
    // A 409 names the file that moved, a 404 names the path git does not
    // list, a 422 carries git's own stderr. Adding a heading to those would
    // put a vaguer sentence above a precise one.
    expect(refusalHeading(new ApiError(409, 'the file changed since this diff was read'))).toBe(
      undefined,
    );
    expect(refusalHeading(new ApiError(404, 'git does not report "x" as untracked'))).toBe(
      undefined,
    );
    expect(refusalHeading(new Error('the daemon is not answering'))).toBe(undefined);
  });
});

/**
 * The row that reports a failed page has one line for it, so that line is
 * where the project's promise either survives the flattening or is lost.
 */
describe('gitFailureLine', () => {
  const refused = new ApiError(500, 'could not read the history', {
    command: 'git log --max-count=200 --skip=200',
    args: ['log', '--max-count=200', '--skip=200'],
    exit_code: 128,
    stderr: 'fatal: bad object HEAD',
  });

  it('keeps the command, the exit code and the raw stderr', () => {
    const line = gitFailureLine(refused);

    expect(line).toContain('git log --max-count=200 --skip=200');
    expect(line).toContain('exit 128');
    expect(line).toContain('fatal: bad object HEAD');
  });

  it('leads with what git said, because the row cuts the line from the right', () => {
    // The line shares a row with a graph that can take a third of the panel.
    // What survives the clipping has to be the half the reader could not have
    // supplied themselves.
    const line = gitFailureLine(refused) ?? '';

    expect(line.indexOf('fatal: bad object HEAD')).toBeLessThan(line.indexOf('git log'));
  });

  it('has nothing to add to a failure git had no part in', () => {
    // The daemon never answered, so there is no command and no stderr, and
    // the message on the row above is already the whole of what happened.
    expect(gitFailureLine(new ApiError(0, 'the daemon is not answering'))).toBeUndefined();
    expect(gitFailureLine(new Error('the daemon is not answering'))).toBeUndefined();
  });
});
