import { useState } from 'react';

import type { Remote, TagPushPlan } from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { SegmentedControl, type Segment } from '../components/SegmentedControl';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';

/**
 * Creating a tag, deleting one locally, and pushing one to a remote.
 *
 * Annotated is the default: a release tag with a message. Lightweight is a
 * choice on the same dialog — a name pointing at a commit, no tag object.
 * git validates the name. What is refused locally is only the empty name, and
 * for annotated tags the empty message — both would be buttons that run a
 * command to do nothing useful, and both are cheaper to catch here than after
 * a round trip.
 */

export interface PendingTagDelete {
  name: string;
  command: string;
}

const TAG_KINDS: readonly Segment<'annotated' | 'lightweight'>[] = [
  { value: 'annotated', label: 'Annotated' },
  { value: 'lightweight', label: 'Lightweight' },
];

export function CreateTagDialog({
  busy,
  targetLabel,
  onCancel,
  onCreate,
}: {
  busy: boolean;
  /** Where the tag will point, in words — "main", "a2801ba". */
  targetLabel: string;
  onCancel: () => void;
  onCreate: (name: string, message: string, annotated: boolean) => void;
}) {
  const [name, setName] = useState('');
  const [message, setMessage] = useState('');
  const [kind, setKind] = useState<'annotated' | 'lightweight'>('annotated');

  const trimmedName = name.trim();
  const trimmedMessage = message.trim();
  const annotated = kind === 'annotated';
  const canCreate = trimmedName !== '' && (!annotated || trimmedMessage !== '');

  return (
    <Dialog
      open
      onClose={onCancel}
      title="New tag"
      description={
        annotated
          ? `An annotated tag on ${targetLabel}.`
          : `A lightweight tag on ${targetLabel} — a name pointing at the commit, with no message of its own.`
      }
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!canCreate}
            onClick={() => onCreate(trimmedName, trimmedMessage, annotated)}
          >
            Create
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field
          label="Name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="v1.0.0"
          autoComplete="off"
          spellCheck={false}
          autoFocus
        />
        <SegmentedControl
          label="Kind of tag"
          segments={TAG_KINDS}
          value={kind}
          onChange={setKind}
          disabled={busy}
        />
        {annotated && (
          <Field
            label="Message"
            value={message}
            onChange={(event) => setMessage(event.target.value)}
            placeholder="What this release is"
            autoComplete="off"
          />
        )}
      </div>
    </Dialog>
  );
}

export function DeleteTagDialog({
  pending,
  busy,
  onCancel,
  onConfirm,
}: {
  pending: PendingTagDelete;
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
      title={`Delete tag ${pending.name}?`}
      command={pending.command}
      losing={[`the tag ${pending.name}`]}
      confirmLabel="Delete"
      description="Only the local tag is removed. A copy on a remote stays until it is deleted there separately."
    />
  );
}

/**
 * Pushing a tag: where it goes, and the exact command.
 *
 * Same shape as publishing a branch — a remote chosen, a plan answered, a
 * button that runs that plan. Not destructive: it adds a tag on the remote
 * rather than taking one away, so ConfirmDialog's loss list does not apply.
 */
export function PushTagDialog({
  tag,
  remotes,
  chosen,
  plan,
  busy,
  onChoose,
  onCancel,
  onPush,
}: {
  tag: string;
  remotes: readonly Remote[];
  chosen: string;
  /** Undefined while the daemon is answering. */
  plan: TagPushPlan | undefined;
  busy: boolean;
  onChoose: (remote: string) => void;
  onCancel: () => void;
  onPush: () => void;
}) {
  return (
    <Dialog
      open
      onClose={onCancel}
      title={`Push ${tag}`}
      description="Sends this tag to the remote under the same name. Moving a tag that is already there is not offered here."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" loading={busy} disabled={plan === undefined} onClick={onPush}>
            Push
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Select
          label="Remote"
          value={chosen}
          onChange={(event) => onChoose(event.target.value)}
          options={remotes.map((remote) => ({
            value: remote.name,
            label: `${remote.name} — ${remote.fetch_url}`,
          }))}
          disabled={busy || remotes.length === 1}
          hint={
            remotes.length === 1
              ? 'The only remote this repository has.'
              : 'Where the tag will be created.'
          }
        />

        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-ink-muted">yagit will run</p>
          {plan === undefined ? (
            <Spinner label="Reading what the push would run" />
          ) : (
            <GitCommand command={plan.command} />
          )}
        </div>
      </div>
    </Dialog>
  );
}
