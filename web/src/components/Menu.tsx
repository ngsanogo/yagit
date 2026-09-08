import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';

import { cx } from '../lib/cx';

/**
 * The actions of one row, behind one button.
 *
 * SegmentedControl's own doc names the moment this becomes the answer: past
 * two or three short options they stop fitting side by side. A reference in
 * the sidebar has four things that can be done to it and room on screen for
 * one, so three of them live here.
 *
 * Built on the popover API rather than on an absolutely positioned div, and
 * the reason is the list it opens over: the references scroll inside their
 * panel, so a menu in that flow would be clipped by the first `overflow:auto`
 * above it. A popover is in the browser's top layer, where nothing clips it —
 * and Escape, click-anywhere-else, focus restoration and stacking come with it
 * rather than being reimplemented here. ADR 0018 is that decision, and what it
 * was decided against.
 *
 * What does NOT come with it is placement: the top layer has no idea where the
 * button was. That is measured on open, below the trigger where there is room
 * and above it where there is not.
 *
 * For a handful of items. A menu long enough to need scrolling is a list, and
 * a list belongs in a panel where it can be searched — but a short window
 * makes three items too many, so the placement caps it to the room it has and
 * lets it scroll rather than painting its last item past the bottom of a
 * screen the page cannot scroll to reach.
 */

export type MenuItem = {
  /** Stable across renders — it is the React key and the focus target. */
  id: string;
  label: string;
  onSelect: () => void;
  /**
   * Marks the item that destroys something, so it does not read like the
   * others. It still opens whatever confirmation the operation needs: the
   * colour is a warning, not the safeguard.
   */
  danger?: boolean;
} & (
  | {
      /**
       * Kept on screen and refused, for an action that exists on this row but
       * not right now — deleting the branch that is checked out. An action that
       * will never apply to the row belongs off the list, not on it greyed out.
       *
       * Refused, not removed from the menu: the arrows still land on it and a
       * screen reader still announces it, because the only way to read a menu
       * is to walk it, and an item the walk skips is an item nobody can be
       * told exists. See aria-disabled on the button below.
       *
       * `reason` is required because a grey item with nothing to say is how
       * people learn that the menu is broken rather than that HEAD is
       * detached. It is drawn on the item, not hidden in a tooltip: a menu
       * clips overflow, and a sentence that is worth reading is worth seeing.
       */
      disabled: true;
      reason: string;
    }
  | { disabled?: false; reason?: never }
);

/**
 * Builds a menu item. Pass a reason to refuse it; omit the reason to offer it.
 *
 * The only way to disable an item: a boolean with no reason does not compile,
 * and a conditional spread of `{ disabled: true }` is not assignable to the
 * union above — TypeScript widens it to `disabled?: true`. This helper is
 * where the two shapes meet.
 */
export function menuItem(
  spec: {
    id: string;
    label: string;
    onSelect: () => void;
    danger?: boolean;
  },
  reason?: string,
): MenuItem {
  if (reason === undefined) {
    return spec;
  }

  // The union requires a reason to be PRESENT; only this can require it to say
  // something. `menuItem(spec, '')` and a reason interpolated from a field
  // that came back empty both type-check, and both produce exactly what the
  // union was written to make impossible: a grey item with a zero-height span
  // under it and nothing to read.
  //
  // Thrown rather than quietly offered or quietly refused. Offering it runs an
  // action the caller meant to withhold; refusing it silently is the broken
  // menu again, one build later and harder to find. This is a programming
  // error, it is deterministic, and it fires the first time the item is drawn.
  const said = reason.trim();
  if (said === '') {
    throw new Error(`menu item ${spec.id} is refused with no reason to show for it`);
  }

  return { ...spec, disabled: true, reason: said };
}

interface MenuProps {
  /**
   * The trigger's accessible name — "More actions for main".
   *
   * Required, and not defaulted to "More": a sidebar holding thirty rows would
   * then hold thirty buttons a screen reader cannot tell apart.
   */
  label: string;
  items: MenuItem[];
  className?: string;
}

/** How far the menu is kept from the edge of the window. */
const VIEWPORT_MARGIN = 8;

/** The gap between the trigger and the menu, so the two read as two things. */
const TRIGGER_GAP = 4;

/*
 * Both are geometry this component writes into an inline style, not visual
 * values a class could carry, so they are here rather than in tokens.css —
 * which no JavaScript in this project reads, by the decision recorded in its
 * own doc.
 */

