import { useRef, useState } from 'react';

import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import type {
  CherryPickPlan,
  InteractiveRebasePlan,
  MergePlan,
  RebaseStep,
  RebasePlan,
  ResetPlan,
  RevertPlan,
  StashApplyPlan,
  StashDropPlan,
  StashPushPlan,
  TagPushPlan,
  UpstreamPlan,
} from '../api/types';
import type { PendingDelete, PendingUnsetUpstream } from './BranchDialogs';
import type { PendingTagDelete } from './TagDialogs';
import type { PendingSubmoduleRemove } from './SubmoduleDialogs';
import type { PendingWorktreeRemove } from './WorktreeDialogs';

/**
 * The one dialog the workbench may have open, and the rule that there is only
 * ever one.
 *
 * A module of its own because that rule belongs to no operation. It started in
 * BranchDialogs, which was true when the only confirmations were a branch's;
 * by the time cherry-pick, revert, reset, the rebase plan and the three stash
 * dialogs had joined it, eight of the eleven kinds were about something other
 * than a branch,
 * and the name was pointing at the wrong thing.
 */

export type WorkbenchDialog =
  | { kind: 'create' }
  // The one that asks nothing and confirms nothing: a search box, its results,
  // and a commit to follow. In the slot all the same, because <dialog> is
  // modal and two open at once is two.
  | { kind: 'search' }
  | { kind: 'create-tag' }
  | { kind: 'rename'; branch: string }
  | {
      kind: 'set-upstream';
      branch: string;
      hasUpstream: boolean;
      remote: string;
      upstream: string;
      plan: UpstreamPlan | undefined;
    }
  | { kind: 'unset-upstream'; pending: PendingUnsetUpstream }
  | { kind: 'delete'; pending: PendingDelete }
  | { kind: 'delete-tag'; pending: PendingTagDelete }
  | { kind: 'push-tag'; tag: string; remote: string; plan: TagPushPlan | undefined }
  | { kind: 'merge'; plan: MergePlan; preferMergeCommit: boolean }
  | { kind: 'rebase'; plan: RebasePlan }
  // The one dialog whose answer is not a yes: the plan is written ON it, so
  // the rows live in the slot beside the range they were read from. Held here
  // rather than in the dialog's own state because the slot is what survives a
  // re-render of the workbench, and a plan lost to one would be a list of
  // decisions somebody made twice.
  | { kind: 'rebase-plan'; plan: InteractiveRebasePlan; steps: RebaseStep[] }
  | { kind: 'cherry-pick'; plan: CherryPickPlan }
  | { kind: 'revert'; plan: RevertPlan }
  | { kind: 'reset'; plan: ResetPlan }
  | { kind: 'stash-push'; plan: StashPushPlan }
  | { kind: 'stash-apply'; plan: StashApplyPlan }
  | { kind: 'stash-drop'; plan: StashDropPlan }
  // Two steps in one dialog rather than two: the fields are filled, the daemon
  // answers with the command, and the same box then shows it. Its plan lives in
  // the slot so a re-render does not lose the line the user is reading.
  | { kind: 'add-worktree'; plan: { command: string; path: string } | undefined }
  | { kind: 'remove-worktree'; pending: PendingWorktreeRemove }
  | { kind: 'add-submodule'; plan: string | undefined }
  | { kind: 'remove-submodule'; pending: PendingSubmoduleRemove }
  // The same two steps in one box as add-submodule, for the same reason: the
  // pattern is typed, the daemon answers with the line, and the box then shows
  // it rather than a second dialog over the first.
  | { kind: 'track-lfs'; plan: string | undefined }
  | { kind: 'untrack-lfs'; pattern: string; command: string };

/**
 * A request for a plan, in the shape this module needs it: something that
 * takes an input and answers once, either way.
 *
 * Structural rather than TanStack's `UseMutationResult`, so that the twenty
 * plan mutations of the workbench all satisfy it without this module having to
 * know what any one of them is, or what it answers with.
 */
export interface PlanRequest<Input, Answer> {
  mutate: (
    input: Input,
    handlers: { onSuccess: (answer: Answer) => void; onError: (error: Error) => void },
  ) => void;
}

