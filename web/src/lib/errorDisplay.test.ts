import { describe, expect, it } from 'vitest';

import { ApiError } from '../api/client';
import { errorSummary, gitFailureLine, refusalHeading } from './errorDisplay';

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

/**
 * The daemon sends git's account of a failure twice — once as the message,
 * once as the object beside it — and this is the subtraction that keeps the
 * interface from drawing both. Getting it wrong is not an error either way:
 * too eager and a route's own sentence disappears, too shy and every failure
 * in the product is twice as tall as it needs to be.
 */
describe('errorSummary', () => {
  const failure = {
    command: 'git switch --no-guess -- refs/heads/nope',
    args: ['switch', '--no-guess', '--', 'refs/heads/nope'],
    exit_code: 128,
    stderr: 'fatal: invalid reference: refs/heads/nope\n',
  };

  it('has nothing to add when the message is what the block is about to draw', () => {
    // What every route that hands the git error straight back sends: the
    // command, the exit code and the stderr, which is exactly what the block
    // under it is about to draw.
    const error = new ApiError(
      500,
      'git switch --no-guess -- refs/heads/nope: exit code 128: ' +
        'fatal: invalid reference: refs/heads/nope',
      failure,
    );

    expect(errorSummary(error)).toBeUndefined();
  });

  it('keeps the sentence a route added in front of it', () => {
    // A route that wrapped the git error said something the block cannot: it
    // names what yagit was doing when git refused.
    const error = new ApiError(
      500,
      'could not read the history: git switch --no-guess -- refs/heads/nope: exit code 128: ' +
        'fatal: invalid reference: refs/heads/nope',
      failure,
    );

    expect(errorSummary(error)).toBe('could not read the history');
  });

  it('subtracts the stand-in the daemon uses when git wrote nothing', () => {
    // git can fail in silence, and the daemon writes "(no error output)"
    // rather than end its sentence on a colon. The stderr field is still
    // empty, so the two halves only line up if this knows about the stand-in.
    const silent = { command: 'git gc', args: ['gc'], exit_code: 1, stderr: '' };

    expect(errorSummary(new ApiError(500, 'git gc: exit code 1: (no error output)', silent))).toBe(
      undefined,
    );
  });

  it('hands back a message that is not built that way, whole', () => {
    // The subtraction is a tail match, so anything the daemon assembled some
    // other way survives untouched rather than being trimmed by a pattern
    // that guessed.
    const error = new ApiError(409, 'the file changed since this diff was read', failure);

    expect(errorSummary(error)).toBe('the file changed since this diff was read');
  });

  it('hands back a failure git had no part in, whole', () => {
    expect(errorSummary(new Error('the daemon is not answering'))).toBe(
      'the daemon is not answering',
    );
    expect(errorSummary(new ApiError(0, 'the daemon is not answering'))).toBe(
      'the daemon is not answering',
    );
  });
});
