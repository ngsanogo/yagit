import { laneColor, LANE_COLOR_COUNT } from '../design/tokens';
import { cx } from '../lib/cx';
import { initialsFromName, stableIndex } from '../lib/format';

/**
 * The chip's diameter, in pixels.
 *
 * Exported because the history's skeleton row has to hold exactly the space
 * the chip will take. It guessed before — a 24 pixel circle where the loaded
 * row drew 22 — and the subject column twitched two pixels sideways as each
 * page arrived, which is the jitter a fixed row height exists to prevent,
 * reintroduced under it.
 *
 * Twenty-four rather than twenty-two: a multiple of the 4px step everything
 * else in the interface lands on.
 */
export const AVATAR_SIZE = 24;

interface AvatarProps {
  name: string;

  /**
   * The diameter, for a caller that has a reason to differ from AVATAR_SIZE.
   *
   * The label no longer scales with it: the initials are drawn at the type
   * scale's floor, which is tuned for a 24 pixel chip. A much larger chip
   * would want a step of the scale chosen for it here rather than arithmetic
   * on this number — that arithmetic is what put the one size no token
   * defines on the screen.
   */
  size?: number;

  className?: string;

  /**
   * Hides the chip from assistive technology.
   *
   * For the places where the author's name is already written beside it. The
   * initials are visible text, so they join the accessible name of whatever
   * contains them: every commit row used to be announced as "AL feat: the
   * second lane" — two letters that mean nothing, in front of the most-read
   * string on the screen, encoding a fact the row says in full a moment later.
   *
   * Not the default. Where the chip stands alone it is the only thing naming
   * the author, and hiding it there would remove the fact rather than repeat
   * it.
   */
  decorative?: boolean;
}

/**
 * Author chip.
 *
 * No image: yagit is a local tool and will not fetch an avatar from a
 * third-party service to draw a list of commits — that would tell the service
 * which history is being read.
 *
 * The tint comes from the name, so it is stable: that is what makes the chip
 * recognizable out of the corner of the eye. It borrows the lane palette,
 * whose ten colors are already tuned to be equally salient.
 *
 * A ring and the initials, and no fill. There was a fill — the tint at 14%
 * over whatever the chip stood on — and it was the tint fighting itself: a
 * light lane colour washed into a dark row raises the ground under its own
 * letters, and the letters are the lane colour. On a selected row that cost
 * the initials their contrast in both themes (4.19:1 dark, 3.95:1 light,
 * against a floor of 4.5), and on a hovered row in the light theme too. With
 * no fill the ground is the row's own and every lane clears the floor
 * everywhere the chip is drawn — the worst pairing in the set is 4.65:1.
 * tokens.test.ts holds that as an assertion now, because the fill was
 * invisible to every instrument the project had until the design page drew a
 * selected row.
 *
 * The ring alone still registers, which was the fill's job: it is the hue
 * that identifies an author, not the area of it.
 */
export function Avatar({ name, size = AVATAR_SIZE, className, decorative = false }: AvatarProps) {
  const tint = laneColor(stableIndex(name, LANE_COLOR_COUNT));

  return (
    <span
      // The tooltip goes with the chip even when it is decorative: a pointer
      // reading it is not the channel the aria-hidden is about.
      title={name}
      aria-hidden={decorative || undefined}
      className={cx(
        'inline-flex shrink-0 items-center justify-center rounded-full',
        // text-2xs and not a fraction of the diameter. The computed size was
        // 9px — the one type size in the product no token defines, below the
        // floor of a scale whose header says "seven steps, not one more" — and
        // it was the size a person's identity was drawn at.
        'border font-semibold select-none text-2xs',
        className,
      )}
      style={{
        width: size,
        height: size,
        color: tint,
        borderColor: tint,
      }}
    >
      {initialsFromName(name)}
    </span>
  );
}
