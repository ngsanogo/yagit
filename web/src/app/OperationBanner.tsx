import { useState } from 'react';

import type {
  Operation,
  OperationAction,
  OperationPlan,
  RepositoryState,
  WorkingDirectory,
} from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { useToast } from '../components/ToastHost';
import { Tooltip } from '../components/Tooltip';
import { errorDescription } from '../lib/errorDisplay';
import { pluralize } from '../lib/format';
import { operationName, useOperation } from './useOperation';

/**
 * What the repository is in the middle of, and the buttons that end it.
 *
 * A merge that stopped leaves a repository in a state every subsequent screen
 * is about and none of them mentions. The file list shows seven conflicted
 * files; `git status` in a terminal would have said "You are currently
 * merging" above them, and the porcelain format yagit reads says nothing at
 * all — so the daemon reads the same marker files git does, and this is where
 * that arrives.
 *
 * The buttons are the daemon's list rather than this file's, and they arrive
 * on the status beside the operation itself. Which instructions an operation
 * takes is a fact about git — `git merge --skip` does not exist, and a merge
 * is finished by committing in the box below rather than continued — so the
 * table lives once, in internal/git/operation.go. A button drawn here is a
 * command the daemon accepts.
 *
 * Two of the three destroy work and go through a confirmation showing the
 * exact line. Continuing destroys nothing and runs on the click, which is the
 * same rule the rest of the application follows rather than an exception made
 * here.
 */

interface OperationBannerProps {
  repositoryId: string;
  status: WorkingDirectory;
}

export function OperationBanner({ repositoryId, status }: OperationBannerProps) {
  const { state } = status;
  const toast = useToast();
  const { act, plan } = useOperation(repositoryId);

  // The dialog opens on an answer rather than beside one, which is the shape
  // the discard and the branch delete use: its whole content is a command the
  // daemon assembled, and one that opened first would draw a placeholder where
  // the command goes.
  const [confirming, setConfirming] = useState<OperationPlan>();

  if (state.operation === '') {
    return null;
  }

  const conflicted = status.files.filter((file) => file.kind === 'unmerged').length;
  const busy = act.isPending || plan.isPending;

  /**
   * Continuing runs on the click; the other two ask first.
   *
   * The split is git's, not this file's: continuing records what was resolved
   * and moves on, and the other two exist to throw something away. What the
   * dialog then SAYS comes from the plan the daemon answers — the command, and
   * the `destroys` that colours the confirmation — so a dialog cannot promise
   * a line the daemon was never going to run.
   */
  const choose = (action: OperationAction) => {
    if (action === 'continue') {
      act.mutate({
        action,
        operation: state.operation,
        identity: state.identity ?? '',
      });
      return;
    }

    plan.mutate(action, {
      onSuccess: setConfirming,
      onError: (error: Error) => {
        toast.push({
          tone: 'danger',
          title: 'Could not read what that would run',
          detail: errorDescription(error),
        });
      },
    });
  };

  return (
    <>
      <div
        // A live region: this appears because something happened in another
        // window — a merge run in a terminal — and a screen reader that only
        // announces what it was told to look at would never mention it.
        role="status"
        className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b border-warning/40 bg-warning-soft px-3 py-1.5"
      >
        <span className="text-xs font-medium text-ink">
          {headline(state.operation, state.branch)}
        </span>

        {state.step !== undefined && state.total !== undefined && (
          <Badge tone="warning">
            step {state.step} of {state.total}
          </Badge>
        )}

        <span className="text-2xs text-ink-muted">{whatIsLeft(state, conflicted)}</span>

        <div className="ml-auto flex items-center gap-1.5">
          {(state.actions ?? []).map((action) => {
            const blocked = blockedBy(action, state, conflicted);
            return (
              // Tooltip and not `title`: a disabled Button drops pointer
              // events, so the browser fires no hover on it and the native
              // tooltip never appears — which would leave the sentence
              // explaining the refusal readable by nobody, exactly when it is
              // needed. See Tooltip's own comment.
              <Tooltip
                key={action}
                // These sit at the right end of the banner, where a bubble
                // centred over the last of them would hang past the window.
                align="end"
                label={blocked ?? describe(action, state.operation)}
              >
                <Button
                  size="sm"
                  // Continuing is the way forward and carries the weight of
                  // one. The other two are exits: present, and not inviting.
                  variant={action === 'continue' ? 'secondary' : 'ghost'}
                  loading={act.isPending && act.variables?.action === action}
                  disabled={busy || blocked !== undefined}
                  onClick={() => choose(action)}
                >
                  {label(action)}
                </Button>
              </Tooltip>
            );
          })}
        </div>
      </div>

      {confirming !== undefined && (
        <ConfirmDialog
          open
          // The daemon's answer, not a constant: git.Destroys is the authority
          // on which of the three throws work away, and this dialog only
          // chooses its colour and its wording from it.
          destructive={confirming.destroys}
          title={`${label(confirming.action)} the ${operationName(confirming.operation)}?`}
          command={confirming.command}
          losing={whatIsLost(confirming.action, confirming.operation)}
          confirmLabel={label(confirming.action)}
          busy={act.isPending}
          onCancel={() => setConfirming(undefined)}
          onConfirm={() => {
            // The operation from the PLAN, not from the banner: the plan was
            // answered from a reading of the repository taken when this dialog
            // opened, and it is what the command on screen describes. Identity
            // travels with it so a finished rebase cannot be aborted as the
            // next one that started under the same kind.
            act.mutate(
              {
                action: confirming.action,
                operation: confirming.operation,
                identity: confirming.identity,
              },
              { onSettled: () => setConfirming(undefined) },
            );
          }}
        />
      )}
    </>
  );
}

