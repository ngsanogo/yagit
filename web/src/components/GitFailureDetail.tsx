import type { GitFailure } from '../api/types';
import { GitCommand } from './GitCommand';

/**
 * What git said, whole.
 *
 * The project's promise is that a git failure reaches the user with the exact
 * command, its exit code and its raw stderr. This is the one place that
 * promise is drawn.
 */
export function GitFailureDetail({ failure }: { failure: GitFailure }) {
  return (
    <div className="flex w-full max-w-2xl flex-col gap-2 text-left">
      <GitCommand command={failure.command} />
      {/* Wrapped rather than scrolled. A <pre> that only scrolls sideways
          hides the end of every long stderr line inside a scroll region no
          keyboard reaches — WCAG 2.1.1, which axe reports as
          scrollable-region-focusable, and this project asserts that rule set. */}
      <pre className="rounded-md bg-sunken p-3 font-mono text-2xs break-words whitespace-pre-wrap text-danger">
        exit {failure.exit_code}
        {'\n'}
        {failure.stderr}
      </pre>
    </div>
  );
}
