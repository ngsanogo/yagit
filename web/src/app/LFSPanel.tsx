import type { LFSSupport } from '../api/types';
import { Panel } from '../components/Panel';
import { QueryErrorState, type RetryableQuery } from '../components/PanelState';
import { cx } from '../lib/cx';
import { REVEALED_ON_ATTENTION } from '../lib/reveal';

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
 * has those two and most have never heard of LFS, so an empty panel here is
 * forty pixels of the sidebar spent saying nothing, and one more panel to
 * scroll past on the way to the ones with something to say. Nor is the wait
 * drawn — see the early return, which is why nothing below it has a loading
 * state.
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
  /** The query behind that failure, so the panel can offer to ask again. */
  retry?: RetryableQuery;

  /** Opens the confirmation, which shows the command the daemon answered. */
  onUntrack: (pattern: string) => void;
}

export function LFSPanel({ support, loading, error, retry, onUntrack }: LFSPanelProps) {
  // Nothing tracked and nothing to say: no panel. The error is still drawn — a
  // repository whose .gitattributes could not be read is not one that tracks
  // nothing, and saying so is the difference between a panel that is absent
  // and one that is quiet.
  //
  // The WAIT is not drawn, and that is the correction: almost no repository
  // uses LFS, so a spinner here is a panel drawn only to be taken away again a
  // few milliseconds later, with everything under it in the column moving down
  // and back up while that happens. The cost is that a repository which does
  // use LFS gets its panel late rather than early — `git lfs version` spawns a
  // process — and arriving once beats arriving, leaving and arriving again.
  if (error === undefined && (loading || support === undefined || support.patterns.length === 0)) {
    return null;
  }

  return (
    <Panel title="Large files" className="max-h-48 shrink-0" flush>
      <div className="flex h-full flex-col overflow-auto">
        {error !== undefined && (
          <QueryErrorState
            title="Could not read the LFS state"
            error={error}
            compact
            retry={retry}
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
                        'rounded-sm px-1 text-2xs text-ink-subtle outline-none',
                        // Pointer events with the opacity, which this row
                        // used to reveal without: an untracked glob is a
                        // line removed from .gitattributes, and it was one
                        // click into what reads as blank space away.
                        REVEALED_ON_ATTENTION,
                        'focus-visible:focus-ring hover:text-danger',
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
