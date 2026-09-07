import type { Stash } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { Tooltip } from '../components/Tooltip';
import { cx } from '../lib/cx';
import { errorDescription } from '../lib/errorDisplay';
import { formatAbsoluteTime, formatRelativeTime } from '../lib/format';
import { stashRef } from './stash';

/**
 * The stash stack, under the references it sits beside.
 *
 * Here rather than in the changes view, though a stash is made OUT of the
 * working directory, because of what it is once it exists: a commit the
 * repository is holding on to, filed against a branch, waiting to be put back.
 * That is the same kind of thing as the list above it — something the
 * repository holds — and it is the reason the column is where both belong.
 *
 * Which is also why making one is a button in this header rather than beside
 * the commit box. Everything about a stash is in one place, and the panel that
 * offers to make one is the panel that then shows it.
 *
 * A row is one click from being read and one menu from being acted on, which
 * is the sidebar's own rule: the click that only looks is the row, and
 * anything that moves the repository is a press of its own.
 */

interface StashPanelProps {
  stashes: Stash[] | undefined;
  /** True while the first read is out. */
  loading: boolean;
  /** Why the list is not on screen, or undefined when it is. */
  error?: Error;

  /** The position being read below, so the row can say so. */
  selected?: number;
  /** Opens one stash's patch. */
  onSelect: (stash: Stash) => void;

  /**
   * Puts the stash question, or undefined where no repository of this shape
   * could ever answer it: a bare one, which has no work tree to save.
   */
  onStash?: () => void;
  /** Whether that plan is being read. */
  stashing?: boolean;
  /**
   * Why stashing is refused right now, or undefined when it is offered.
   *
   * The sentence rather than a boolean, for the reason CommitDetails takes
   * one: a button greyed with nothing to say reads as a screen that is broken.
   */
  stashRefusal?: string;

  /** Puts one stash back, keeping it or removing it. */
  onApply?: (stash: Stash) => void;
  /** Throws one away, after the confirmation the caller puts up. */
  onDrop?: (stash: Stash) => void;
}

export function StashPanel({
  stashes,
  loading,
  error,
  selected,
  onSelect,
  onStash,
  stashing = false,
  stashRefusal,
  onApply,
  onDrop,
}: StashPanelProps) {
  return (
    <Panel
      title={`Stashes${stashes === undefined || stashes.length === 0 ? '' : ` — ${stashes.length}`}`}
      // Capped, and able to shrink below the cap. The references above want
      // the height far more than a stack of three does — but a panel that
      // could only ever be its content's height pushed the page itself into
      // scrolling on a short window, which is the one thing this layout must
      // not do. Bounded here and scrolled inside, exactly as the panel above.
      className="max-h-48 min-h-0"
      flush
      actions={
        onStash === undefined ? undefined : <StashAction {...{ onStash, stashing, stashRefusal }} />
      }
    >
      <div className="h-full overflow-auto">
        {loading && (
          <div className="grid place-items-center p-4">
            <Spinner label="Reading the stashes" />
          </div>
        )}

        {error !== undefined && (
          <EmptyState
            title="Could not read the stashes"
            description=""
            detail={errorDescription(error)}
            className="py-6"
          />
        )}

        {stashes !== undefined && stashes.length === 0 && (
          <p className="px-3 py-4 text-xs text-ink-subtle">
            Nothing stashed. Setting the work tree aside puts it here, and it stays until you put it
            back.
          </p>
        )}

        {stashes?.map((stash) => (
          <Row
            key={stash.sha + stash.index}
            stash={stash}
            current={selected === stash.index}
            onSelect={() => onSelect(stash)}
            items={rowItems({ stash, onApply, onDrop })}
          />
        ))}
      </div>
    </Panel>
  );
}

/**
 * The button that makes one, offered or refused.
 *
 * Two shapes for the reason CommitDetails draws two: a disabled Button drops
 * pointer events, so the browser fires no hover on it and a `title` would be
 * readable by nobody — precisely when the sentence is needed. Tooltip hovers
 * the span around it and reaches the button through aria-describedby.
 */
function StashAction({
  onStash,
  stashing,
  stashRefusal,
}: {
  onStash: () => void;
  stashing: boolean;
  stashRefusal: string | undefined;
}) {
  const button = (
    <Button
      size="sm"
      variant="ghost"
      onClick={onStash}
      loading={stashing}
      disabled={stashRefusal !== undefined}
      aria-label="Stash the changes in the work tree"
    >
      Stash changes
    </Button>
  );

  if (stashRefusal === undefined) {
    return button;
  }
  return <Tooltip label={stashRefusal}>{button}</Tooltip>;
}

/**
 * One entry.
 *
 * The message is the label and the position is the small print, which is the
 * right way round for reading and the wrong way round for git: stash@{1} is
 * what the command takes, and it is also the part that means something else
 * tomorrow. So it is shown — somebody reading the log panel needs to match the
 * two — and it is never what the row sends. The object name is.
 */
function Row({
  stash,
  current,
  onSelect,
  items,
}: {
  stash: Stash;
  current: boolean;
  onSelect: () => void;
  items: MenuItem[];
}) {
  const made = new Date(stash.date);

  return (
    <div className="group/row relative">
      <button
        type="button"
        onClick={onSelect}
        aria-current={current ? 'true' : undefined}
        className={cx(
          'flex w-full min-w-0 flex-col gap-0.5 px-3 py-2 text-left',
          'transition-colors transition-instant outline-none',
          'hover:bg-sunken focus-visible:focus-ring',
          current && 'bg-sunken',
        )}
      >
        <span className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-sm text-ink">
            {stash.message === '' ? stashRef(stash.index) : stash.message}
          </span>
          {/* Only where there is one. A stash made on a detached HEAD has no
              branch, and git's own "(no branch)" is not a name anybody can do
              anything with. */}
          {stash.branch !== '' && <Badge>{stash.branch}</Badge>}
        </span>

        <span className="flex min-w-0 items-center gap-2 text-2xs text-ink-subtle">
          <code className="font-mono">{stashRef(stash.index)}</code>
          <span title={formatAbsoluteTime(made)}>{formatRelativeTime(made, new Date())}</span>
        </span>
      </button>

      {items.length > 0 && (
        <div className="absolute top-1.5 right-2 opacity-0 transition-opacity transition-instant group-hover/row:opacity-100 focus-within:opacity-100">
          <Menu label={`Actions for ${stashRef(stash.index)}`} items={items} />
        </div>
      )}
    </div>
  );
}

/**
 * What a row offers.
 *
 * Three items and no button, unlike the reference rows beside them, and the
 * reason is which click is wanted often enough to be worth the width. On a
 * branch that is "check out"; on a stash there is no such favourite — apply
 * and pop are chosen between rather than one being the default, and a
 * mis-click on either writes the work tree.
 */
function rowItems({
  stash,
  onApply,
  onDrop,
}: {
  stash: Stash;
  onApply?: (stash: Stash) => void;
  onDrop?: (stash: Stash) => void;
}): MenuItem[] {
  const items: MenuItem[] = [];

  if (onApply !== undefined) {
    // One item for both modes rather than two. The dialog it opens is where
    // apply and pop are chosen between, because that is where the sentence
    // saying what each does can sit beside the choice.
    items.push(
      menuItem({
        id: 'apply',
        label: 'Put back…',
        onSelect: () => onApply(stash),
      }),
    );
  }

  if (onDrop !== undefined) {
    items.push(
      menuItem({
        id: 'drop',
        label: 'Drop…',
        onSelect: () => onDrop(stash),
        danger: true,
      }),
    );
  }

  return items;
}
