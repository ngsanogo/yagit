/**
 * Which modifier key the person reading the screen actually has.
 *
 * Every shortcut in this application is taken with `metaKey || ctrlKey`, so
 * both keys work everywhere and only the hint beside the button was ever
 * wrong: it said `⌘` to a reader on Linux or Windows, naming a key their
 * keyboard does not have for the only shortcut that view offers.
 *
 * It has to be asked in the browser rather than answered at build time, and
 * that follows from [ADR 0001]: the daemon runs on a machine that may have no
 * screen at all and the page is opened from somewhere else, so which of the
 * six binaries was built says nothing about the keyboard in front of the
 * reader. A Go-side flag or a compile-time constant would be answering a
 * different question.
 *
 * The decision is a pure function of a platform string so it can be tested
 * without a browser; `commandModifier` is the one place in the product that
 * reads `navigator`.
 *
 * [ADR 0001]: ../../../docs/adr/0001-a-local-daemon-and-a-browser.md
 */

/** A modifier key, as it is drawn and as it is read out. */
export interface ModifierKey {
  /** What the hint shows: the glyph on the key, or its short name. */
  label: string;
  /**
   * What a screen reader says in place of the glyph.
   *
   * `⌘` is announced by some readers as "place of interest sign" and by others
   * as nothing at all, so the hint carries the word beside it. `Ctrl` needs
   * the same treatment for the opposite reason: it is read as a word, and the
   * word it is read as is not always "control".
   */
  name: string;
}

const COMMAND: ModifierKey = { label: '⌘', name: 'Command' };
const CONTROL: ModifierKey = { label: 'Ctrl', name: 'Control' };

/**
 * The platform strings that mean an Apple keyboard.
 *
 * `navigator.platform` answers `MacIntel`, `MacPPC`, `iPhone`, `iPad` or
 * `iPod`; `navigator.userAgentData.platform` answers `macOS`. Matched as
 * substrings rather than exactly, because the list of spellings is the
 * browsers' to extend and a reader on the one that gets added next is better
 * served by a loose match than by a table nobody updated.
 */
const APPLE_PLATFORMS = ['mac', 'iphone', 'ipad', 'ipod'];

/**
 * The command modifier for a platform string.
 *
 * Anything unrecognised, and anything missing, is `Ctrl`. That is the answer
 * fewer readers are wrong about: an Apple user shown `Ctrl` presses the key
 * beside it and the shortcut still fires, where everybody else shown `⌘` is
 * looking for a key that is not on the keyboard.
 */
export function commandModifierFor(platform: string | undefined): ModifierKey {
  const said = (platform ?? '').toLowerCase();
  return APPLE_PLATFORMS.some((name) => said.includes(name)) ? COMMAND : CONTROL;
}

/** The command modifier for the browser this page is open in. */
export function commandModifier(): ModifierKey {
  return commandModifierFor(browserPlatform());
}

/**
 * What the browser says it is running on.
 *
 * `userAgentData` first, because it is the answer with a future — but it is
 * Chromium's alone and is absent from the DOM type definitions, so it is
 * reached through an intersection with Navigator: the property stays optional
 * and the engines that do not have it stay type-checked, which `any` would
 * have thrown away along with the rest of the object.
 *
 * `navigator.platform` is deprecated and is still the only thing every other
 * engine answers. Named here rather than worked around silently: the whole of
 * the deprecation is that it should not be used to decide what a browser can
 * do, and this decides what a keyboard has on it.
 */
function browserPlatform(): string | undefined {
  const agent = navigator as Navigator & { userAgentData?: { platform?: string } };
  const hinted = agent.userAgentData?.platform;
  if (hinted !== undefined && hinted !== '') {
    return hinted;
  }
  return navigator.platform;
}
