import type { LFSSupport } from '../api/types';
import { Button } from '../components/Button';
import { Tooltip } from '../components/Tooltip';

/**
 * Starting the two things a repository is not born using: a submodule, and Git
 * LFS.
 *
 * A row of its own because of a shape the column above it has. Those panels
 * are drawn only where they have something to list — most repositories pin
 * nothing and have never heard of LFS, and an empty panel would cost the
 * references forty pixels to say so — and each of them used to hold the only
 * way to start the thing it lists. The result was a feature that could be used
 * only by a repository already using it: no submodule, no panel, no "Add
 * submodule"; and untracking a last LFS pattern took the panel away
 * mid-action, with no way to put the pattern back.
 *
 * So the two entry points moved OUT of the panels, to the one place that is
 * always drawn. Always, and not only when a panel is missing: an entry point
 * that moves as soon as you use it is worse than one that costs a row. This is
 * the "one obvious way" rule paid for in pixels — one row of two buttons,
 * against the two panel headers that hiding them saved.
 *
 * Both operations write into a work tree, which is why a bare repository is
 * shown none of this rather than two permanent refusals.
 */

export function RepositoryAdditions({
  lfs,
  lfsLoading,
  lfsPending,
  submodulePending,
  onAddSubmodule,
  onTrackLFS,
}: {
  /** Undefined while the first answer is still being read, or if it failed. */
  lfs: LFSSupport | undefined;
  lfsLoading: boolean;
  /** A plan is being read for one of them, so the button that asked says so. */
  lfsPending: boolean;
  submodulePending: boolean;
  onAddSubmodule: () => void;
  onTrackLFS: () => void;
}) {
  const lfsRefusal = lfsUnavailableReason(lfs, lfsLoading);

  return (
    <div className="flex shrink-0 flex-wrap items-center gap-1">
      <Button size="sm" variant="ghost" onClick={onAddSubmodule} loading={submodulePending}>
        Add submodule…
      </Button>

      {/* Refused with its reason on the button rather than left off the row,
          which is the same rule Menu states for an item it greys out: "git-lfs
          is not installed" is the answer somebody is looking for, and a button
          that vanished would have them looking for it in yagit instead of on
          their machine. */}
      <Tooltip label={lfsRefusal ?? 'Keep files matching a pattern outside the repository.'}>
        <Button
          size="sm"
          variant="ghost"
          onClick={onTrackLFS}
          loading={lfsPending}
          disabled={lfsRefusal !== undefined}
        >
          Track large files…
        </Button>
      </Tooltip>
    </div>
  );
}

/** Why tracking cannot be offered, or undefined when it can. */
function lfsUnavailableReason(lfs: LFSSupport | undefined, loading: boolean): string | undefined {
  if (loading) {
    return 'Still asking git-lfs whether it is installed.';
  }
  if (lfs === undefined) {
    return 'yagit could not tell whether git-lfs is installed here.';
  }
  if (!lfs.installed) {
    return 'git-lfs is not installed on this machine. yagit will not install it for you.';
  }
  return undefined;
}
