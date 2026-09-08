import AxeBuilder from '@axe-core/playwright';
import type { Page } from '@playwright/test';

/**
 * The axe scan, in one place.
 *
 * It lives beside the tests rather than inside one of them because two specs
 * need it: the design system, where every component is shown in every state,
 * and the product screens, where components are combined in ways no showcase
 * predicts. A scan that only ever ran against the showcase would be checking
 * the reference and not the thing.
 */

/** The rule set this project holds itself to: WCAG 2.1, levels A and AA. */
export const wcagAA = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

/**
 * Two things this scan does not measure, written down because a gate whose
 * blind spots are undocumented reads as a gate with none.
 *
 * **`label-content-name-mismatch` stays off.** It is WCAG 2.5.3 — Label in
 * Name, level A — and it fires when a control's accessible name does not
 * contain its visible label, which is what stops somebody driving the
 * interface by voice from reaching a button by saying the words on it. axe
 * ships it disabled as experimental, and switching it on here was tried and
 * abandoned, for a reason worth keeping: this interface puts visible text
 * inside controls that their aria-label deliberately leaves out. A diff line
 * is a button showing two gutter numbers before the code; a change row shows
 * the one-letter status mark before the path; the name of each is the sentence
 * a screen reader should hear, not the glyphs beside it. The rule cannot tell
 * that apart from a real mismatch, so turning it on paints the changes view
 * red line by line.
 *
 * It would also, correctly, have caught three buttons whose names shared no
 * words with their labels: `Update all` and `Sync URLs` in the submodule
 * panel, and `Add worktree` in the worktree panel. Those were the real defect
 * the rule exists for and they are fixed — each name now begins with the
 * words on the button and the sentence moved to a native `title`, which is
 * also the only hover text a panel header can show without Panel clipping it.
 * What is left before this rule can come on is one decision: whether the diff
 * gutter's line numbers and the change row's status letter are label text or
 * decoration. Marking them `aria-hidden` would settle it in the rule's favour
 * and would have to answer how a line number reaches a screen reader instead.
 *
 * **`results.incomplete` is not read.** axe's third answer means "a human has
 * to look at this one", and the colour-contrast check lands there whenever it
 * cannot resolve the ground a piece of text sits on. This interface gives it
 * plenty of chances: every `bg-<token>/<n>` is a translucent ground, so file
 * marks, ref badges and diff line tints are composites axe may decline to
 * measure rather than measure and fail. A pairing that ends up there is not
 * green, it is unmeasured, and nothing below reports it as either.
 *
 * Asserting that `incomplete` is empty is not the fix on its own: it would
 * turn every such pairing into a gate at once, and the ones already known to
 * be under the floor — the status palette over a highlighted row in the light
 * theme, argued in `web/src/design/tokens.css` — would fail the suite on the
 * spot rather than in a change somebody chose to make. It wants a spec that
 * names the pairings it has accepted, and the specs are not this file.
 * `web/src/design/tokens.test.ts` measures the declarations meanwhile, which
 * covers the tokens against each other but never the composite on the screen.
 */

/**
 * axe returns a deep object per violation, and an assertion on it prints
 * hundreds of lines of DOM. This keeps the failure readable: the rule, what it
 * means, and the elements at fault.
 */
export async function violations(page: Page) {
  const results = await new AxeBuilder({ page }).withTags(wcagAA).analyze();

  return results.violations.map((violation) => ({
    rule: violation.id,
    impact: violation.impact,
    help: violation.help,
    elements: violation.nodes.map((node) => node.target.join(' ')),
  }));
}