export function Menu({ label, items, className }: MenuProps) {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const menuId = useId();

  /** Which end the next open lands on: Up opens a menu from the bottom. */
  const openAt = useRef<'first' | 'last'>('first');

  /**
   * Whether the menu was open when the pointer went down on the trigger.
   *
   * The browser dismisses an auto popover on pointerup — before the click ever
   * reaches the handler below. A trigger that toggled React state would
   * therefore read "closed" on the click that had just closed it and open it
   * straight back up: the button could never close its own menu. Pointerdown
   * is the last moment the truth is still observable.
   */
  const openOnPointerDown = useRef(false);

  /**
   * Where the trigger was when the menu was placed against it.
   *
   * The dismissal below asks whether the row has moved, not whether something
   * scrolled. Those are not the same question: bringing the row into view is
   * itself a scroll, and its event arrives a frame later — after the menu it
   * opened is already on screen. Measured position answers what the event
   * cannot.
   */
  const anchoredAt = useRef({ x: 0, y: 0 });

  /**
   * What the open menu's height depends on, as one string.
   *
   * The dependency of the placement effect below, in place of items.length —
   * which counts the rows and says nothing about how tall each one is. A
   * refused item draws its reason under its label, and a reason that arrives
   * or leaves changes the menu's height while the count stays put: RemoteBar
   * rebuilds its three items on every working-directory poll, so a menu opened
   * while the status is still loading holds three "Reading the work tree…"
   * sentences that vanish a moment later. Placed for the tall box
   * and never re-placed, it hangs off the button it was anchored to.
   *
   * Newlines as the separator, because these are single-line interface
   * strings: the alternative is a signature that two different menus can
   * collide on, and a collision is a placement silently skipped.
   */
  const shape = items.map((item) => `${item.id}\n${item.label}\n${item.reason ?? ''}`).join('\n\n');

  /**
   * Closes the menu from inside the component, and puts focus back on the
   * trigger: an item taken, Tab, a second click on the button, a scroll.
   *
   * The other half of the closing is the browser's own light dismiss — Escape,
   * a click elsewhere, another menu opening — which restores focus itself and
   * reports back through the toggle event below. Those closes must NOT pull
   * focus to the trigger: the click that dismissed the menu was on its way to
   * something else.
   *
   * Memoized because the effect that dismisses on a scroll depends on it, and
   * a new function every render would tear those listeners down and put them
   * back on every one.
   */
  const close = useCallback(() => {
    setOpen(false);
    // Without a scroll: the trigger is on screen in every case that reaches
    // here except the one where the page scrolled out from under the menu, and
    // dragging that row back would undo the scroll the reader asked for.
    triggerRef.current?.focus({ preventScroll: true });
  }, []);

  // Shown and placed before the browser paints, so the menu never appears in
  // the corner first and jumps to the button afterwards. Both halves have to
  // be in the same layout effect: the size is only measurable once it is
  // displayed, and the placement depends on the size.
  //
  // Re-run when the menu's contents change as well as when it opens: a list
  // that grows, or an item that gains a sentence, while it is open is a menu
  // whose height no longer matches the geometry it was placed with — hanging
  // off the edge of a window the page cannot scroll to reach.
  useLayoutEffect(() => {
    const popover = popoverRef.current;
    const trigger = triggerRef.current;
    if (popover === null || trigger === null) {
      return;
    }

    if (!open) {
      if (popover.matches(':popover-open')) {
        popover.hidePopover();
      }
      return;
    }

    // Only the run that opens it puts focus in it. A re-placement caused by
    // the list changing must not drag the reader back to the first item.
    const opening = !popover.matches(':popover-open');
    if (opening) {
      popover.showPopover();
    }

    // The window, not the viewport: innerWidth and innerHeight include the
    // scrollbar gutters, and a menu placed against those is drawn half under
    // the scrollbar.
    const { clientWidth: windowWidth, clientHeight: windowHeight } = document.documentElement;

    const anchor = trigger.getBoundingClientRect();
    // Measured with no cap, so what is measured is the menu's own height and
    // not the one a previous placement imposed on it.
    popover.style.maxHeight = '';
    const menu = popover.getBoundingClientRect();

    // Below the button, unless the window has no room for it there and does
    // above. Not "below when past the halfway line": a two-item menu near the
    // bottom of a tall window still fits below it, and flipping it would move
    // the first item away from the pointer for nothing. When neither side can
    // hold it — a short window, a laptop with devtools open — it takes the
    // roomier one and scrolls, because a menu whose last item is painted off
    // the bottom of the screen cannot be reached at all: the page does not
    // scroll the top layer.
    const roomBelow = windowHeight - anchor.bottom - TRIGGER_GAP - VIEWPORT_MARGIN;
    const roomAbove = anchor.top - TRIGGER_GAP - VIEWPORT_MARGIN;
    const goesBelow =
      menu.height <= roomBelow || (menu.height > roomAbove && roomBelow >= roomAbove);

    const room = goesBelow ? roomBelow : roomAbove;
    const height = Math.min(menu.height, room);
    const top = goesBelow ? anchor.bottom + TRIGGER_GAP : anchor.top - TRIGGER_GAP - height;

    // Right edges aligned, which is where a row's actions sit. Pulled back
    // when that would put it off the right or the left of the window — a
    // narrow panel on a narrow screen. The clamp to the margin is applied
    // last, so a menu wider than the window starts at the left edge rather
    // than off it.
    const left = Math.max(
      VIEWPORT_MARGIN,
      Math.min(anchor.right - menu.width, windowWidth - menu.width - VIEWPORT_MARGIN),
    );

    popover.style.maxHeight = `${room}px`;
    popover.style.top = `${top}px`;
    popover.style.left = `${left}px`;
    anchoredAt.current = { x: anchor.x, y: anchor.y };

    if (opening) {
      // Focus lands on an item here rather than in an effect of its own: the
      // items become focusable the moment the popover reaches the top layer,
      // which is the line above that put it there.
      focusItem(popover, openAt.current === 'first' ? 0 : -1);
    }
  }, [open, shape]);

  // The browser closes an auto popover by itself — Escape, a click anywhere
  // else, another menu opening — and tells React nothing about it. Without
  // this the button would keep claiming aria-expanded="true" over a menu that
  // is gone.
  useEffect(() => {
    const popover = popoverRef.current;
    if (popover === null) {
      return;
    }

    const handleToggle = (event: ToggleEvent) => {
      if (event.newState === 'closed') {
        setOpen(false);
      }
    };

    popover.addEventListener('toggle', handleToggle);
    return () => popover.removeEventListener('toggle', handleToggle);
  }, []);

  // The menu is placed once, against where the row was at the time. Anything
  // that moves the row afterwards leaves it pointing at nothing, so it closes
  // instead of following: a menu that tracked its anchor would still have to
  // answer for the anchor scrolling out of its panel, and "the actions of that
  // row, over there, above a different row" is not an answer.
  useEffect(() => {
    if (!open) {
      return;
    }

    const closeIfMoved = (event?: Event) => {
      const trigger = triggerRef.current;
      if (trigger === null) {
        return;
      }

      // Only a scroller the trigger is inside can move it. Everything else on
      // the page — the commit list, the diff, the log — is reached by the
      // capture below and answered here, before measuring: reading a rect
      // flushes layout, and doing that on every frame of every unrelated flick
      // is a scroll janked by a menu that is not even involved.
      //
      // It also settles the menu's own scrolling: the popover does not contain
      // the trigger, so a wheel inside a long menu is not a reason to close it.
      if (event !== undefined && event.target instanceof Node && !event.target.contains(trigger)) {
        return;
      }

      const anchor = trigger.getBoundingClientRect();
      if (
        Math.abs(anchor.x - anchoredAt.current.x) < 1 &&
        Math.abs(anchor.y - anchoredAt.current.y) < 1
      ) {
        return;
      }
      close();
    };

    // Capture: the panel that scrolls is an ancestor of the trigger, and a
    // scroll inside one does not bubble to the window.
    window.addEventListener('scroll', closeIfMoved, true);
    window.addEventListener('resize', closeIfMoved);

    return () => {
      window.removeEventListener('scroll', closeIfMoved, true);
      window.removeEventListener('resize', closeIfMoved);
    };
  }, [open, close]);

  const handleMenuKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        moveFocus(popoverRef.current, 1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        moveFocus(popoverRef.current, -1);
        break;
      case 'Home':
        event.preventDefault();
        focusItem(popoverRef.current, 0);
        break;
      case 'End':
        event.preventDefault();
        focusItem(popoverRef.current, -1);
        break;
      case 'Tab':
        // Closed rather than trapped. A menu is a detour, not a place to live:
        // focus goes back to the trigger, and a forward Tab then carries on
        // from there into the page behind it.
        close();
        // Backwards is where that stops being true. Shift+Tab from the trigger
        // would step over it to whatever precedes the row, so "open the menu,
        // change my mind, back out" would lose the button it was opened with.
        // Stopping here leaves the reader where they started.
        if (event.shiftKey) {
          event.preventDefault();
        }
        break;
      default:
        break;
    }
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        // A menu with nothing in it opens onto nothing: focus would have no
        // item to land on, and the arrow keys — which this handler only reads
        // while the menu is closed — would then be inert with a popover still
        // on screen. Refused at the button instead, where it is visible.
        disabled={items.length === 0}
        onPointerDown={() => {
          openOnPointerDown.current = open;
        }}
        onClick={(event) => {
          // Enter and Space on a button arrive here as a click with a detail
          // of 0, and no pointer went down for them: nothing dismissed the
          // menu behind their back, so they can read the state itself. Only a
          // real pointer's click has to fall back on what was recorded above.
          const wasOpen = event.detail === 0 ? open : openOnPointerDown.current;

          if (wasOpen) {
            close();
            return;
          }
          openAt.current = 'first';
          setOpen(true);
        }}
        onKeyDown={(event) => {
          // The arrows open the menu at the end they point to, which is what
          // every menu button does and what aria-haspopup announced it would.
          // Enter and Space arrive as a click and are handled there.
          if (open || (event.key !== 'ArrowDown' && event.key !== 'ArrowUp')) {
            return;
          }
          event.preventDefault();
          openAt.current = event.key === 'ArrowDown' ? 'first' : 'last';
          setOpen(true);
        }}
        className={cx(
          'inline-flex h-7 w-7 items-center justify-center rounded-sm',
          'text-ink-muted transition-colors transition-instant',
          'hover:bg-hover hover:text-ink focus-visible:focus-ring outline-none',
          // Dimmed like every other refused control in this design system. A
          // button that is refused and looks exactly like one that is not is
          // worse than either: it invites the click it will not answer.
          'disabled:pointer-events-none disabled:opacity-45',
          open && 'bg-selected text-ink',
          className,
        )}
      >
        <EllipsisGlyph />
      </button>

      <div
        ref={popoverRef}
        id={menuId}
        // "auto" is what brings Escape and light dismiss. "manual" would leave
        // both to be written here, and written worse.
        popover="auto"
        role="menu"
        aria-label={label}
        onKeyDown={handleMenuKeyDown}
        className={cx(
          // `inset-auto` and no offsets: the layout effect above writes them.
          // A popover's own position is the whole viewport — `fixed` with
          // `inset: 0` — which left the right edge fighting the left one.
          // max-w, and it is not decoration: a fixed popover shrinks to fit,
          // so without a ceiling the longest reason sentence IS the menu's
          // width — "main follows no branch on a remote, so there is nothing
          // to force-update. Publish it first." is one 400px line off a 28px
          // trigger, and `text-pretty` cannot wrap what is never made to. The
          // same ceiling Tooltip uses, for the same sentence-shaped reason.
          'fixed inset-auto m-0 max-w-xs min-w-40 overflow-y-auto rounded-md p-1',
          'border border-line-strong bg-raised shadow-popover',
        )}
      >
        {/* Only while it is open. A sidebar of thirty rows is thirty of these,
            and a closed one has nothing to say: display:none already keeps it
            out of the accessibility tree, and not building it keeps it out of
            the document as well. */}
        {open &&
          items.map((item) => (
            <button
              key={item.id}
              type="button"
              role="menuitem"
              // Roving focus: the menu is entered by the arrow keys and by the
              // open, never by Tab — which closes it.
              tabIndex={-1}
              // aria-disabled, never the disabled attribute. A disabled button
              // cannot take focus, and the arrows are the only way through a
              // menu — so the attribute would hide the refused action from
              // exactly the reader who most needs to be told it is there.
              aria-disabled={item.disabled === true ? true : undefined}
              // The label alone. A menuitem is named by its contents, so the
              // reason drawn inside the button joined the accessible name the
              // moment it appeared: the item announced "Delete… HEAD is on
              // main, so it cannot be deleted" and aria-describedby then read
              // the same sentence out a second time. It also renamed every
              // refused item in the application — getByRole('menuitem', {
              // name: 'Delete…', exact: true }) stopped matching, and only
              // Playwright's substring default hid that from the suite.
              aria-label={item.disabled === true ? item.label : undefined}
              aria-describedby={item.disabled === true ? `${menuId}-${item.id}-reason` : undefined}
              onPointerMove={(event) => {
                // The pointer moves focus rather than lighting a second row:
                // hover and focus are one highlight here, so there is never a
                // menu with the mouse over one item and the keyboard on
                // another and no way to tell which Enter would take.
                //
                // pointermove, not pointerenter. A menu opened by the keyboard
                // lands under whatever the cursor was already resting on, and
                // the boundary event that fires for that would throw away the
                // item ArrowUp had just chosen. Only a pointer that actually
                // moves is a pointer being used.
                if (document.activeElement !== event.currentTarget) {
                  event.currentTarget.focus();
                }
              }}
              onClick={() => {
                // Refused, and it says so rather than closing: a menu that
                // shut on a click that did nothing would read as an action
                // that silently failed.
                if (item.disabled === true) {
                  return;
                }
                // Closed before the action runs, and focus goes back to the
                // trigger. An action that opens a dialog would otherwise put
                // one in front of a menu still on top of it — the one nesting
                // the design system does not allow.
                close();
                item.onSelect();
              }}
              className={cx(
                'flex w-full flex-col items-start rounded-sm px-2.5 py-1.5 text-left text-xs',
                'transition-colors transition-instant outline-none',
                itemTone(item),
              )}
            >
              {item.label}
              {item.disabled === true && (
                <span
                  id={`${menuId}-${item.id}-reason`}
                  className="mt-0.5 text-2xs text-pretty text-ink-subtle"
                >
                  {item.reason}
                </span>
              )}
            </button>
          ))}
      </div>
    </>
  );
}

