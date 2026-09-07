import { describe, expect, it } from 'vitest';

import { menuItem } from './Menu';

/**
 * The one thing about a menu item that no type can hold up.
 *
 * MenuItem's union already refuses a refused item with no `reason` property —
 * that is a compile error, and the reason it is a union at all. What it cannot
 * refuse is a reason that is present and says nothing, which is the same grey
 * item with the same nothing under it: `''` type-checks, and so does a
 * sentence built out of a branch name that came back empty.
 *
 * A closed menu builds no items, so the end-to-end suite cannot reach this and
 * a rendering test would have nothing to render. It is a fact about the
 * builder, and this is where it is checked.
 */
describe('menuItem', () => {
  const spec = { id: 'delete', label: 'Delete…', onSelect: () => undefined };

  it('offers the item when no reason is given', () => {
    const item = menuItem(spec);

    expect(item.disabled).toBeUndefined();
    expect(item.reason).toBeUndefined();
  });

  it('refuses the item, with the reason, when one is given', () => {
    const item = menuItem(spec, 'HEAD is on main, so it cannot be deleted');

    expect(item.disabled).toBe(true);
    expect(item.reason).toBe('HEAD is on main, so it cannot be deleted');
  });

  // A grey item with nothing to say is how people learn that the menu is
  // broken rather than that HEAD is detached, and that is the whole argument
  // for the union. Thrown rather than offered — which would run an action the
  // caller meant to withhold — and rather than refused in silence, which is
  // the broken menu again with nobody told.
  it('refuses to build a refusal that explains nothing', () => {
    expect(() => menuItem(spec, '')).toThrow('delete');
    expect(() => menuItem(spec, '   ')).toThrow(/no reason/);
  });

  // The reason is drawn as a sentence under the label, so what surrounds it
  // is layout rather than text.
  it('trims the reason it keeps', () => {
    expect(menuItem(spec, '  nothing to force-update.  ').reason).toBe('nothing to force-update.');
  });
});