/**
 * The one dialog slot the workbench has.
 *
 * One state and not ten booleans, because <dialog> is modal in the browser's
 * top layer and two of them mean two: the second draws over the first, the
 * page under both is inert, and cancelling the top one uncovers a question the
 * user had stopped looking at. Dialog.tsx says the maximum depth is one; this
 * is what makes that true rather than hoped for. Ten independent flags could
 * not — most of these open on an ANSWER rather than on a click, so the two
 * clicks that stack them can be seconds apart.
 *
 * Which is also why opening is two words. A click opens now (`open`). A
 * question the daemon is still answering takes a `claim` first, and the claim
 * only opens if nothing has asked for the slot since — so a plan that comes
 * back after the user has moved on is dropped rather than landing under their
 * hands, where the next Enter would confirm it. Nothing is lost: what was
 * dropped is a question, and the row that asks it is still there.
 *
 * `propose` is that second shape written once. Almost every confirmation in
 * the workbench asks the daemon what a command would do and opens on the
 * answer, which is [ADR 0021] and [ADR 0022] made executable: the line a
 * dialog shows is read from the repository at the moment of the click, never
 * assembled in the browser out of a list it has been holding since before it.
 * Twenty copies of that shape were twenty chances to claim the slot after the
 * request instead of before it, or to swallow the refusal.
 *
 * [ADR 0021]: ../../../docs/adr/0021-a-shown-command-is-not-a-setting.md
 * [ADR 0022]: ../../../docs/adr/0022-a-merge-is-checked-before-it-runs.md
 */
export function useWorkbenchDialog() {
  const [dialog, setDialog] = useState<WorkbenchDialog | undefined>(undefined);
  const toast = useToast();

  // Which request for the slot is still the live one. A counter rather than a
  // boolean: two plans in flight are ordinary, and only the last click counts.
  const latest = useRef(0);

  const open = (next: WorkbenchDialog | undefined) => {
    latest.current += 1;
    setDialog(next);
  };

  const claim = () => {
    const mine = ++latest.current;
    return (next: WorkbenchDialog) => {
      if (latest.current === mine) {
        setDialog(next);
      }
    };
  };

  const into: ProposalTarget = {
    claim,
    report: (title, error) =>
      toast.push({ tone: 'danger', title, detail: errorDescription(error) }),
  };

  return {
    dialog,
    open,
    close: () => open(undefined),
    claim,
    propose: <Input, Answer>(
      request: PlanRequest<Input, Answer>,
      input: Input,
      subject: string,
      onAnswer: (answer: Answer) => WorkbenchDialog | undefined,
    ) => propose(into, request, input, subject, onAnswer),
  };
}

/** Where a proposal puts its two outcomes: the dialog, and the refusal. */
export interface ProposalTarget {
  claim: () => (next: WorkbenchDialog) => void;
  report: (title: string, error: Error) => void;
}

/**
 * Asks the daemon what a command would do, then opens the dialog on the answer.
 *
 * Three outcomes, written here once rather than at each of the twenty places
 * that ask for a plan:
 *
 *   - the plan answers, and the dialog opens on what it said;
 *   - the plan answers with nothing to confirm — `onAnswer` returns undefined —
 *     and the claim is released without opening anything;
 *   - the plan fails, and it is reported. Never silently: a button that reads a
 *     command and then does nothing at all is a button the user presses again.
 *
 * The last one is why this is a function. Written out twenty times, two of them
 * had no failure path at all: asking what adding a worktree or a submodule
 * would run, and being refused, left the dialog sitting there with nothing
 * said. A shape that cannot be written without its refusal cannot lose it.
 *
 * The slot is claimed BEFORE the request goes out, so an answer that arrives
 * after the user has moved on is dropped rather than opening under their hands,
 * where the next Enter would confirm it.
 *
 * `subject` completes "Could not read …" — "what deleting main would run".
 *
 * A free function rather than a method of the hook so that the rule can be
 * tested without a browser: see dialogSlot.test.ts.
 */
export function propose<Input, Answer>(
  into: ProposalTarget,
  request: PlanRequest<Input, Answer>,
  input: Input,
  subject: string,
  onAnswer: (answer: Answer) => WorkbenchDialog | undefined,
): void {
  const opens = into.claim();
  request.mutate(input, {
    onSuccess: (answer) => {
      const next = onAnswer(answer);
      if (next !== undefined) {
        opens(next);
      }
    },
    onError: (error) => into.report(`Could not read ${subject}`, error),
  });
}

/** Everything the workbench's one dialog slot offers. */
export type DialogSlot = ReturnType<typeof useWorkbenchDialog>;
