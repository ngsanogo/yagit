import type { Commit, InteractiveRebasePlan, RebaseInstruction, RebaseStep } from '../api/types';
import { namedCommit, pluralize, shortenSha } from '../lib/format';

/**
 * A rebase plan: the words it is written in, the edits that can be made to it,
 * and what the result of one says in a sentence.
 *
 * A module of its own, kept apart from the dialog that draws it, for the
 * reason resetSummary is: every rule here is a fact about what git will do
 * with a todo list, and facts about git are worth reading and testing on their
 * own. The dialog arranges rows; this decides what a row means.
 */

/**
 * The five, in the order the picker offers them.
 *
 * Keep first because it is what every row starts as and what most of them
 * stay. Drop last because it is the one that takes something away. The two
 * combines sit together in the middle, since choosing between them is one
 * question — whose message survives — and not two.
 */
export const REBASE_INSTRUCTIONS: readonly RebaseInstruction[] = [
  'pick',
  'fixup',
  'fixup -C',
  'edit',
  'drop',
];

/**
 * What a row's instruction is called on screen.
 *
 * Not git's verb, and this is the one place the two are allowed to differ.
 * `fixup -C` is a flag whose case is its whole meaning, which is unreadable as
 * a label and unmistakable as a command — so the row says what happens and the
 * command line beneath the plan says what runs. The value in between never
 * changes shape.
 *
 * Short enough to fit beside the commit it acts on, which is the constraint
 * that shaped them: a label long enough to be complete crushes the subject on
 * the same row, and the subject is what the reader is deciding about. So the
 * second combine says "keep this message" and leaves "into the one above" to
 * the row's own indentation and to the legend, which appears exactly when that
 * instruction is in use.
 */
export function instructionLabel(instruction: RebaseInstruction): string {
  switch (instruction) {
    case 'pick':
      return 'Keep';
    case 'fixup':
      return 'Combine into the one above';
    case 'fixup -C':
      return 'Combine, keep this message';
    case 'edit':
      return 'Stop to amend';
    case 'drop':
      return 'Drop';
    default:
      // A daemon newer than this page. Naming the verb is more useful than
      // naming nothing: it is the word in git's own documentation.
      return instruction;
  }
}

/**
 * What the instruction does, for the reader deciding between them.
 *
 * The two combines are the pair this exists for. Their labels differ by five
 * words and their outcomes differ by which commit message is gone afterwards,
 * which is not a difference a label can carry on its own.
 */
export function instructionDescription(instruction: RebaseInstruction): string {
  switch (instruction) {
    case 'pick':
      return 'Written again on what comes before it, or left exactly where it is when nothing below it changed.';
    case 'fixup':
      return 'Folded into the commit above. Its changes survive; its message is discarded.';
    case 'fixup -C':
      return 'Folded into the commit above, and its message replaces that commit’s.';
    case 'edit':
      return 'Applied, and then git stops so you can amend it. The banner carries on from there.';
    case 'drop':
      return 'Left out. Neither its changes nor its message survive.';
    default:
      return '';
  }
}

/** Whether an instruction folds its commit into the one above it. */
export function combines(instruction: RebaseInstruction): boolean {
  return instruction === 'fixup' || instruction === 'fixup -C';
}

/**
 * The plan a dialog opens on: every commit kept, in the order the branch
 * already holds them.
 *
 * The history as it stands, which is what an interactive rebase starts from in
 * a terminal too. A dialog that opened on anything else would be proposing an
 * edit nobody made.
 */
export function initialSteps(commits: readonly Commit[]): RebaseStep[] {
  return commits.map((commit) => ({ commit: commit.sha, instruction: 'pick' }));
}

/**
 * The plan with one row moved.
 *
 * A copy rather than a splice in place: these are React state, and a list
 * mutated in place is a list the renderer has no reason to believe changed.
 * An index outside the list comes back unmoved rather than throwing — the
 * buttons that call this are already disabled at the ends, and a plan is not
 * worth losing to a race between a click and a re-render.
 */
export function moveStep(steps: readonly RebaseStep[], from: number, to: number): RebaseStep[] {
  // The row is read before the bounds are argued about, which folds the two
  // guards into one: an index no row sits at and an index outside the list are
  // the same non-event, and the row that comes back is a row.
  const step = steps[from];
  if (step === undefined || to < 0 || to >= steps.length || from === to) {
    return [...steps];
  }

  const moved = steps.filter((_, at) => at !== from);
  moved.splice(to, 0, step);
  return moved;
}

/** The plan with one row's instruction replaced. */
export function withInstruction(
  steps: readonly RebaseStep[],
  index: number,
  instruction: RebaseInstruction,
): RebaseStep[] {
  return steps.map((step, at) => (at === index ? { ...step, instruction } : step));
}

/**
 * The first row from which git writes commits again, or the length of the plan
 * when it writes none.
 *
 * git fast-forwards over the bottom of a plan for as long as it matches the
 * history that is already there — same commits, same order, every one kept —
 * and those commits keep their hashes. From the first row that differs,
 * everything above is written again under a new hash, because each commit
 * names its parent.
 *
 * The dialog says so, and it is the sentence that makes the absence of
 * `--no-ff` visible: a plan that changes its last row alone costs one commit,
 * not the whole branch.
 */
export function firstRewritten(steps: readonly RebaseStep[], commits: readonly Commit[]): number {
  for (const [index, step] of steps.entries()) {
    if (step.instruction !== 'pick' || step.commit !== commits[index]?.sha) {
      return index;
    }
  }
  return steps.length;
}

