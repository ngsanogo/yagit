/**
 * The dismiss control on a panel that opened over the history.
 *
 * One component rather than the same eleven lines in every panel: the glyph,
 * the hit area and the focus ring are the same in all of them, and the only
 * thing that differs is what a screen reader is told is being closed — which
 * is exactly the thing a copy gets wrong when the next panel arrives.
 */
export function CloseButton({ label, onClose }: { label: string; onClose: () => void }) {
  return (
    <button
      type="button"
      onClick={onClose}
      aria-label={label}
      className="rounded-sm p-1 text-ink-subtle transition-colors transition-instant outline-none hover:text-ink focus-visible:focus-ring"
    >
      <svg width="13" height="13" viewBox="0 0 14 14" fill="none" aria-hidden="true">
        <path
          d="M3.5 3.5l7 7M10.5 3.5l-7 7"
          stroke="currentColor"
          strokeWidth="1.4"
          strokeLinecap="round"
        />
      </svg>
    </button>
  );
}
