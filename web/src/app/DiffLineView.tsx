import type { DiffLine } from '../api/types';
import { cx } from '../lib/cx';
import type { LineSpan } from './diffSpans';

/**
 * The two tints a line's kind paints: the whole line, and the run inside it
 * that actually moved.
 *
 * Kept as one table because they are one decision. The pale wash says which
 * side of the diff the line is on; the stronger one says which characters of
 * it are the change, and a colour picked for the second without the first
 * beside it is how the two stop being read as the same colour.
 */
const KIND_TINTS: Record<DiffLine['kind'], { whole: string; changed: string }> = {
  added: { whole: 'bg-added/12', changed: 'bg-added/30' },
  removed: { whole: 'bg-deleted/12', changed: 'bg-deleted/30' },
  context: { whole: '', changed: '' },
};

const MARKERS: Record<DiffLine['kind'], string> = {
  added: '+',
  removed: '-',
  context: ' ',
};

/**
 * What a screen reader is told this line is.
 *
 * Built from the text the daemon sent, which is already bounded — the label of
 * a minified line used to be the whole line. The count is spoken when there is
 * one, because a name that simply stops is a name that lies about the line.
 *
 * The whitespace note is the same fact the glyphs draw. A line whose only
 * change is an indent reads identically to the line above it in this name, and
 * a reader who cannot see the dots has nothing else to go on.
 */
function accessibleName(line: DiffLine, spans: readonly LineSpan[] | undefined): string {
  const kind = line.kind === 'added' ? 'Added' : 'Removed';
  const rest =
    line.truncated !== undefined && line.truncated > 0
      ? `, and ${line.truncated.toLocaleString()} more characters not shown`
      : '';
  return `${kind} line: ${line.text}${rest}${whitespaceNote(spans) ?? ''}`;
}

/** The one sentence the glyphs are drawing, for a reader who cannot see them. */
function whitespaceNote(spans: readonly LineSpan[] | undefined): string | undefined {
  if (spans === undefined) {
    return undefined;
  }
  if (spans.some((span) => span.glyphs && span.changed)) {
    return ', where what changed is the whitespace';
  }
  return spans.some((span) => span.glyphs) ? ', with trailing whitespace' : undefined;
}

/**
 * One line of a diff.
 *
 * onToggle is what makes a line choosable, and its absence is what makes a
 * diff read-only — a commit's diff has nothing to stage. The line renders the
 * same either way, which is the point of it being one component.
 *
 * Two elements carry backgrounds and they own different things: the row owns
 * the STATE — hovered, chosen — and the span inside it owns the KIND. They
 * used to be one element with two background utilities on it, which is not a
 * choice at all but a question put to the cascade: `bg-accent-soft/60` sorts
 * before `bg-added/10`, so a chosen added line painted as an ordinary added
 * one and the only evidence of the choice was a 2px stripe at the left edge.
 * Layered, a chosen line is an accent wash with the kind's colour still over
 * it, and a hovered one keeps its green or its red instead of losing it to an
 * opaque highlight.
 *
 * The two number columns sit OUTSIDE that span deliberately. Every wash the
 * row can wear lightens what is under it, and the numbers are the faintest ink
 * in the pane: measured against --color-ink-subtle, a hovered added line's
 * numbers come to 4.34:1 with the kind tint over them and 5.29:1 without,
 * which is the AA floor on the wrong side of a state a pointer produces by
 * accident. Outside the tint they read against the row alone, and the coloured
 * band starts where the diff's own +/- marker does.
 */