/**
 * Why a button is disabled, or undefined when it is not.
 *
 * Returned as the sentence rather than as a boolean, so a button can never be
 * disabled without one, and shown through a Tooltip because that is the only
 * thing a disabled control can speak through.
 *
 * Two sources, and the order is which one the user meets first. The daemon's
 * is a refusal — it read the rebase's plan, it will answer 409, and there is
 * nothing to be done here about a `squash` it has no editor for. The conflict
 * count is this file's own, because the file list is what it holds and the
 * daemon's status does not count for it: git refuses that continue anyway, in
 * a good sentence, and saying it before the click rather than after is the
 * difference between a button that explains itself and one that fails.
 */
function blockedBy(
  action: OperationAction,
  state: RepositoryState,
  conflicted: number,
): string | undefined {
  const refused = state.blocked?.[action];
  if (refused !== undefined) {
    return refused;
  }

  if (action === 'continue' && conflicted > 0) {
    return `${pluralize(conflicted, 'file')} still conflicted. Resolve and stage ${
      conflicted === 1 ? 'it' : 'them'
    } first.`;
  }

  return undefined;
}

/**
 * What a button that CAN be pressed will do, in one line.
 *
 * Continue is the one that needs saying. It commits, and what it commits under
 * is the message of the commit being replayed — never what is typed into the
 * box below, which is a real thing to reach for while a cherry-pick sits on a
 * conflict. A merge is not continued here at all, and that is the other half
 * of the same answer: where the box IS the way to finish, there is no second
 * button that ignores it.
 */
function describe(action: OperationAction, operation: Operation): string {
  const name = operationName(operation);

  switch (action) {
    case 'continue':
      return `Carries the ${name} on, committing what is resolved under the message being replayed — never what is typed in the box below.`;
    case 'skip':
      return `Drops the commit this ${name} stopped on and moves to the next.`;
    case 'abort':
      return operation === 'bisect'
        ? 'Checks the original branch back out and forgets every verdict.'
        : `Calls the ${name} off and puts the repository back where it started.`;
  }
}

function label(action: OperationAction): string {
  switch (action) {
    case 'abort':
      return 'Abort';
    case 'continue':
      return 'Continue';
    case 'skip':
      return 'Skip';
  }
}

/**
 * What the confirmation promises to destroy.
 *
 * Every line has to be something git really does take away, and for an abort
 * that is not the same sentence for every operation. `git rebase --abort`
 * resets hard to where the branch started, so everything since goes, including
 * a commit made by hand along the way. The sequencer's operations and `git am`
 * both refuse to rewind past a commit they did not make themselves — abort a
 * cherry-pick that was finished with yagit's commit box and git says "You
 * seem to have moved HEAD. Not rewinding", exits 0, and leaves it standing.
 *
 * Committing by hand is the ORDINARY way out of a conflict here, so that is
 * the common case rather than the exotic one, and a dialog promising to
 * discard those commits would be lying in it. So the line hedges, which is the
 * honest thing to do about a rewind git decides on for itself.
 *
 * What is NOT listed is the rest of the sequence. Those commits are not
 * applied and nothing happens to them: they are still on the branch they came
 * from, and this list is headed "This will permanently discard".
 */
function whatIsLost(action: OperationAction, operation: Operation): [string, ...string[]] {
  const name = operationName(operation);

  if (action === 'skip') {
    return [`the commit this ${name} stopped on, and every change in it`];
  }

  switch (operation) {
    case 'merge':
      return ['every conflict resolved since the merge began'];
    case 'bisect':
      return ['every good and bad verdict given so far'];
    case 'rebase':
      return [
        'every commit this rebase has replayed, and any you committed yourself along the way',
        'every conflict resolved since it began',
      ];
    default:
      return [
        `the commits this ${name} applied itself, unless you have committed since — git will not rewind past a commit of your own`,
        'every conflict resolved since it began',
      ];
  }
}

/**
 * The operation, in the present continuous, because it is still going on.
 *
 * git's own vocabulary throughout. "Rebasing" is what git calls it and what
 * every answer somebody searches for calls it, and an interface that invented
 * a friendlier word would be teaching a name that appears nowhere else.
 */
function headline(operation: Operation, branch: string | undefined): string {
  switch (operation) {
    case 'merge':
      return 'Merging';
    case 'rebase':
      return branch === undefined || branch === '' ? 'Rebasing' : `Rebasing ${branch}`;
    case 'cherry-pick':
      return 'Cherry-picking';
    case 'revert':
      return 'Reverting';
    case 'bisect':
      return 'Bisecting';
    case 'am':
      return 'Applying patches';
    case '':
      return '';
  }
}

/**
 * The next thing to do.
 *
 * Sentences and not a decision tree. A bisect is the one operation here that
 * conflicts cannot happen in and that committing does not end, so it gets its
 * own line rather than being told to commit.
 *
 * A rebase yagit will not continue is told so here as well as on the button,
 * because this is the line that says what to do next and "Continue to carry
 * on" beside a Continue that is refused is the interface arguing with itself.
 *
 * Otherwise a merge is told to commit and everything else is told to continue,
 * which is the same split the buttons are drawn from: committing ends a merge,
 * and it leaves a rebase exactly where it was.
 */
function whatIsLeft(state: RepositoryState, conflicted: number): string {
  if (state.operation === 'bisect') {
    return 'Mark this commit good or bad in your terminal to carry on.';
  }

  const refused = state.blocked?.continue;
  if (refused !== undefined) {
    return refused;
  }

  if (conflicted > 0) {
    return `${pluralize(conflicted, 'file')} still conflicted — open one to choose a side.`;
  }
  if (state.operation === 'merge') {
    return 'Nothing is left conflicted. Commit to finish it.';
  }
  return 'Nothing is left conflicted. Continue to carry on.';
}