/**
 * How one item is coloured: offered, offered and destructive, or refused.
 *
 * The refused row is the one worth explaining, because it used to be an
 * opacity and that was a bug rather than a style. `aria-disabled:opacity-45`
 * dimmed the whole button, reason sentence included, and 45% of ANY ink over
 * this menu's own surface is about 1.7:1 in the light theme — so the sentence
 * the design insists on drawing rather than hiding in a tooltip was drawn and
 * could not be read. Ink tokens say the same thing and stay legible: muted for
 * the label and subtle for the reason clear AA against both the menu surface
 * and the focus highlight, in both themes.
 *
 * A refused item is also not red, whatever its `danger` flag says. Nothing is
 * about to be destroyed — that is the whole content of the refusal — and a red
 * item that does nothing spends the one colour reserved for the item that
 * does.
 *
 * The highlight IS the focus indicator, and a ring is not: this menu clips its
 * own padding, so an outline drawn 2px outside an item would be cut off on the
 * first and the last.
 */
function itemTone(item: MenuItem): string {
  if (item.disabled === true) {
    return 'text-ink-muted focus:bg-selected';
  }
  if (item.danger === true) {
    return 'text-danger focus:bg-danger-soft';
  }
  return 'text-ink focus:bg-selected';
}

/**
 * The items of an open menu.
 *
 * All of them, refused ones included. A refused item carries aria-disabled
 * rather than the disabled attribute precisely so that it can still take
 * focus: arrow keys are the only way through a menu, so an item they step over
 * is one a screen reader can never reach and never announce. It is reachable
 * and it does nothing, which is the state the row is actually in.
 */
function focusableItems(menu: HTMLElement | null): HTMLButtonElement[] {
  return Array.from(menu?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? []);
}

/** Moves focus to an item by index, wrapping at both ends. */
function focusItem(menu: HTMLElement | null, index: number) {
  const items = focusableItems(menu);
  if (items.length === 0) {
    return;
  }
  // Two moduli, not `(index + length) % length`: this one is also right below
  // -length, and End reaches the last item by passing -1 through it.
  items[((index % items.length) + items.length) % items.length]?.focus();
}

/** Moves focus one item along from wherever it is. */
function moveFocus(menu: HTMLElement | null, step: -1 | 1) {
  const items = focusableItems(menu);
  const from = items.indexOf(document.activeElement as HTMLButtonElement);
  // Focus not on an item yet reads as one place before the first, so Down
  // lands on the first and Up wraps to the last.
  focusItem(menu, from < 0 ? (step > 0 ? 0 : -1) : from + step);
}

function EllipsisGlyph() {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor" aria-hidden="true">
      <circle cx="3" cy="7" r="1.2" />
      <circle cx="7" cy="7" r="1.2" />
      <circle cx="11" cy="7" r="1.2" />
    </svg>
  );
}
