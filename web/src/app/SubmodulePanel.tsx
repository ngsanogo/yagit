import type { Submodule } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { QueryErrorState, type RetryableQuery } from '../components/PanelState';
import { cx } from '../lib/cx';
import { shortenSha } from '../lib/format';

/**
 * The repositories this one pins, under the worktrees.
 *
 * Drawn only where there is at least one, unlike the panels above it: every
 * repository has a working tree and a stash stack, and most have no
 * submodules at all — an empty panel here is height spent saying nothing, and
 * one more panel to scroll past on the way to the ones with something to say.
 * Nor is the wait drawn — see the early return, which is why nothing below it
 * has a loading state.
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
  /** The query behind that failure, so the panel can offer to ask again. */
  retry?: RetryableQuery;

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
  retry,
  onUpdate,
  updating = false,
  onSync,
  onRemove,
}: SubmodulePanelProps) {
  // Nothing pinned and nothing to say: no panel. Adding is not lost with it:
  // see the doc above, and RepositoryAdditions.
  //
  // The wait counts as nothing to say. Most repositories pin nothing, so the
  // only thing a spinner here ever did was draw a panel and take it away a few
  // milliseconds later, moving everything under it in the column down and back
  // up on the way. A repository that does have submodules gets this panel a
  // moment later instead, which is the cheaper of the two surprises.
  if (error === undefined && (loading || submodules === undefined || submodules.length === 0)) {
    return null;
  }

  // Worth offering only where something is missing: an update with every
  // checkout already in place reports success for doing nothing.
  const anyMissing = submodules?.some((submodule) => !submodule.present) ?? false;

  return (
    <Panel
      title={`Submodules${submodules === undefined ? '' : ` — ${submodules.length}`}`}
      className="max-h-48 shrink-0"
      flush
      actions={
        <div className="flex items-center gap-1">
          {/* The name begins with the words on the button, and the sentence
              is a description rather than the name. An aria-label that
              replaces the visible text breaks WCAG 2.5.3 (Label in Name,
              level A) and with it voice control: somebody who says "click
              Update all" is naming a control whose accessible name did not
              contain those words. The explanation goes in a native `title`
              for the reason CommitAction sets out — a Tooltip's bubble hangs
              above its anchor and Panel is `overflow-hidden`, so a bubble on
              a header button is painted outside the panel and clipped away
              unread. */}
          {anyMissing && onUpdate !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onUpdate('')}
              loading={updating}
              aria-label="Update all submodules"
              title="Checks out every submodule at the commit this repository records."
            >
              Update all
            </Button>
          )}
          {onSync !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              onClick={onSync}
              aria-label="Sync URLs from .gitmodules"
              title="Copies the submodule URLs from .gitmodules into this repository's config."
            >
              Sync URLs
            </Button>
          )}
        </div>
      }
    >
      <div className="h-full overflow-auto">
        {error !== undefined && (
          <QueryErrorState
            title="Could not read the submodules"
            error={error}
            compact
            retry={retry}
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
