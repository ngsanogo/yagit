import type { Submodule } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';

/**
 * The repositories this one pins, under the worktrees.
 *
 * Drawn only where there is at least one, unlike the panels above it: every
 * repository has a working tree and a stash stack, and most have no
 * submodules at all — an empty panel in that column would cost height the
 * references want and say nothing.
 *
 * Which is why pinning one is NOT offered here. The button was, and it made
 * the operation unreachable for exactly the repositories that needed it: no
 * submodule, no panel, no way to add a first. It lives in RepositoryAdditions
 * now — the row under these panels, drawn whether this panel is or not. What
 * is left here are the actions that need a row to act on.
 *
 * What a row has to make legible is the state, because "submodule" covers four
 * of them and they need different things done: recorded but never fetched,
 * fetched and at the pinned commit, fetched and somewhere else, and a gitlink
 * with nothing in `.gitmodules` describing it.
 */

interface SubmodulePanelProps {
  submodules: Submodule[] | undefined;
  loading: boolean;
  error?: Error;

  /** Checks out what this repository records — all of them, or one. */
  onUpdate?: (path: string) => void;
  updating?: boolean;

  /** Copies the URLs from .gitmodules into the local config. */
  onSync?: () => void;

  /** Unpins one, after the confirmation the caller puts up. */
  onRemove?: (submodule: Submodule) => void;
}

export function SubmodulePanel({
  submodules,
  loading,
  error,
  onUpdate,
  updating = false,
  onSync,
  onRemove,
}: SubmodulePanelProps) {
  // Nothing pinned and nothing to say: no panel. Adding is not lost with it:
  // see the doc above, and RepositoryAdditions.
  if (!loading && error === undefined && submodules !== undefined && submodules.length === 0) {
    return null;
  }

  // Worth offering only where something is missing: an update with every
  // checkout already in place reports success for doing nothing.
  const anyMissing = submodules?.some((submodule) => !submodule.present) ?? false;

  return (
    <Panel
      title={`Submodules${submodules === undefined ? '' : ` — ${submodules.length}`}`}
      className="max-h-48 min-h-0"
      flush
      actions={
        <div className="flex items-center gap-1">
          {anyMissing && onUpdate !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onUpdate('')}
              loading={updating}
              aria-label="Check out every submodule at the commit this repository records"
            >
              Update all
            </Button>
          )}
          {onSync !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              onClick={onSync}
              aria-label="Copy the submodule URLs from .gitmodules into this repository's config"
            >
              Sync URLs
            </Button>
          )}
        </div>
      }
    >
      <div className="h-full overflow-auto">
        {loading && (
          <div className="grid place-items-center p-4">
            <Spinner label="Reading the submodules" />
          </div>
        )}

        {error !== undefined && (
          <EmptyState
            title="Could not read the submodules"
            description=""
            detail={errorDescription(error)}
            className="py-6"
          />
        )}

        {submodules?.map((submodule) => (
          <Row
            key={submodule.path}
            submodule={submodule}
            items={rowItems({ submodule, onUpdate, onRemove })}
          />
        ))}
      </div>
    </Panel>
  );
}

/** What state a submodule is in, in the words a row shows. */
export function submoduleState(submodule: Submodule): string {
  if (!submodule.present) {
    return submodule.initialised ? 'not checked out' : 'not fetched';
  }
  if (submodule.moved) {
    return `moved to ${shortenSha(submodule.head)}`;
  }
  return `at ${shortenSha(submodule.recorded)}`;
}

function Row({ submodule, items }: { submodule: Submodule; items: MenuItem[] }) {
  return (
    <div className="group/row relative">
      <div
        title={submodule.url === '' ? submodule.path : `${submodule.path} — ${submodule.url}`}
        className={cx('flex w-full min-w-0 flex-col gap-0.5 px-3 py-2 text-left')}
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-sm text-ink">{submodule.path}</span>
          {/* A gitlink nobody described. Named rather than hidden: it is why
              an update cannot fetch it, and the fix is a file to commit. */}
          {!submodule.declared && <Badge tone="warning">not in .gitmodules</Badge>}
          {submodule.moved && <Badge tone="warning">moved</Badge>}
        </span>

        <span className="flex min-w-0 items-center gap-2 text-2xs text-ink-subtle">
          <span className="truncate">{submoduleState(submodule)}</span>
        </span>
      </div>

      {items.length > 0 && (
        <div className="absolute top-1.5 right-2 opacity-0 transition-opacity transition-instant group-hover/row:opacity-100 focus-within:opacity-100">
          <Menu label={`Actions for the submodule ${submodule.path}`} items={items} />
        </div>
      )}
    </div>
  );
}

function rowItems({
  submodule,
  onUpdate,
  onRemove,
}: {
  submodule: Submodule;
  onUpdate?: (path: string) => void;
  onRemove?: (submodule: Submodule) => void;
}): MenuItem[] {
  const items: MenuItem[] = [];

  if (onUpdate !== undefined) {
    items.push(
      menuItem(
        {
          id: 'update',
          label: 'Update…',
          onSelect: () => onUpdate(submodule.path),
        },
        submodule.declared
          ? undefined
          : 'Nothing in .gitmodules says where to fetch this from, so git has no URL to use.',
      ),
    );
  }

  if (onRemove !== undefined) {
    items.push(
      menuItem({
        id: 'remove',
        label: 'Remove…',
        onSelect: () => onRemove(submodule),
        danger: true,
      }),
    );
  }

  return items;
}
