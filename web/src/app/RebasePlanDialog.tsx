import type { InteractiveRebasePlan, RebaseInstruction, RebaseStep } from '../api/types';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Select } from '../components/Select';
import { cx } from '../lib/cx';
import { pluralize, shortenSha } from '../lib/format';
import {
  REBASE_INSTRUCTIONS,
  combines,
  instructionDescription,
  instructionLabel,
  moveStep,
  planLosses,
  planRefusal,
  planSummary,
  withInstruction,
} from './interactiveRebase';

/**
 * The rebase plan, as a list to be rearranged.
 *
 * A confirmation like every other destructive operation here — the command is
 * on it, the losses are named on it — with the difference that what is being
 * confirmed does not exist until the user has written it. So the rows ARE the
 * question, and they are drawn in git's own order: oldest at the top, which is
 * the order a todo list is executed in and therefore the order in which "the
 * one above" means anything at all.
 *
 * That last point is why the list is not drawn newest-first to match the
 * history above it. Combining folds a commit into the one above it in the
 * PLAN, and a plan drawn upside down would have that word pointing at the
 * wrong row on the one screen where it has to be exact.
 */

const MOVE_UP = -1;
const MOVE_DOWN = 1;

export function RebasePlanDialog({
  plan,
  steps,
  busy,
  onCancel,
  onConfirm,
  onSteps,
}: {
  plan: InteractiveRebasePlan;
  /** The plan as it stands, held by the workbench so a re-render cannot lose it. */
  steps: RebaseStep[];
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onSteps: (steps: RebaseStep[]) => void;
}) {
  const refusal = planRefusal(steps, plan.commits);
  const losses = planLosses(steps, plan.commits);
  const subjects = new Map(plan.commits.map((commit) => [commit.sha, commit.subject]));

  const editor = (
    <div className="flex flex-col gap-3">
      <ol className="flex max-h-96 flex-col gap-1 overflow-y-auto rounded-md border border-line bg-sunken p-1.5">
        {steps.map((step, index) => (
          <PlanRow
            key={step.commit}
            step={step}
            subject={subjects.get(step.commit) ?? ''}
            position={index}
            last={index === steps.length - 1}
            disabled={busy}
            onMove={(by) => onSteps(moveStep(steps, index, index + by))}
            onInstruction={(instruction) => onSteps(withInstruction(steps, index, instruction))}
          />
        ))}
      </ol>

      <Legend steps={steps} />

      {refusal !== undefined && (
        <p className="rounded-md border border-warning/35 bg-warning-soft/50 px-3 py-2 text-sm text-ink">
          {refusal}
        </p>
      )}
    </div>
  );

  // One set of props and two calls, for the reason ResetDialog needs two:
  // ConfirmDialog's destructive arm REQUIRES a non-empty loss list, and the
  // only way to satisfy that type when there is nothing to name is not to pass
  // `destructive` at all. A plan that changes nothing is refused above, so the
  // second call is reached only while the dialog is still being written.
  const shared = {
    open: true,
    busy,
    confirmDisabled: refusal !== undefined,
    onCancel,
    onConfirm,
    // Wider than any other confirmation here, because this is the only one
    // whose content is a list to be read row by row: at the default width the
    // instruction beside a commit and the commit's own subject compete for the
    // same inches, and the subject — the thing being decided about — loses.
    size: 'wide' as const,
    title: `Rewrite the ${pluralize(plan.commits.length, 'commit')} after ${shortenSha(plan.base)}?`,
    description: planSummary(steps, plan.commits, plan),
    command: plan.command,
    confirmLabel: 'Run the plan',
  };

  if (losses !== undefined) {
    return (
      <ConfirmDialog {...shared} destructive losing={losses}>
        {editor}
      </ConfirmDialog>
    );
  }

  return <ConfirmDialog {...shared}>{editor}</ConfirmDialog>;
}

