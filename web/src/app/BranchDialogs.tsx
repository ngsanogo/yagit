import { useState } from 'react';

import type {
  CherryPickPlan,
  MergePlan,
  RebasePlan,
  ResetMode,
  ResetPlan,
  RevertPlan,
  Remote,
  UpstreamPlan,
} from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { SegmentedControl } from '../components/SegmentedControl';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';
import { shortenSha } from '../lib/format';
import { cherryPickSummary } from './cherrypick';
import { mergeSummary } from './merge';
import { rebaseLosses, rebaseSummary } from './rebase';
import { resetLosses, resetSummary } from './reset';
import { revertSummary } from './revert';

/**
 * The two branch operations that need a name typed, and the six that need
 * permission.
 *
 * Creating and renaming ask for a string and run something reversible.
 * Deleting runs `git branch -D`, merging runs a command that can rewrite every
 * file in the work tree, rebasing writes the branch's own commits again under
 * new hashes, cherry-picking applies one commit onto the branch HEAD is on,
 * reverting takes one commit's changes back out of it, and resetting moves
 * that branch to a selected ancestor in soft, mixed or hard. The last six ask
 * first, and they ask with the line the daemon says it will run rather than
 * one assembled here — see useBranches.planDelete, planMerge, planRebase,
 * useCherryPick.plan, useRevert.plan and useReset.plan for why that round trip
 * exists.
 *
 * Each confirmation carries a sentence beside its command, and the ones that
 * take something away carry a list of what disappears, for the same reason: a
 * command is exact and is not an explanation. What the sentences say is
 * mergeSummary's, rebaseSummary's, cherryPickSummary's, revertSummary's and
 * resetSummary's job; what the lists name is rebaseLosses's and resetLosses's.
 *
 * All of them are mounted only while they are open — useWorkbenchDialog is
 * what decides which one that is, and it lives in dialogSlot.ts because the
 * one-modal rule belongs to no operation — and that is what empties their
 * fields: the state
 * is born with the component, so nothing has to remember to reset it, and
 * nothing resets it while the dialog is still up, which is what keeps a name
 * git refused on screen to be fixed.
 *
 * git validates the name, not these dialogs. A branch name has rules
 * (git-check-ref-format) that no field here should be re-deciding, and git's
 * refusal names the rule that was broken. What is refused locally is only the
 * empty string and the no-op rename, because both are buttons that would run a
 * command to do nothing.
 */

export function CreateBranchDialog({
  busy,
  startLabel,
  onCancel,
  onCreate,
}: {
  busy: boolean;
  /**
   * Where the branch will start, in words — "main", "a2801ba".
   *
   * Shown, never edited. Picking a start point is choosing a commit, and the
   * place to choose a commit is the history, not a text field in a dialog that
   * would have to explain what it accepts.
   */
  startLabel: string;
  onCancel: () => void;
  onCreate: (name: string, switchTo: boolean) => void;
}) {
  const [name, setName] = useState('');
  const [switchTo, setSwitchTo] = useState(true);

  const trimmed = name.trim();

  return (
    <Dialog
      open
      onClose={onCancel}
      title="New branch"
      description={`It will start at ${startLabel}.`}
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={trimmed === ''}
            onClick={() => onCreate(trimmed, switchTo)}
          >
            Create
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Field
          label="Branch name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="feature/lane-assignment"
          autoFocus
        />

        {/* Checked by default, because making a branch is nearly always the
            first half of moving onto it. The box is here rather than in a
            second button so that the choice is visible before the click
            rather than remembered after it. */}
        <label className="flex items-center gap-2 text-xs text-ink-muted">
          <input
            type="checkbox"
            checked={switchTo}
            onChange={(event) => setSwitchTo(event.target.checked)}
            className="accent-accent"
          />
          Check it out
        </label>
      </div>
    </Dialog>
  );
}