export function LineView({
  line,
  gutter,
  spans,
  findSpans: findRuns,
  selected,
  tabStop,
  onToggle,
}: {
  line: DiffLine;
  gutter: string;
  spans: readonly LineSpan[] | undefined;
  findSpans?: readonly { text: string; hit: boolean; current: boolean }[];
  selected: boolean;
  tabStop: boolean;
  onToggle?: (line: DiffLine, extend: boolean) => void;
}) {
  const changeable = line.kind !== 'context' && onToggle !== undefined;
  const note = whitespaceNote(spans);

  const body =
    findRuns !== undefined
      ? findRuns.map((span, at) => (
          <span
            key={at}
            // The current match is the accent itself, with the ink made for
            // it. The others are outlined rather than filled: a soft accent
            // fill is the green an added line is already tinted with, and on
            // one it vanished.
            className={cx(
              span.current && 'bg-accent text-accent-ink',
              span.hit && !span.current && 'ring-1 ring-accent ring-inset',
            )}
          >
            {span.text}
          </span>
        ))
      : spans === undefined
        ? line.text
        : spans.map((span, at) => (
            <Span key={at} span={span} tint={KIND_TINTS[line.kind].changed} />
          ));

  const number = cx(
    gutter,
    'shrink-0 pr-2 text-right text-2xs text-ink-subtle tabular select-none',
  );
  // A hairline after the second number, so the two gutters read as a margin
  // and the +/- marker starts where the code does.
  const lastNumber = cx(number, 'border-r border-line');

  const content = (
    <>
      {/* Both line numbers, always, in a fixed-width column. An added line has
          no old number and a removed one has no new number; leaving the space
          empty rather than collapsing it is what keeps the two columns from
          jittering down the file. */}
      <span className={number}>{line.old_line === 0 ? '' : line.old_line}</span>
      <span className={lastNumber}>{line.new_line === 0 ? '' : line.new_line}</span>

      <span className={cx('flex min-w-0 flex-1 items-start', KIND_TINTS[line.kind].whole)}>
        <span
          aria-hidden="true"
          className={cx(
            'w-4 shrink-0 text-center select-none',
            line.kind === 'added' && 'text-added',
            line.kind === 'removed' && 'text-deleted',
            line.kind === 'context' && 'text-ink-subtle',
          )}
        >
          {MARKERS[line.kind]}
        </span>

        {/* A read-only row has no aria-label to carry the +/- through, and the
            glyph beside it is aria-hidden precisely so it is not read as
            punctuation: without this, an addition, a deletion and a context
            line are the same text to a screen reader, which is the state a
            patch is least readable in. select-none so a copy of the patch is
            still the patch. */}
        {!changeable && line.kind !== 'context' && (
          <span className="sr-only select-none">
            {line.kind === 'added' ? 'Added line: ' : 'Removed line: '}
          </span>
        )}

        {/* pre-wrap, not pre: a long line has to wrap rather than push a
            horizontal scrollbar under the whole file, and the leading spaces of
            indented code have to survive. wrap-anywhere, not break-all: both
            keep a minified line from forcing that scrollbar, but break-all
            takes the break at whatever character the edge falls on even when a
            space sat three characters earlier, which cuts every wrapped line of
            prose, Markdown and comments mid-word. */}
        <span className="min-w-0 flex-1 pr-3 wrap-anywhere whitespace-pre-wrap text-ink">
          {body}
          {line.truncated !== undefined && line.truncated > 0 && (
            /* Said, not silently done. A line that stops with no sign of it
               reads as the file having ended there. */
            <span className="ml-2 text-2xs text-ink-subtle">
              (+{line.truncated.toLocaleString()} more characters, not shown)
            </span>
          )}
          {line.no_newline && (
            <span className="ml-2 text-2xs text-ink-subtle">(no newline at end of file)</span>
          )}
          {!changeable && note !== undefined && <span className="sr-only select-none">{note}</span>}
        </span>
      </span>
    </>
  );

  const shared = cx(
    'flex w-full items-start border-l-2 text-left',
    selected ? 'border-accent bg-accent-soft/60' : 'border-transparent',
  );

  if (!changeable) {
    // data-diff-line here too: find scrolls by that attribute, and a
    // read-only patch has no buttons to hang it on.
    return (
      <div className={shared} data-diff-line={line.index}>
        {content}
      </div>
    );
  }

  return (
    <button
      type="button"
      // The pane's roving tab stop. Every other changed line stays reachable
      // by arrow key rather than by Tab; see moveByKey.
      tabIndex={tabStop ? 0 : -1}
      data-diff-line={line.index}
      aria-pressed={selected}
      aria-label={accessibleName(line, spans)}
      onClick={(event) => {
        // A press, a drag and a release inside one line is a text selection,
        // and the browser reports it as a click on the element both ends
        // landed in. Without this, copying an identifier or an error string
        // out of a diff silently arms a staging selection — and the header
        // answers that by swapping the hint for a Stage button and a red
        // Discard one. Enter from the keyboard arrives with nothing selected,
        // so the keyboard path is untouched.
        if ((window.getSelection()?.toString() ?? '') !== '') {
          return;
        }
        onToggle(line, event.shiftKey);
      }}
      className={cx(
        shared,
        'cursor-pointer outline-none focus-visible:focus-ring',
        // Not on a chosen line: --color-hover is opaque, so hovering one would
        // paint over the accent wash and take away the only full-width mark
        // the choice has. The other row lists in the workbench draw the same
        // bargain the same way.
        !selected && 'hover:bg-hover',
      )}
    >
      {content}
    </button>
  );
}

