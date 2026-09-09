import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';

import { cx } from '../lib/cx';
import { Toast, type ToastTone } from './Toast';

/** What a call site asks for. Everything else about a toast is the host's. */
export interface ToastRequest {
  tone: ToastTone;
  title: string;
  detail?: ReactNode;
  /**
   * The way out of what the toast reports, as one control.
   *
   * A danger toast never expires, so a failure with a known next step — a
   * push refused because the branch is behind, a stash that needs the file
   * opened — otherwise ends as a card that names the problem and offers
   * nothing. One control and not a row of them: a notification is read by
   * somebody who was looking at something else, and a second choice on it is
   * a second thing to read before they can go back to it.
   */
  action?: ReactNode;
}

interface ToastMessage extends ToastRequest {
  id: number;
  /** How many times this notification arrived with nothing else in between. */
  repeats: number;
}

interface ToastHostContextValue {
  push: (message: ToastRequest) => void;
  /**
   * Says something to a screen reader and draws nothing.
   *
   * Separate from push, and the difference is who the message is for. A toast
   * is for an outcome the user has to SEE — it survives the screen they were
   * on, it can carry git's stderr, and a danger one waits until it is
   * dismissed. An announcement is for an outcome a sighted user has already
   * seen happen: three files moved from Unstaged to Staged, the list under
   * the pointer redrew itself. Drawing a card for that would be a card in
   * front of the work every time the work succeeds, so it is said and not
   * drawn.
   *
   * Every toast goes through here too, on its way to being drawn. See the
   * regions themselves for why the saying cannot be left to the card.
   */
  announce: (message: string) => void;
}

const ToastHostContext = createContext<ToastHostContextValue | null>(null);

/**
 * How long a toast nobody has to act on stays on screen.
 *
 * Long enough to read a short sentence twice, which is what "Committed" is.
 * A danger toast is exempt: it carries the raw stderr of a command that
 * failed, and taking that away on a timer would be the interface deciding the
 * user had finished reading their only copy of it.
 */
const TRANSIENT_MS = 4_000;

/**
 * How many toasts are drawn at once. The oldest beyond it is dropped.
 *
 * Two, and the number is a height rather than a taste. The stack is anchored
 * to the bottom and grows upwards; a failure toast is a title, a command, an
 * exit code and however much stderr git chose to write, so a quarter of a
 * window each is the good case. Uncapped, the third failure reaches the top
 * of the screen and the fourth goes past it — and what goes past it first is
 * the top of the OLDEST toast, which is the corner its dismiss button is in.
 * On the way there the stack covers the header, where Fetch, Pull and Push
 * are: the control that caused the error, behind the message about it.
 *
 * Two rather than one because the pair that matters is the failure that just
 * happened and the one before it — a fetch that failed, and then the push
 * that failed for the same reason. Nothing is lost by dropping the rest:
 * every command yagit runs is in the command log panel with its exit code and
 * stderr, which is the copy that is not on a timer and not in a corner.
 */
const MAX_STACK = 2;

/**
 * The stack after one more notification arrives.
 *
 * A function on its own, because the two decisions in it — how many toasts
 * may be on screen and when two of them are one — are policy, and policy
 * reachable only through a mounted component is policy nobody checks.
 */
export function nextStack(
  current: readonly ToastMessage[],
  message: ToastRequest,
  id: number,
): ToastMessage[] {
  const newest = current[current.length - 1];

  /*
   * Collapsed rather than stacked when the newest toast is this one again.
   * Three clicks on a remote that is not there are otherwise three identical
   * cards, and the third pushes the first off the top of the window.
   *
   * Matched on the tone and the title, not on the detail: the detail is a
   * React node built afresh for every failure and so never equal to itself,
   * and the title is already the sentence that names the operation and what
   * it was aimed at. "Could not fetch from origin" twice is that request
   * refused twice. The newest detail and action replace the older ones,
   * because what the user wants to read is the attempt they have just made.
   */
  if (newest !== undefined && newest.tone === message.tone && newest.title === message.title) {
    return [...current.slice(0, -1), { ...message, id: newest.id, repeats: newest.repeats + 1 }];
  }

  return [...current, { ...message, id, repeats: 1 }].slice(-MAX_STACK);
}

