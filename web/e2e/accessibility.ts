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