/**
 * One run of a line: the part that moved, the whitespace that cannot be seen,
 * or ordinary text.
 *
 * The glyphs are drawn INSTEAD of the whitespace and the whitespace is kept
 * beside them at zero width, which is the only arrangement that satisfies both
 * halves of the problem. Replacing the characters outright would put a middle
 * dot into every copy taken out of the pane, and pasting `····fmt.Println` into
 * an editor is a worse day than not seeing the indent change; leaving them
 * alone and tinting the run instead cannot distinguish a tab from the four
 * spaces that replaced it, which is the commonest whitespace change there is.
 * So the visible run is select-none and aria-hidden, and the twin beside it —
 * clipped to no width, whitespace-pre so the copy keeps every character — is
 * what a selection actually picks up. The line numbers in this same component
 * have relied on select-none for a clean copy since the pane was written.
 *
 * A tab keeps its own character after the arrow rather than being replaced by
 * a fixed number of spaces: the arrow occupies one column and the real tab
 * then advances to the next tab stop, so the text after it lands exactly where
 * it lands on every other line. An arrow alone would be one column wide where
 * the tab was four, and the file would step in and out down the page.
 *
 * align-bottom on the twin is not decoration. An inline-block whose overflow
 * is not visible takes its baseline from its bottom margin edge rather than
 * from the line inside it, so the twin — a whole line-height tall, standing on
 * the row's baseline — grows the line box above it and the row comes out
 * taller than every row around it. Aligning it to the bottom of the line
 * instead makes it fit inside the height the row already had. A pane whose
 * rows change height wherever an indent changed is the jitter the fixed gutter
 * exists to prevent, arrived at from the other direction.
 */
function Span({ span, tint }: { span: LineSpan; tint: string }) {
  const marked = span.changed ? tint : undefined;

  if (!span.glyphs) {
    return marked === undefined ? <>{span.text}</> : <span className={marked}>{span.text}</span>;
  }

  return (
    <span className={marked}>
      <span aria-hidden="true" className="select-none">
        {glyphsFor(span.text)}
      </span>
      <span className="inline-block w-0 overflow-hidden align-bottom whitespace-pre">
        {span.text}
      </span>
    </span>
  );
}

/** A middle dot per space, an arrow per tab — and the tab itself, for width. */
function glyphsFor(text: string): string {
  // oxlint-disable-next-line typescript/no-misused-spread -- one glyph per code point of whitespace
  return [...text].map((character) => (character === '\t' ? '→\t' : '·')).join('');
}
