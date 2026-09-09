/**
 * What keeps a row's second action out of the way until it is wanted.
 *
 * Opacity and pointer events move together, and that pairing is the whole
 * point: transparent alone leaves a button nobody can see and everybody can
 * click, sitting in what reads as blank space on a row whose own click means
 * something else. The invisible button has been Discard on a changed file and
 * the menu holding Drop on a stash — a press that landed in the gap destroyed
 * work, or opened the one menu it must not.
 *
 * The third pair is for that menu. Its popover is in the browser's top layer,
 * nowhere near the row in the document, so `focus-within` is false for as long
 * as the menu has focus — without it the trigger fades out from under the menu
 * it opened, and the pointer heading for "Drop…" crosses a button that is no
 * longer there. Rows with no menu carry the pair and never match it, which is
 * cheaper than a second constant that differs by two lines.
 *
 * One constant, and therefore one group name. Tailwind reads class names out
 * of the source as literal text, so a group name cannot be interpolated in:
 * every row that reveals an action this way is `group/row`. That is precisely
 * what the copies this replaces could not have. Four wrote the pairing out in
 * full under two different group names, and only one of those carried the
 * transition; three more rows — worktrees, submodules, an LFS pattern — kept
 * the opacity and dropped the pointer events, which is the half that stops a
 * click. So lists sitting side by side faded, snapped, or answered a press
 * aimed at the row with a menu holding Remove.
 */
export const REVEALED_ON_ATTENTION = [
  'pointer-events-none opacity-0 transition-opacity transition-instant',
  'group-hover/row:pointer-events-auto group-hover/row:opacity-100',
  'group-focus-within/row:pointer-events-auto group-focus-within/row:opacity-100',
  'group-has-[[aria-expanded=true]]/row:pointer-events-auto',
  'group-has-[[aria-expanded=true]]/row:opacity-100',
].join(' ');