function PlanRow({
  step,
  subject,
  position,
  last,
  disabled,
  onMove,
  onInstruction,
}: {
  step: RebaseStep;
  subject: string;
  position: number;
  last: boolean;
  disabled: boolean;
  onMove: (by: number) => void;
  onInstruction: (instruction: RebaseInstruction) => void;
}) {
  const short = shortenSha(step.commit);
  const dropped = step.instruction === 'drop';

  return (
    <li className="flex items-center gap-2 rounded-sm px-1.5 py-1">
      <div className="flex shrink-0 flex-col">
        <MoveButton
          direction="up"
          label={`Move ${short} up`}
          disabled={disabled || position === 0}
          onClick={() => onMove(MOVE_UP)}
        />
        <MoveButton
          direction="down"
          label={`Move ${short} down`}
          disabled={disabled || last}
          onClick={() => onMove(MOVE_DOWN)}
        />
      </div>

      {/* The indent is on the commit rather than on the row, so that the two
          controls stay in their columns. A folded row is stepped in under the
          row it folds into, because "the one above" is the whole of what the
          instruction means and a list of flat rows makes the reader count. */}
      <code
        className={cx(
          'shrink-0 font-mono text-2xs text-ink-subtle',
          combines(step.instruction) && 'ml-6',
        )}
      >
        {short}
      </code>

      {/* Struck through and muted, never dimmed. Opacity over a token is a
          colour nobody chose: `text-ink-subtle` at 55% fails the contrast the
          token was picked to pass, and the row that says "this one is going"
          is not the row to make hard to read. */}
      <span
        className={cx(
          'min-w-0 flex-1 truncate text-sm',
          dropped ? 'text-ink-muted line-through' : 'text-ink',
        )}
        title={subject}
      >
        {subject}
      </span>

      <Select
        hideLabel
        label={`What to do with ${short}${subject === '' ? '' : `, ${subject}`}`}
        className="w-60 shrink-0"
        value={step.instruction}
        disabled={disabled}
        options={REBASE_INSTRUCTIONS.map((instruction) => ({
          value: instruction,
          label: instructionLabel(instruction),
        }))}
        onChange={(event) => onInstruction(event.target.value as RebaseInstruction)}
      />
    </li>
  );
}

/**
 * What the instructions in THIS plan do.
 *
 * Only the ones in use, and never `pick`: a legend describing five verbs when
 * the plan holds two is a paragraph nobody reads, and "Keep" needs no gloss.
 * It appears when a row is changed and disappears when it is changed back,
 * which is the only moment the explanation is worth the space.
 */
function Legend({ steps }: { steps: readonly RebaseStep[] }) {
  const used = REBASE_INSTRUCTIONS.filter(
    (instruction) =>
      instruction !== 'pick' && steps.some((step) => step.instruction === instruction),
  );

  if (used.length === 0) {
    return null;
  }

  // A flowing paragraph per instruction rather than two columns: a definition
  // list in a flex row wraps its description under itself and leaves the
  // second line hanging in the middle of the dialog, which reads as a layout
  // accident. Inline, it wraps back to the margin like the sentence it is.
  return (
    <dl className="flex flex-col gap-1 text-2xs text-ink-muted">
      {used.map((instruction) => (
        <div key={instruction}>
          <dt className="inline font-medium text-ink-subtle">{instructionLabel(instruction)}</dt>
          <dd className="inline"> — {instructionDescription(instruction)}</dd>
        </div>
      ))}
    </dl>
  );
}

function MoveButton({
  direction,
  label,
  disabled,
  onClick,
}: {
  direction: 'up' | 'down';
  label: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className={cx(
        'rounded-sm px-0.5 text-ink-subtle transition-colors transition-instant outline-none',
        'hover:text-ink focus-visible:focus-ring',
        'disabled:pointer-events-none disabled:opacity-30',
      )}
    >
      <ChevronGlyph direction={direction} />
    </button>
  );
}

function ChevronGlyph({ direction }: { direction: 'up' | 'down' }) {
  return (
    <svg width="11" height="11" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <path
        d={direction === 'up' ? 'M3.5 8.5l3.5-3.5 3.5 3.5' : 'M3.5 5.5l3.5 3.5 3.5-3.5'}
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
