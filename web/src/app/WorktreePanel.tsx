import type { Worktree } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { leafOf } from '../lib/path';

/**
 * The checkouts this repository has, under the stash it sits beside.
 *
 * In this column for the same reason the stash is: it is something the
 * repository holds rather than something the working directory is doing. The
 * list is the same whichever checkout you have open — they share one git
 * directory — so the row for the one you are on says so, and nothing else in
 * the panel changes between tabs.
 *
 * A row is not a tab, and cannot be. The registry's identity for a repository
 * is its GIT directory, which every linked worktree shares — so opening one
 * would answer with the tab that is already open on the main tree. What the
 * list is for is knowing they exist, which branch each holds (git refuses to
 * check the same branch out twice), and being able to make and unmake them.
 */

interface WorktreePanelProps {
  worktrees: Worktree[] | undefined;
  loading: boolean;
  error?: Error;

  /** Makes another checkout. */
  onAdd?: () => void;
  /** Whether that plan is being read. */
  adding?: boolean;

  /** Removes one, after the confirmation the caller puts up. */
  onRemove?: (worktree: Worktree) => void;
  /** Forgets the ones whose directories are gone. */
  onPrune?: () => void;
  pruning?: boolean;
}

export function WorktreePanel({
  worktrees,
  loading,
  error,
  onAdd,
  adding = false,
  onRemove,
  onPrune,
  pruning = false,
}: WorktreePanelProps) {
  // Only worth offering where there is something to forget: prune on a list
  // with nothing stale is a button that reports success for doing nothing.
  const stale = worktrees?.some((worktree) => worktree.prunable) ?? false;

  return (
    <Panel
      title={`Worktrees${worktrees === undefined ? '' : ` — ${worktrees.length}`}`}
      className="max-h-48 min-h-0"
      flush
      actions={
        <div className="flex items-center gap-1">
          {stale && onPrune !== undefined && (
            <Button size="sm" variant="ghost" onClick={onPrune} loading={pruning}>
              Prune
            </Button>
          )}
          {onAdd !== undefined && (
            <Button
              size="sm"
              variant="ghost"
              onClick={onAdd}
              loading={adding}
              aria-label="Check out another worktree of this repository"
            >
              Add worktree
            </Button>
          )}
        </div>
      }
    >
      <div className="h-full overflow-auto">
        {loading && (
          <div className="grid place-items-center p-4">
            <Spinner label="Reading the worktrees" />
          </div>
        )}

        {error !== undefined && (
          <EmptyState
            title="Could not read the worktrees"
            description=""
            detail={errorDescription(error)}
            className="py-6"
          />
        )}

        {worktrees?.map((worktree) => (
          <Row key={worktree.path} worktree={worktree} items={rowItems({ worktree, onRemove })} />
        ))}
      </div>
    </Panel>
  );
}

/**
 * How a worktree is named in an accessible label: by what it has checked out,
 * never by where it lives.
 *
 * git guarantees the answer is unique — one branch is checked out in one
 * working tree — so it identifies the row as well as the path would. And it
 * keeps an absolute path out of a name a screen reader reads aloud, which is
 * the whole of the accessibility argument on its own.
 */
function describe(worktree: Worktree): string {
  if (worktree.bare) {
    return 'with no checkout';
  }
  if (worktree.detached) {
    return `detached at ${shortenSha(worktree.head)}`;
  }
  return `on ${worktree.branch}`;
}

// A row reads rather than acts, so it is a div and not a button: there is no
// click on it, and a disabled button with no action is a focus stop that leads
// nowhere.
function Row({ worktree, items }: { worktree: Worktree; items: MenuItem[] }) {
  return (
    <div className="group/row relative">
      <div
        aria-current={worktree.current ? 'true' : undefined}
        title={worktree.path}
        className={cx(
          'flex w-full min-w-0 flex-col gap-0.5 px-3 py-2 text-left',
          worktree.current && 'bg-sunken',
        )}
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-sm text-ink">{leafOf(worktree.path)}</span>
          {worktree.main && <Badge>main tree</Badge>}
          {worktree.current && <Badge>this tab</Badge>}
        </span>

        <span className="flex min-w-0 items-center gap-2 text-2xs text-ink-subtle">
          {worktree.bare ? (
            <span>bare — no checkout</span>
          ) : worktree.detached ? (
            <code className="font-mono">detached at {shortenSha(worktree.head)}</code>
          ) : (
            <span className="truncate">branch {worktree.branch}</span>
          )}
          {worktree.locked && <span>· locked</span>}
          {worktree.prunable && <span>· missing</span>}
        </span>
      </div>

      {items.length > 0 && (
        <div className="absolute top-1.5 right-2 opacity-0 transition-opacity transition-instant group-hover/row:opacity-100 focus-within:opacity-100">
          <Menu label={`Actions for the worktree ${describe(worktree)}`} items={items} />
        </div>
      )}
    </div>
  );
}

function rowItems({
  worktree,
  onRemove,
}: {
  worktree: Worktree;
  onRemove?: (worktree: Worktree) => void;
}): MenuItem[] {
  if (onRemove === undefined) {
    return [];
  }

  // Refused with a sentence rather than hidden, for the reason the sidebar
  // refuses a delete on the branch HEAD is on: an item that is simply absent
  // reads as a feature that does not exist.
  const refusal = worktree.main
    ? 'The main working tree is the repository itself; git will not remove it.'
    : undefined;

  return [
    menuItem(
      {
        id: 'remove',
        label: 'Remove…',
        onSelect: () => onRemove(worktree),
        danger: true,
      },
      refusal,
    ),
  ];
}
