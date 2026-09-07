import type { LFSSupport } from '../api/types';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { errorDescription } from '../lib/errorDisplay';

/**
 * Git LFS: which paths are kept outside the repository.
 *
 * The panel's first job is the answer nobody else gives: whether `git lfs` is
 * installed at all. A repository with LFS patterns and no git-lfs on this
 * machine is a real state, and it presents as files checking out as three
 * lines of metadata — so the panel says it once, plainly, instead of leaving
 * somebody to work it out from a diff.
 *
 * yagit never installs git-lfs. Installing a binary is not something a git
 * client should do behind somebody's back, and the wrong version of it is
 * worse than none.
 *
 * Nothing here transfers anything, and that is not an omission. LFS installs
 * itself into git as a filter and git runs it, so every fetch, pull, push and
 * checkout yagit already drives carries LFS content already. A "download
 * objects" button would be a second way to do what the ordinary one does.
 *
 * Drawn only where this repository routes something through LFS, like the
 * submodules above it and unlike the stash and the worktrees: every repository
 * has those two and most have never heard of LFS, so an empty panel in that
 * column would cost the references forty pixels of height to say nothing.
 *
 * What that used to cost was the feature. The form for the first pattern was
 * inside the panel a repository with no pattern is not given, so tracking one
 * could only be done by a repository that already did — and untracking the
 * last one took the panel away mid-action with no way back. Adding is now in
 * RepositoryAdditions, the row under these panels, which is there whether this
 * panel is or not; this lists what is tracked and offers the row action.
 */

interface LFSPanelProps {
  /** Undefined while the first answer is still being read. */
  support: LFSSupport | undefined;
  loading: boolean;
  error?: Error;

  /** Opens the confirmation, which shows the command the daemon answered. */
  onUntrack: (pattern: string) => void;
}

export function LFSPanel({ support, loading, error, onUntrack }: LFSPanelProps) {
  // Nothing tracked and nothing to say: no panel. The error and the wait are
  // still drawn — a repository whose .gitattributes could not be read is not
  // one that tracks nothing, and saying so is the difference between a panel
  // that is absent and one that is quiet.
  if (support !== undefined && support.patterns.length === 0) {
    return null;
  }

  return (
    <Panel title="Large files" className="max-h-48 min-h-0" flush>
      <div className="flex h-full flex-col overflow-auto">
        {loading && (
          <div className="grid place-items-center p-4">
            <Spinner label="Asking git-lfs" />
          </div>
        )}

        {error !== undefined && (
          <EmptyState
            title="Could not read the LFS state"
            description=""
            detail={errorDescription(error)}
            className="py-6"
          />
        )}

        {support !== undefined && (
          <>
            {/* Said whichever way round it is, because both halves matter and
                neither implies the other: patterns without the program is a
                checkout of pointer files, and the program without patterns is
                a repository that simply does not use LFS. */}
            {!support.installed && (
              <p className="px-3 py-2 text-xs text-ink-subtle">
                git-lfs is not installed here. yagit will not install it for you — until you do, a
                repository that uses LFS checks out pointer files instead of contents, and patterns
                cannot be changed from here.
              </p>
            )}

            <ul className="flex flex-col">
              {support.patterns.map((glob) => (
                <li
                  key={glob}
                  className="group/row flex items-center gap-2 px-3 py-1.5 hover:bg-hover"
                >
                  <code className="min-w-0 flex-1 truncate font-mono text-xs text-ink">{glob}</code>
                  {support.installed && (
                    <button
                      type="button"
                      onClick={() => onUntrack(glob)}
                      className={cx(
                        'rounded-sm px-1 text-2xs text-ink-subtle opacity-0',
                        'transition-opacity transition-instant outline-none',
                        'group-hover/row:opacity-100 focus-visible:opacity-100 focus-visible:focus-ring',
                        'hover:text-danger',
                      )}
                    >
                      Untrack
                    </button>
                  )}
                </li>
              ))}
            </ul>

            {support.installed && support.version !== '' && (
              <p className="truncate px-3 pb-2 font-mono text-2xs text-ink-subtle">
                {support.version}
              </p>
            )}
          </>
        )}
      </div>
    </Panel>
  );
}
