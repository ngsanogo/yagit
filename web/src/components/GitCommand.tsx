import { copyLabel, useClipboard } from '../lib/clipboard';
import { cx } from '../lib/cx';
import { CheckIcon, CopyIcon } from './Icons';
import { Tooltip } from './Tooltip';

interface GitCommandProps {
  command: string;
  className?: string;
  /** Set to false to hide the copy button, for dense lists. */
  copyable?: boolean;
}

/**
 * Shows a git command, either as it will run or as it ran.
 *
 * This is the component that carries one of the project's central promises:
 * no destructive operation runs without its exact command having been shown.
 * That is why both the log panel and the confirmation dialogs use it, and
 * both show exactly the same thing.
 */
export function GitCommand({ command, className, copyable = true }: GitCommandProps) {
  const clipboard = useClipboard();

  return (
    <div
      className={cx(
        'group/command flex min-w-0 items-start gap-2 rounded-md bg-sunken px-2.5 py-1.5',
        'border border-line',
        className,
      )}
    >
      <span aria-hidden="true" className="mt-px shrink-0 font-mono text-xs text-ink-subtle">
        $
      </span>
      {/* break-words, not break-all: a command is words with spaces between
          them, and breaking anywhere splits `refs/heads/lane-assignm` from
          `ent` while a space sits unused two characters to the left. Long
          tokens with nowhere to break — a path, a sha — still break, because
          that is what overflow-wrap does when a word cannot fit. */}
      <code className="min-w-0 flex-1 font-mono text-xs break-words text-ink">{command}</code>

      {copyable && (
        <Tooltip label={copyLabel(clipboard.state, 'Copy command')}>
          <button
            type="button"
            onClick={() => void clipboard.copy(command)}
            aria-label={copyLabel(clipboard.state, 'Copy command')}
            className={cx(
              'rounded-sm p-1 outline-none transition-colors transition-instant',
              'focus-visible:focus-ring',
              clipboard.state === 'failed' ? 'text-danger' : 'text-ink-subtle hover:text-ink',
            )}
          >
            {clipboard.state === 'copied' ? <CheckIcon /> : <CopyIcon />}
          </button>
        </Tooltip>
      )}
    </div>
  );
}