export function RenameBranchDialog({
  branch,
  busy,
  onCancel,
  onRename,
}: {
  /**
   * The branch being renamed.
   *
   * The caller keys this component on it, so a second rename starts from the
   * second branch's name rather than from whatever was typed for the first.
   */
  branch: string;
  busy: boolean;
  onCancel: () => void;
  onRename: (to: string) => void;
}) {
  // Seeded with the current name, which is what a rename box should open on:
  // most renames change part of a name, and starting from empty makes somebody
  // retype the part they were keeping.
  const [name, setName] = useState(branch);

  const trimmed = name.trim();

  return (
    <Dialog
      open
      onClose={onCancel}
      title={`Rename ${branch}`}
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={trimmed === '' || trimmed === branch}
            onClick={() => onRename(trimmed)}
          >
            Rename
          </Button>
        </>
      }
    >
      <Field
        label="New name"
        value={name}
        onChange={(event) => setName(event.target.value)}
        autoFocus
      />
    </Dialog>
  );
}

/** What the delete confirmation needs: the branch, and git's own command. */
export interface PendingDelete {
  name: string;
  force: boolean;
  /** The exact line the daemon answered with. */
  command: string;
}

/** Which of the eight is open, and what it is about. */
/**
 * The merge confirmation, opened on a plan the daemon answered.
 *
 * Everything on it comes from that one reading of the two branches — the two
 * names in the title, the sentence, and the command — so nothing here can
 * describe a merge other than the one the button will run.
 */
export function MergeBranchDialog({
  plan,
  busy,
  preferringMergeCommit,
  onPreferMergeCommit,
  onCancel,
  onConfirm,
}: {
  plan: MergePlan;
  busy: boolean;
  /**
   * Whether the "merge commit anyway" preference is on. Only meaningful when
   * a fast-forward was possible; the dialog re-asks the daemon when it flips
   * (ADR 0021).
   */
  preferringMergeCommit: boolean;
  onPreferMergeCommit: (prefer: boolean) => void;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  // The preference only applies where a fast-forward was the natural reading.
  // Once the plan is already a merge commit because the branches diverged,
  // the box would be a lie — there is no fast-forward to refuse.
  const canPreferMergeCommit = plan.outcome === 'fast-forward' || preferringMergeCommit;

  return (
    <ConfirmDialog
      open
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Merge ${plan.branch} into ${plan.into}?`}
      description={mergeSummary(plan)}
      command={plan.command}
      confirmLabel="Merge"
    >
      {canPreferMergeCommit && (
        <label className="flex cursor-pointer items-start gap-2 text-sm text-ink">
          <input
            type="checkbox"
            className="mt-0.5 size-3.5 shrink-0 accent-accent"
            checked={preferringMergeCommit}
            disabled={busy}
            onChange={(event) => onPreferMergeCommit(event.target.checked)}
          />
          <span>
            Create a merge commit anyway
            <span className="mt-0.5 block text-2xs text-ink-subtle">
              Uses <span className="font-mono">--no-ff</span> even though a fast-forward is
              possible.
            </span>
          </span>
        </label>
      )}
    </ConfirmDialog>
  );
}

/**
 * The rebase confirmation, opened on a plan the daemon answered.
 *
 * Two shapes, and which one is drawn is the outcome the daemon read — never a
 * guess made here. A replay writes every commit the branch holds again under a
 * new hash and recreates none of the merge commits it passes, which is the most
 * history the interface can take away in one click; ConfirmDialog's non-empty
 * `losing` tuple is what makes naming it a compile-time requirement, and
 * rebaseLosses is what names it.
 *
 * The other two take nothing. An up-to-date rebase writes nothing at all and a
 * fast-forward moves a pointer, so a red panel over either of them would be a
 * warning about a loss that does not happen — which is how people learn to read
 * past the one that does.
 */
export function RebaseBranchDialog({
  plan,
  busy,
  onCancel,
  onConfirm,
}: {
  plan: RebasePlan;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const title = `Rebase ${plan.from} onto ${plan.onto}?`;
  const description = rebaseSummary(plan);

  if (plan.outcome !== 'rebase') {
    return (
      <ConfirmDialog
        open
        busy={busy}
        onCancel={onCancel}
        onConfirm={onConfirm}
        title={title}
        description={description}
        command={plan.command}
        confirmLabel="Rebase"
      />
    );
  }

  return (
    <ConfirmDialog
      open
      destructive
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={title}
      description={description}
      command={plan.command}
      losing={rebaseLosses(plan)}
      confirmLabel="Rebase"
    />
  );
}

/**
 * The cherry-pick confirmation, opened on a plan the daemon answered.
 *
 * Only the two outcomes that run reach here. Up-to-date answers with an empty
 * command and is toasted from the plan mutation instead — there is no
 * cherry-pick that succeeds as a no-op, and a dialog that invented one would
 * break ConfirmDialog's promise that the line on screen is the line that runs.
 */
export function CherryPickDialog({
  plan,
  busy,
  onCancel,
  onConfirm,
}: {
  plan: CherryPickPlan;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Cherry-pick ${shortenSha(plan.commit)} onto ${plan.into}?`}
      description={cherryPickSummary(plan)}
      command={plan.command}
      confirmLabel="Cherry-pick"
    />
  );
}