/**
 * Why this plan cannot be run, or undefined when it can.
 *
 * The daemon refuses each of these too, and refuses them last — after the
 * range has been read again, which is where a refusal belongs when the
 * repository gets a say. What these are for is the button: a confirmation that
 * can only be answered by being pressed and failing is a confirmation that
 * teaches people to press it.
 *
 * Only the rules that are true of the LIST are here. Whether the list is still
 * the commits after the base is a question about the repository, and this page
 * is in no position to ask it.
 */
export function planRefusal(
  steps: readonly RebaseStep[],
  commits: readonly Commit[],
): string | undefined {
  if (steps.length === 0) {
    return 'A plan needs at least one commit.';
  }

  const kept = steps.find((step) => step.instruction !== 'drop');
  if (kept !== undefined && combines(kept.instruction)) {
    return (
      `${shortenSha(kept.commit)} is the first commit this plan keeps, so there is ` +
      `nothing above it to combine into. Move it down, or keep it.`
    );
  }

  if (firstRewritten(steps, commits) === steps.length) {
    return 'This plan is the history exactly as it stands, so running it would change nothing.';
  }

  return undefined;
}

/**
 * What running this plan would do, said in words.
 *
 * The command alone is not an explanation and, here, is barely a clue: `git
 * rebase --interactive -- a2801ba` is the same line for a plan that reorders
 * two commits and a plan that throws four away. Everything that differs
 * between them is in the rows, and this is the reading of the rows.
 */
export function planSummary(
  steps: readonly RebaseStep[],
  commits: readonly Commit[],
  plan: InteractiveRebasePlan,
): string {
  const dropped = steps.filter((step) => step.instruction === 'drop').length;
  const combined = steps.filter((step) => combines(step.instruction)).length;
  const stops = steps.filter((step) => step.instruction === 'edit').length;
  const surviving = steps.length - dropped - combined;

  const clauses: string[] = [
    `${plan.from} keeps ${pluralize(surviving, 'commit')} of ${steps.length}` +
      `, on top of ${namedCommit(plan.base, plan.subject)}.`,
  ];

  if (dropped > 0) {
    clauses.push(`${pluralize(dropped, 'commit')} dropped.`);
  }
  if (combined > 0) {
    clauses.push(`${pluralize(combined, 'commit')} folded into the one above it.`);
  }

  const from = firstRewritten(steps, commits);
  if (from < steps.length) {
    const rewritten = steps.length - from;
    const untouched = from;
    clauses.push(
      `${pluralize(rewritten, 'commit')} written again under new hashes` +
        (untouched === 0
          ? '.'
          : `, and the ${pluralize(untouched, 'commit')} below them keep theirs.`),
    );
  }

  if (stops > 0) {
    clauses.push(
      stops === 1
        ? 'git stops once, so you can amend that commit; the banner carries on from there.'
        : `git stops ${stops} times, once at each commit to amend.`,
    );
  }

  return clauses.join(' ');
}

/**
 * What this plan takes away, named precisely.
 *
 * Every rebase that runs is destructive in ConfirmDialog's sense — the commits
 * it rewrites stop being what the branch points at — so this never comes back
 * empty for a plan worth running. The two entries are different losses and are
 * named apart: a dropped commit's CHANGES are gone, while a rewritten one is
 * only gone under that hash.
 *
 * "through the reflog" and not "only through the reflog": a rewritten commit
 * that a tag or another branch also points at stays perfectly reachable. What
 * is always true is that the reflog still holds them.
 */
export function planLosses(
  steps: readonly RebaseStep[],
  commits: readonly Commit[],
): readonly [string, ...string[]] | undefined {
  const items: string[] = [];

  const dropped = steps.filter((step) => step.instruction === 'drop');
  if (dropped.length > 0) {
    items.push(
      `the changes in ${pluralize(dropped.length, 'dropped commit')} — ` +
        `${dropped.map((step) => shortenSha(step.commit)).join(', ')}`,
    );
  }

  const discarded = steps.filter((step) => step.instruction === 'fixup').length;
  if (discarded > 0) {
    items.push(`the message of ${pluralize(discarded, 'commit')} folded into the one above it`);
  }

  const rewritten = steps.length - firstRewritten(steps, commits);
  if (rewritten > 0) {
    items.push(
      `${pluralize(rewritten, 'commit')} under the hashes they have now — ` +
        `reachable afterwards through the reflog`,
    );
  }

  if (items.length === 0) {
    return undefined;
  }
  return items as [string, ...string[]];
}

/**
 * What the toast says once a plan has run.
 *
 * Two endings rather than one, because a plan holding an `edit` does not
 * finish: git stops in the middle of it having exited zero, and the daemon
 * says so in the answer. "Rewrote 4 commits" over a repository waiting at the
 * second of them would be the interface reporting the wrong half of what
 * happened.
 */
export function planReport(
  steps: readonly RebaseStep[],
  branch: string,
  stopped: boolean,
  step?: number,
  total?: number,
): string {
  if (!stopped) {
    return `Rewrote ${pluralize(steps.length, 'commit')} on ${branch}`;
  }
  return step !== undefined && total !== undefined && step > 0 && total > 0
    ? `Stopped at ${step} of ${total} on ${branch} — amend it, then continue`
    : `Stopped part way through the plan on ${branch} — amend it, then continue`;
}
