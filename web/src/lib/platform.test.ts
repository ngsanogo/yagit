import { describe, expect, it } from 'vitest';

import { commandModifierFor } from './platform';

/**
 * The hint beside a shortcut is the only place the product names a key, and
 * naming the wrong one is worse than naming none: the shortcut works on every
 * platform, so a reader told `⌘` on a keyboard without one concludes there is
 * no shortcut rather than that the label is wrong.
 */
describe('commandModifierFor', () => {
  it('names Command on the platform strings an Apple browser answers with', () => {
    // navigator.platform, then userAgentData.platform: the two spellings that
    // reach this from a desktop.
    expect(commandModifierFor('MacIntel').label).toBe('⌘');
    expect(commandModifierFor('macOS').label).toBe('⌘');
    expect(commandModifierFor('MacPPC').label).toBe('⌘');
  });

  it('names Command on the hand-held platforms, which take a keyboard too', () => {
    expect(commandModifierFor('iPhone').label).toBe('⌘');
    expect(commandModifierFor('iPad').label).toBe('⌘');
    expect(commandModifierFor('iPod').label).toBe('⌘');
  });

  it('names Ctrl everywhere else', () => {
    expect(commandModifierFor('Linux x86_64').label).toBe('Ctrl');
    expect(commandModifierFor('Win32').label).toBe('Ctrl');
    expect(commandModifierFor('Windows').label).toBe('Ctrl');
    expect(commandModifierFor('FreeBSD amd64').label).toBe('Ctrl');
  });

  // The browser that answers neither question — every engine has dropped
  // something at some point — gets the answer that leaves the fewest readers
  // hunting for a key that is not there.
  it('falls back to Ctrl when the browser says nothing', () => {
    expect(commandModifierFor(undefined).label).toBe('Ctrl');
    expect(commandModifierFor('').label).toBe('Ctrl');
  });

  it('carries a spoken name for both, because neither glyph is read aloud', () => {
    expect(commandModifierFor('MacIntel').name).toBe('Command');
    expect(commandModifierFor('Linux x86_64').name).toBe('Control');
  });
});