/**
 * The revert confirmation, opened on a plan the daemon answered.
 *
 * A revert always records a new commit, so every plan that reaches here has a
 * command — the refusals (merge, root, not on the branch) never open a dialog.
 */
export function RevertDialog({
  plan,
  busy,
  onCancel,
  onConfirm,
}: {
  plan: RevertPlan;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Revert ${shortenSha(plan.commit)} on ${plan.into}?`}
      description={revertSummary(plan)}
      command={plan.command}
      confirmLabel="Revert"
    />
  );
}

const RESET_MODES: { value: ResetMode; label: string }[] = [
  { value: 'soft', label: 'Soft' },
  { value: 'mixed', label: 'Mixed' },
  { value: 'hard', label: 'Hard' },
];

/**
 * The reset confirmation, opened on a plan the daemon answered.
 *
 * Soft, mixed and hard are a choice on this dialog rather than three buttons
 * on the commit panel: the panel already carries checkout, cherry-pick and
 * revert, and the three modes are faces of one question. Changing the mode
 * re-asks the daemon so the command on screen is always the one that will run.
 *
 * Hard is destructive when something would actually be discarded; a clean
 * reset to HEAD is still offered, but without the red list, because naming an
 * empty loss would be a lie.
 */
export function ResetDialog({
  plan,
  busy,
  planning = false,
  onCancel,
  onConfirm,
  onModeChange,
}: {
  plan: ResetPlan;
  busy: boolean;
  /** True while a mode change is re-reading the plan. */
  planning?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onModeChange: (mode: ResetMode) => void;
}) {
  const losses = resetLosses(plan);
  // Locked while the daemon is answering and while the reset itself runs:
  // every segment is a question with a round trip behind it, and the command
  // on screen has to be the one the button next to it will send.
  const locked = busy || planning;
  const modePicker = (
    <SegmentedControl
      label="Reset mode"
      segments={RESET_MODES}
      value={plan.mode}
      onChange={onModeChange}
      disabled={locked}
    />
  );

  // One set of props and two calls, because ConfirmDialog's destructive arm
  // REQUIRES a non-empty loss list — that is the type doing its job, and the
  // only way to satisfy it is to not pass `destructive` at all when there is
  // nothing to name. Everything that does not depend on that is written once.
  const shared = {
    open: true,
    busy: locked,
    onCancel,
    onConfirm,
    title: `Reset ${plan.into} to ${shortenSha(plan.commit)}?`,
    description: resetSummary(plan),
    command: plan.command,
    confirmLabel: `Reset --${plan.mode}`,
  };

  if (losses !== undefined) {
    return (
      <ConfirmDialog {...shared} destructive losing={losses}>
        {modePicker}
      </ConfirmDialog>
    );
  }

  return <ConfirmDialog {...shared}>{modePicker}</ConfirmDialog>;
}

export function DeleteBranchDialog({
  pending,
  busy,
  onCancel,
  onConfirm,
}: {
  pending: PendingDelete;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open
      destructive
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Delete ${pending.name}?`}
      command={pending.command}
      // Named as what it is rather than as what it usually is. A branch is a
      // name pointing at a commit; deleting it takes the name away, and takes
      // the commits with it only where nothing else can reach them — which is
      // exactly the case `-d` refuses and `-D` is for.
      losing={[
        `the branch ${pending.name}`,
        ...(pending.force ? ['any commits nothing else still points at'] : []),
      ]}
      confirmLabel="Delete"
    />
  );
}

