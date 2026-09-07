import { useEffect, useRef, useState } from 'react';

/**
 * Putting one string on the clipboard, and saying whether it got there.
 *
 * The clipboard is the one browser API this interface uses that is routinely
 * refused — an insecure context, a revoked permission — and it refuses
 * silently. A button that looks the same whether it worked or not is worse
 * than no button, so the refusal is a state of its own and the cause stays in
 * the console.
 *
 * A hook rather than a component: the two things yagit copies — a git command
 * and a SHA — sit in layouts with nothing in common, and sharing the chrome
 * would mean one of them wearing the other's. The behaviour is what has to be
 * the same.
 */

/** Where a copy control is between one click and the next. */
export type CopyState = 'idle' | 'copied' | 'failed';

/**
 * How long the control says what happened before returning to rest. Long
 * enough to read after the eye has moved on, short enough that the next copy
 * is not still showing the last one's answer.
 */
const REST_DELAY_MS = 1600;

/**
 * What a copy control says in each state.
 *
 * `atRest` names what is being copied and is the only word that differs
 * between the two places this is used; "Copied" and the refusal are the same
 * sentence wherever they appear, so they are written once.
 */
export function copyLabel(state: CopyState, atRest: string): string {
  switch (state) {
    case 'copied':
      return 'Copied';
    case 'failed':
      return 'Clipboard unavailable';
    default:
      return atRest;
  }
}

export function useClipboard(): {
  state: CopyState;
  copy: (text: string) => Promise<void>;
} {
  const [state, setState] = useState<CopyState>('idle');

  // The timer that returns the control to rest, held so a second click can
  // cancel the first. Without that, clicking twice in quick succession let the
  // first timer land on the second copy and clear its confirmation early — and
  // a timer still pending at unmount set state on a component that was gone.
  const resetTimer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(resetTimer.current), []);

  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setState('copied');
    } catch (cause) {
      console.error('clipboard write denied', cause);
      setState('failed');
    }
    window.clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => setState('idle'), REST_DELAY_MS);
  };

  return { state, copy };
}
