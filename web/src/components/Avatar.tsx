import { laneColor, LANE_COLOR_COUNT } from '../design/tokens';
import { cx } from '../lib/cx';
import { initialsFromName, stableIndex } from '../lib/format';

interface AvatarProps {
  name: string;
  size?: number;
  className?: string;
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
 */
export function Avatar({ name, size = 22, className }: AvatarProps) {
  const tint = laneColor(stableIndex(name, LANE_COLOR_COUNT));

  return (
    <span
      title={name}
      className={cx(
        'inline-flex shrink-0 items-center justify-center rounded-full',
        'border font-semibold select-none',
        className,
      )}
      style={{
        width: size,
        height: size,
        fontSize: Math.round(size * 0.42),
        color: tint,
        borderColor: tint,
        // A washed-out background rather than a solid fill: the chip has to
        // register without competing with the graph, which uses these same
        // hues at full saturation.
        backgroundColor: `color-mix(in oklch, ${tint} 14%, transparent)`,
      }}
    >
      {initialsFromName(name)}
    </span>
  );
}