/**
 * A small stack of notifications, fixed to the bottom corner.
 *
 * Mutations that fail outside the form they came from — closing a tab, for
 * example — have nowhere else to put their error. A toast is honest about
 * being transient: the user can dismiss it and keep working.
 *
 * The BOTTOM corner, and that is the whole reason this note exists. The
 * header's actions live in the top right, so a stack anchored there covers
 * the controls the user reaches for next — and a danger toast, which never
 * expires, covered them for good.
 *
 * Anchored at the bottom, the box grows upwards and the last child is the one
 * in the corner. So the list is rendered in the order it was pushed: the
 * newest toast always appears in the same place, and the older ones move out
 * of its way. MAX_STACK is the other half of that promise — bottom-anchored
 * only keeps the header clear while the stack is shorter than the window.
 *
 * The stack is drawn in the browser's top layer, as a popover. A z-index
 * cannot reach it: a modal <dialog> puts its backdrop in the top layer, so
 * the toast raised by an operation started FROM a dialog — a plan that git
 * refused, with the explanation in its stderr — was painted under the scrim
 * and its dismiss button could not be clicked at any depth. This is the
 * mechanism Menu already uses and ADR 0018 already argued for, applied to the
 * one overlay that was left behind on a number.
 */
export function ToastHost({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  /*
   * What is said, in two regions rather than one.
   *
   * A screen reader announces a CHANGE to a live region, and writing the text
   * a region already holds is not one: "Staged 3 files" twice in a row — two
   * clicks on the same button — would be announced once, and the second click
   * would be the one that reported nothing. Written to a different region
   * each time, both land. The other region is cleared rather than left
   * standing, so nothing is re-read when the tree is walked; a removal is not
   * an addition, and clearing announces nothing by itself.
   *
   * The same alternation is what makes a repeat of a toast land. The stack
   * collapses "Could not fetch from origin" into the card above it and shows a
   * count, which is right on screen and is no change at all to a region
   * holding that sentence already — so the second refusal, and the third,
   * would be the ones nothing said.
   */
  const [announcements, setAnnouncements] = useState<readonly [string, string]>(['', '']);
  const spokenLast = useRef(0);

  // A ref, not state. Two toasts pushed before React re-renders — a close that
  // fails on two tabs at once, which is exactly when this component earns its
  // place — would both read the same value out of state and come out with the
  // same key. React then draws one of them, and dismissing it dismisses the
  // other. A counter that is not rendered has no business being state, and
  // keeping it out of the dependency list is what makes push stable, so no
  // consumer re-renders because a toast appeared somewhere else.
  const nextId = useRef(0);

  const stack = useRef<HTMLDivElement>(null);

  /*
   * How many notifications have arrived, which is not how many are on screen.
   *
   * State rather than another ref, because the effect below has to run once
   * per arrival and only a rendered value can make it. The length of the
   * stack cannot: two of the three ways a toast arrives leave it exactly as
   * it was — a repeat collapses into the card above it, and a push at
   * MAX_STACK drops the oldest as it adds the newest. Counting the stack
   * instead is how the run of failures this whole file is about would be the
   * one case that never reclaimed the top layer.
   */
  const [arrivals, setArrivals] = useState(0);

  const dismiss = useCallback((id: number) => {
    setToasts((current) => current.filter((toast) => toast.id !== id));
  }, []);

  const announce = useCallback((message: string) => {
    const region = spokenLast.current === 0 ? 1 : 0;
    spokenLast.current = region;
    setAnnouncements(region === 0 ? [message, ''] : ['', message]);
  }, []);

  const push = useCallback(
    (message: ToastRequest) => {
      // Allocated outside the updater, which React may run twice: the id has
      // to come from somewhere that is not asked the same question twice. A
      // collapsed toast keeps the id it already had and this one goes unused,
      // which costs a gap in a sequence nobody reads.
      const id = nextId.current;
      nextId.current += 1;
      setToasts((current) => nextStack(current, message, id));
      setArrivals((count) => count + 1);
      // Said as well as drawn, from a region that is not the one being drawn
      // into. See the stack's own note: the element the cards live in leaves
      // the top layer and comes back to climb over a dialog, and a live
      // region that can be taken out of the document is a live region that
      // will one day be out of it on the frame that mattered. The title is
      // the sentence that names the operation and what it was aimed at; the
      // detail is git's stderr, which reaches a reader from the card, where
      // it is also selectable and still on screen a minute later.
      announce(message.title);
    },
    [announce],
  );

  /*
   * Claims the top layer, and claims it back when a modal has taken it.
   *
   * The stack is shown while it is still empty, so that a card is drawn into
   * an element that is already where it belongs rather than one arriving with
   * the card inside it.
   *
   * After that it stays where it is, with one exception. The top layer is
   * ordered by when each element entered it, so a dialog opened later sits
   * above a stack shown earlier — backdrop and all — and the notification
   * raised by an operation started FROM that dialog is painted under the
   * scrim, which is the failure this popover exists to end. Leaving and
   * re-entering puts the newest one back on top.
   *
   * Only then, and not on every arrival, because hiding a popover that holds
   * focus hands focus back to wherever it came from: a second failure would
   * throw the user off the first one's Dismiss button as they reached for it.
   * Not worth paying while there is nothing above us to climb over.
   *
   * What this used to cost as well was the announcement — the cards are the
   * only copy of a notification's text, and taking them out of the document
   * on the frame one landed is a sentence a reader never hears. That is why
   * push() says the title through the regions below instead, which no part of
   * this dance can reach. The two ways a toast reports itself are now the
   * card, for as long as it is on screen, and a sentence in a region that has
   * been in the document since the page loaded.
   *
   * `:popover-open` and `:modal` are both inside the floor ADR 0018 already
   * put under this project. An engine that cannot parse the first cannot run
   * showPopover() either — they shipped together in all three — and `:modal`
   * is older than both. So there is no browser that reaches this line and
   * fails at it; a try/catch here would be catching a case that would already
   * have failed a line earlier, and silencing it.
   */
  useEffect(() => {
    const element = stack.current;
    if (element === null) {
      return;
    }

    if (!element.matches(':popover-open')) {
      element.showPopover();
      return;
    }

    // Nothing has arrived, so this is the mount running a second time — which
    // is what React does in development, and there is nothing to reclaim yet.
    if (arrivals === 0 || document.querySelector(':modal') === null) {
      return;
    }
    element.hidePopover();
    element.showPopover();
  }, [arrivals]);

  const value = useMemo(() => ({ push, announce }), [push, announce]);

  return (
    <ToastHostContext.Provider value={value}>
      {children}
      <div
        ref={stack}
        // "manual" and not "auto": light dismiss would close the stack on the
        // first click anywhere, and the click a user makes after a failure is
        // usually the retry — which would take the explanation away as they
        // reached for it. Escape is not wanted here either; the cross is.
        popover="manual"
        // No aria-live of its own. It used to carry one, as the always-present
        // region an insertion is reliably announced from — and then it became
        // the element that leaves the top layer and re-enters it to climb over
        // a dialog, which is a region that can be out of the document on the
        // one frame a sentence landed in it. A region has to be somewhere
        // nothing moves, so it is the pair below. What is left here is paint
        // and the two controls a card carries, and each card still declares
        // the status or alert it is.
        className={cx(
          // A popover comes with a UA rule of its own — `inset: 0`, a border,
          // padding, `overflow: auto` and an opaque background. The offsets
          // are cancelled edge by edge rather than with `inset-auto`, because
          // `inset` is a shorthand over the two edges that are wanted and the
          // winner of that pair is decided by the order Tailwind happens to
          // emit them in.
          'pointer-events-none fixed top-auto right-3 bottom-3 left-auto',
          'm-0 flex flex-col gap-2 overflow-visible bg-transparent p-0',
        )}
      >
        {toasts.map((toast) => (
          <div key={toast.id} className="pointer-events-auto">
            <Toast
              tone={toast.tone}
              title={toast.title}
              detail={toast.detail}
              action={toast.action}
              repeats={toast.repeats}
              onDismiss={() => dismiss(toast.id)}
            />
            {/* Keyed by the repeat count as well as the id, so a toast that
                says the same thing again starts its four seconds again rather
                than inheriting the remains of the first one's. */}
            {toast.tone !== 'danger' && (
              <Expiry key={`${toast.id}:${toast.repeats}`} id={toast.id} onExpire={dismiss} />
            )}
          </div>
        ))}
      </div>

      {/* Outside the popover on purpose, and it is now the only place anything
          is announced from: a popover is display:none until it is shown, and
          a live region that is not in the document is a live region nothing is
          announced from. Everything the host says goes through here — what
          announce() was asked to say, and the title of every toast. */}
      <div className="sr-only">
        <p aria-live="polite" aria-atomic="true">
          {announcements[0]}
        </p>
        <p aria-live="polite" aria-atomic="true">
          {announcements[1]}
        </p>
      </div>
    </ToastHostContext.Provider>
  );
}

/**
 * The timer for one toast, as a component rather than an effect in the host.
 *
 * Mounted with the toast and unmounted with it, so the timeout is cleared by
 * React's own lifecycle. Held in the host instead, it would be a map of
 * identifiers to handles that has to be kept in step with the list by hand —
 * and the failure mode of getting that wrong is a toast that dismisses the one
 * that replaced it.
 */
function Expiry({ id, onExpire }: { id: number; onExpire: (id: number) => void }) {
  useEffect(() => {
    const handle = window.setTimeout(() => onExpire(id), TRANSIENT_MS);
    return () => window.clearTimeout(handle);
  }, [id, onExpire]);

  return null;
}

export function useToast(): ToastHostContextValue {
  const context = useContext(ToastHostContext);
  if (context === null) {
    throw new Error('useToast must be used inside ToastHost');
  }
  return context;
}
