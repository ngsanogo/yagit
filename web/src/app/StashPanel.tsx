import type { Stash } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { QueryErrorState, type RetryableQuery } from '../components/PanelState';
import { Spinner } from '../components/Spinner';
import { Tooltip } from '../components/Tooltip';
import { cx } from '../lib/cx';
import { formatExactTime, formatRelativeTime } from '../lib/format';
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
  /** The query behind that failure, so the panel can offer to ask again. */
  retry?: RetryableQuery;

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
  retry,
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
      // Sized to what it holds and capped there: a stack of three takes three
      // rows of height, and a stack of thirty scrolls inside this panel rather
      // than pushing the worktrees off the column. Not shrinkable — the
      // sidebar column is what scrolls when the panels together outgrow it,
      // and a panel that gave up rows to spare the column was hiding them with
      // nothing on screen to say so.
      className="max-h-48 shrink-0"
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
          <QueryErrorState title="Could not read the stashes" error={error} compact retry={retry} />
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
 *
 * The span carries a native `title` as well as the bubble, and the two are not
 * belt and braces. This row is a Panel's header, Panel is `overflow-hidden`,
 * and Tooltip's bubble hangs above its anchor in the normal flow — so the
 * sentence explaining why stashing is refused was painted outside the panel
 * and clipped away unread, which is the trap Tooltip's own comment names for
 * scroll containers met from the other side. The bubble is the half a screen
 * reader hears through aria-describedby, the only route into a control that
 * has dropped its pointer events; the `title` is the half a pointer can read,
 * because the browser draws it outside the page where nothing clips it.
 * Neither reaches both readers alone. CommitDetails' refused actions are drawn
 * the same way, six pixels from the same panel edge.
 *
 * The accessible name begins with the words on the button. It used to read
 * "Stash the changes in the work tree" over a button saying "Stash changes",
 * so somebody driving by voice said what they could see and matched nothing,
 * and somebody hearing the name could not match it to what a colleague was
 * pointing at — WCAG 2.5.3, which the axe gate cannot see because
 * label-content-name-mismatch ships disabled. Naming the work tree still earns
 * its place: this is the only control here that acts on the working directory
 * rather than on the stack below it.
 *
 * The ellipsis is the promise the menus in this same panel already make: the
 * click opens the dialog that asks what to keep, and does not stash on the
 * press. It is left off the accessible name, where CommitDetails leaves it
 * off too — a screen reader reading three dots aloud is noise, and the name
 * has to stay a phrase a voice-control user can say.
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
      aria-label="Stash changes in the work tree"
    >
      Stash changes…
    </Button>
  );

  if (stashRefusal === undefined) {
    return button;
  }

  // The span is what both of them hang on: it is the element that still has
  // pointer events once the button has given them up.
  return (
    <span className="inline-flex" title={stashRefusal}>
      <Tooltip label={stashRefusal}>{button}</Tooltip>
    </span>
  );
}

/**
 * What keeps a row's menu out of the way until it is wanted.
 *
 * Opacity and pointer events move together, and that pairing is the whole
 * point: transparent alone leaves a button nobody can see and everybody can
 * click, floating over the right-hand end of a row whose own click opens the
 * stash below. A press that landed in what reads as blank space opened a menu
 * instead of the patch, and the menu that opened was the one holding Drop.
 *
 * The third pair is for the menu itself. Its popover is in the browser's top
 * layer, nowhere near this row in the document, so `focus-within` is false for
 * as long as the menu has focus — without it the trigger fades out from under
 * the menu it opened, and the pointer heading for "Drop…" crosses a button
 * that is no longer there.
 *
 * ChangeList and RefSidebar carry the same constant, written out rather than
 * shared for the reason ChangeList's copy records: the three use different
 * group names, and the shared home for it — web/src/lib — belongs to none of
 * the three files. This is the third copy, which is the point at which it
 * should be lifted out.
 */
const REVEALED_ON_ATTENTION = [
  'pointer-events-none opacity-0 transition-opacity transition-instant',
  'group-hover/row:pointer-events-auto group-hover/row:opacity-100',
  'group-focus-within/row:pointer-events-auto group-focus-within/row:opacity-100',
  'group-has-[[aria-expanded=true]]/row:pointer-events-auto',
  'group-has-[[aria-expanded=true]]/row:opacity-100',
].join(' ');

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
        // The two tokens the design system keeps apart, kept apart here: this
        // row painted both states with `bg-sunken`, so hovering any stash drew
        // it exactly as the one whose patch is open below, and hovering the
        // open one answered with nothing at all. `--color-hover` lightens and
        // `--color-selected` lightens further, which is the direction every
        // other list in the workbench moves in.
        className={cx(
          'flex w-full min-w-0 flex-col gap-0.5 px-3 py-2 text-left',
          'transition-colors transition-instant outline-none',
          'focus-visible:focus-ring',
          current ? 'bg-selected' : 'hover:bg-hover',
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
          {/* The clock the visible form drops. Past a week formatRelativeTime
              is a bare date, so this hover used to hand back the string
              underneath it; a stack of stashes made in one afternoon is the
              ordinary case, and the hour is the only thing that orders them. */}
          <span title={formatExactTime(made)}>{formatRelativeTime(made, new Date())}</span>
        </span>
      </button>

      {items.length > 0 && (
        <div className={cx('absolute top-1.5 right-2', REVEALED_ON_ATTENTION)}>
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