/**
 * Recording where a branch follows, without pushing.
 *
 * Distinct from publishing: no objects leave the machine. The plan is asked as
 * the destination is chosen, because the command is the dialog's whole promise
 * — the same rule the publish and force-push confirmations hold to.
 */
export function SetUpstreamDialog({
  branch,
  remotes,
  trackingChoices,
  remote,
  upstream,
  plan,
  hasUpstream,
  busy,
  planning,
  onRemoteChange,
  onUpstreamChange,
  onPickTracking,
  onCancel,
  onSave,
}: {
  branch: string;
  remotes: readonly Remote[];
  /** Branch names on the chosen remote that already exist as remote-tracking refs. */
  trackingChoices: readonly string[];
  remote: string;
  upstream: string;
  plan: UpstreamPlan | undefined;
  /** True when the branch already follows something. */
  hasUpstream: boolean;
  busy: boolean;
  planning: boolean;
  onRemoteChange: (remote: string) => void;
  onUpstreamChange: (upstream: string) => void;
  onPickTracking: (upstream: string) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const trimmed = upstream.trim();
  const hasDestination = remote !== '' && trimmed !== '';
  // The command on screen must name the remote and branch in the fields —
  // a plan for an earlier keystroke is refused rather than confirmed.
  const planMatches = plan !== undefined && plan.remote === remote && plan.upstream === trimmed;

  return (
    <Dialog
      open
      onClose={onCancel}
      title={hasUpstream ? `Change upstream for ${branch}` : `Set upstream for ${branch}`}
      description="Every later push and pull follows what you record here, without sending anything now."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!hasDestination || !planMatches || planning}
            onClick={onSave}
          >
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Select
          label="Remote"
          value={remote}
          onChange={(event) => onRemoteChange(event.target.value)}
          options={remotes.map((entry) => ({
            value: entry.name,
            label: `${entry.name} — ${entry.fetch_url}`,
          }))}
          disabled={busy || remotes.length === 1}
          hint={
            remotes.length === 1
              ? 'The only remote this repository has.'
              : 'The branch will follow one on the remote you pick.'
          }
        />

        <Field
          label="Branch on remote"
          value={upstream}
          onChange={(event) => onUpstreamChange(event.target.value)}
          placeholder="main"
          autoComplete="off"
          spellCheck={false}
          disabled={busy}
          hint={
            trackingChoices.length > 0
              ? 'Or pick one that already exists as a remote-tracking branch below.'
              : 'The short branch name on that remote.'
          }
        />

        {trackingChoices.length > 0 && (
          <Select
            label="Remote-tracking branch"
            value=""
            onChange={(event) => onPickTracking(event.target.value)}
            options={[
              { value: '', label: 'Pick one…' },
              ...trackingChoices.map((name) => ({ value: name, label: name })),
            ]}
            disabled={busy}
          />
        )}

        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-ink-muted">yagit will run</p>
          {!hasDestination ? (
            <p className="text-sm text-ink-muted">Choose a remote and a branch name.</p>
          ) : planning || !planMatches ? (
            <Spinner label="Reading what setting upstream would run" />
          ) : (
            <GitCommand command={plan.command} />
          )}
        </div>
      </div>
    </Dialog>
  );
}

export interface PendingUnsetUpstream {
  branch: string;
  command: string;
}

export function UnsetUpstreamDialog({
  pending,
  busy,
  onCancel,
  onConfirm,
}: {
  pending: PendingUnsetUpstream;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Unset upstream for ${pending.branch}?`}
      command={pending.command}
      losing={[
        `the follow ${pending.branch} has to a remote branch`,
        'the silent destination every later push and pull reads',
      ]}
      confirmLabel="Unset upstream"
    />
  );
}
