import { Button } from '../components/Button';
import { cx } from '../lib/cx';
import { pluralize } from '../lib/format';
import type { Operation } from '../api/types';
import {
  applyChoice,
  conflictSideNotes,
  layOutConflicts,
  type ConflictBlock,
  type ConflictSideNotes,
  type NumberedLine,
  type RegionChoice,
} from './conflict';

/**
 * A conflicted file, region by region, with the choice offered where the
 * choice is.
 *
 * git resolves what it can and writes the rest into the file between markers.
 * That is the whole of a conflict: the file is ordinary text, the two versions
 * are both in it, and somebody has to say which. Every git client either sends
 * that person to another editor or shows them the markers as a wall of
 * punctuation — this shows the two versions as two blocks and puts three
 * buttons between them.
 *
 * The choice never leaves the browser. Taking a side here rewrites the buffer
 * and nothing else; the file is written when the user saves it, and staged when
 * they stage it. That is the same separation the rest of the panel keeps — disk
 * and index are different places — and it is what makes taking a side
 * reversible right up until the save.
 *
 * Distinct from taking a side of the WHOLE file, which is `git checkout --ours`
 * and does leave the browser. Both exist because they answer different
 * questions: one file where every region goes the same way is one command, and
 * a file where they do not is this.
 */

interface ConflictResolverProps {
  /** The buffer as it stands, markers and all. Re-read on every keystroke. */
  text: string;
  /** Hands back the buffer with one region rewritten. */
  onChange: (text: string) => void;
  busy: boolean;
  /**
   * What the repository is in the middle of. Decides what the notes beside
   * "ours" and "theirs" say — during a rebase they mean the opposite of a
   * merge, and a note that does not follow the operation names the wrong side.
   */
  operation: Operation;
}

/**
 * How many unchanged lines are drawn on each side of a region.
 *
 * A conflict in a two-thousand-line file is three lines somebody needs to see
 * and 1,997 they do not, and a pane that renders all of them scrolls past the
 * thing it exists to show. Three matches the context git puts around a hunk,
 * which is the number this project already uses for the same judgement.
 */
const CONTEXT_LINES = 3;

export function ConflictResolver({ text, onChange, busy, operation }: ConflictResolverProps) {
  const { blocks, skippedAfter } = layOutConflicts(text, CONTEXT_LINES);
  const notes = conflictSideNotes(operation);

  return (
    <div className="min-h-0 flex-1 overflow-auto">
      {blocks.map((block, position) => (
        <RegionView
          // Regions have no identity of their own, and the whole list is
          // rebuilt from the text on every keystroke.
          key={position}
          block={block}
          position={position}
          total={blocks.length}
          busy={busy}
          notes={notes}
          onChoose={(choice) => onChange(applyChoice(text, block.region, choice))}
        />
      ))}

      <Skipped count={skippedAfter} />
    </div>
  );
}

/**
 * The lines this pane is not drawing, counted.
 *
 * Without it, a jump from line 17 to line 400 reads as a file that ends at 17.
 */
function Skipped({ count }: { count: number }) {
  if (count <= 0) {
    return null;
  }
  return (
    <p className="border-t border-line px-3 py-1 text-2xs text-ink-subtle">
      {pluralize(count, 'unchanged line')}
    </p>
  );
}

function RegionView({
  block,
  position,
  total,
  busy,
  notes,
  onChoose,
}: {
  block: ConflictBlock;
  position: number;
  total: number;
  busy: boolean;
  notes: ConflictSideNotes;
  onChoose: (choice: RegionChoice) => void;
}) {
  const { region, before, after } = block;

  return (
    <section className="border-b border-line last:border-b-0">
      <Skipped count={block.skippedBefore} />

      <header className="sticky top-0 flex items-center gap-2 bg-sunken px-3 py-1.5">
        <span className="text-2xs font-medium text-ink-muted">
          Conflict {position + 1} of {total}
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-1">
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => onChoose('ours')}>
            Keep ours
          </Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => onChoose('theirs')}>
            Keep theirs
          </Button>
          {/* Last, because it is the least often right: two sides that both
              added something independently. Keeping both otherwise produces
              code that says everything twice. */}
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => onChoose('both')}>
            Keep both
          </Button>
        </span>
      </header>

      <div className="font-mono text-xs">
        <Context lines={before} />

        <Side
          label={region.ourLabel === '' ? 'ours' : region.ourLabel}
          note={notes.ours}
          lines={region.ours}
          tone="ours"
        />

        {/* Only under diff3 and zdiff3. It is what the two sides diverged FROM,
            and it is the thing that turns "which of these do I want" into
            "which of them changed what". No button takes it: keeping it would
            resolve the conflict by undoing both sides. */}
        {region.base !== undefined && (
          <Side label="was" note="what both sides started from" lines={region.base} tone="base" />
        )}

        <Side
          label={region.theirLabel === '' ? 'theirs' : region.theirLabel}
          note={notes.theirs}
          lines={region.theirs}
          tone="theirs"
        />

        <Context lines={after} />
      </div>
    </section>
  );
}

const TONE_CLASSES = {
  ours: { band: 'bg-ours-soft', rule: 'border-ours', label: 'text-ours' },
  theirs: { band: 'bg-theirs-soft', rule: 'border-theirs', label: 'text-theirs' },
  base: { band: 'bg-sunken', rule: 'border-line-strong', label: 'text-ink-subtle' },
} as const;

function Side({
  label,
  note,
  lines,
  tone,
}: {
  label: string;
  note: string;
  lines: string[];
  tone: keyof typeof TONE_CLASSES;
}) {
  const classes = TONE_CLASSES[tone];

  return (
    <div className={cx('border-l-2', classes.band, classes.rule)}>
      <p className="flex items-baseline gap-2 px-3 pt-1">
        <span className={cx('text-2xs font-medium', classes.label)}>{label}</span>
        <span className="text-2xs text-ink-subtle">{note}</span>
      </p>

      {lines.length === 0 ? (
        // An empty side is a real resolution — one branch added lines and the
        // other added nothing — and it has to be visible, or "Keep theirs"
        // looks like a button that does nothing.
        <p className="px-3 pb-1 text-2xs text-ink-subtle italic">nothing on this side</p>
      ) : (
        <div className="pb-1">
          {lines.map((line, offset) => (
            <pre key={offset} className="px-3 break-all whitespace-pre-wrap text-ink">
              {line === '' ? ' ' : line}
            </pre>
          ))}
        </div>
      )}
    </div>
  );
}

/** Unchanged lines, with their real numbers: this is a place in a file. */
function Context({ lines }: { lines: NumberedLine[] }) {
  return (
    <div>
      {lines.map((line) => (
        <div key={line.number} className="flex items-start">
          <span className="w-12 shrink-0 pr-2 text-right text-2xs text-ink-subtle tabular select-none">
            {line.number}
          </span>
          <pre className="min-w-0 flex-1 pr-3 break-all whitespace-pre-wrap text-ink-muted">
            {line.text === '' ? ' ' : line.text}
          </pre>
        </div>
      ))}
    </div>
  );
}
