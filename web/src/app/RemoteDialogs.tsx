import { useState } from 'react';

import type { PushPlan, Remote } from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';

/**
 * The two pushes that ask first.
 *
 * Everything else on the remote bar runs on the click. These two do not, and
 * for opposite reasons: one has a question that must be answered before it can
 * run at all, and the other is the only operation in this application that can
 * take work away from somebody who is not sitting here.
 *
 * Both are built on a plan the daemon answered — the exact command, and the
 * destination it read off the branch's upstream. Nothing here assembles a
 * refspec: the line shown and the line git receives have one definition
 * between them, which is the same rule the discard and delete confirmations
 * hold to.
 */

/**
 * Publishing a branch: where it goes, and what that records.
 *
 * A dialog rather than a button that picks for you, even when there is one
 * remote to pick. `--set-upstream` is a decision with a life beyond this
 * click — every later push and pull follows it silently — and a choice made
 * once and remembered forever is exactly the kind that should be visible when
 * it is made.
 */
export function PublishBranchDialog({
  branch,
  remotes,
  chosen,
  plan,
  busy,
  onChoose,
  onCancel,
  onPublish,
}: {
  branch: string;
  remotes: Remote[];
  /** The remote currently selected, which the plan below was asked for. */
  chosen: string;
  /** Undefined while the daemon is answering what the push would run. */
  plan: PushPlan | undefined;
  busy: boolean;
  onChoose: (remote: string) => void;
  onCancel: () => void;
  onPublish: () => void;
}) {
  return (
    <Dialog
      open
      onClose={onCancel}
      title={`Publish ${branch}`}
      description="The branch does not exist on any remote yet. Publishing it sends it and records where it went, so later pushes and pulls need no answer."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            // Refused until the daemon has said what would run. The button's
            // whole promise is the command above it, and a click that ran
            // something the dialog had not shown would break exactly that.
            disabled={plan === undefined}
            onClick={onPublish}
          >
            Publish
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Select
          label="Remote"
          value={chosen}
          onChange={(event) => onChoose(event.target.value)}
          options={remotes.map((remote) => ({
            value: remote.name,
            // The URL beside the name, because "origin" says nothing about
            // which machine it is on — and a fork and its upstream are both
            // called something short and forgettable.
            label: `${remote.name} — ${remote.fetch_url}`,
          }))}
          disabled={busy || remotes.length === 1}
          hint={
            remotes.length === 1
              ? 'The only remote this repository has.'
              : 'The branch will follow the one you pick.'
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

/**
 * Force pushing: what it overwrites, and the lease that limits it.
 *
 * The one confirmation in this application whose loss is somebody else's. What
 * it names is therefore not "your commits" but the commits on the remote —
 * and the two flags in the command are the reason the sentence can be as
 * narrow as it is: `--force-with-lease` refuses unless the remote is where
 * this repository last saw it, and `--force-if-includes` refuses unless what
 * is being overwritten is in this branch's history.
 */
export function ForcePushDialog({
  plan,
  busy,
  onCancel,
  onConfirm,
}: {
  /** Undefined while the daemon answers; the dialog is not up until it has. */
  plan: PushPlan | undefined;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  if (plan === undefined) {
    return null;
  }

  const destination = `${plan.remote}/${plan.ref.replace(/^refs\/heads\//, '')}`;

  return (
    <ConfirmDialog
      open
      destructive
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Force push ${plan.local_branch} to ${destination}?`}
      command={plan.command}
      losing={[
        `any commit on ${destination} that ${plan.local_branch} does not have`,
        'the ability of anyone who has already pulled it to pull again cleanly',
      ]}
      confirmLabel="Force push"
    />
  );
}

/**
 * The configured remotes: list, add, rename, remove — one dialog depth at a
 * time.
 *
 * The list is its own view rather than a settings page: there is nowhere else
 * in the workbench that names a remote as configuration rather than as a
 * remote-tracking branch, and putting add/rename/remove on every origin/main
 * row would look like properties of that branch.
 */

export type ManageRemotesView =
  | { kind: 'list' }
  | { kind: 'add' }
  | { kind: 'rename'; name: string }
  | { kind: 'set-url'; name: string; current: string; url: string; plan?: string }
  | { kind: 'remove'; name: string; command: string };

export function ManageRemotesDialog({
  remotes,
  view,
  busy,
  planningUrl,
  onClose,
  onView,
  onAdd,
  onRename,
  onPlanSetUrl,
  onSetUrl,
  onProposeRemove,
  onConfirmRemove,
}: {
  remotes: Remote[];
  view: ManageRemotesView;
  busy: boolean;
  /** True while the set-url plan is being read. */
  planningUrl: boolean;
  onClose: () => void;
  onView: (view: ManageRemotesView) => void;
  onAdd: (name: string, url: string) => void;
  onRename: (from: string, to: string) => void;
  /** Re-asks the daemon for the command as the URL is typed. */
  onPlanSetUrl: (name: string, url: string) => void;
  onSetUrl: (name: string, url: string) => void;
  onProposeRemove: (name: string) => void;
  onConfirmRemove: () => void;
}) {
  if (view.kind === 'add') {
    return <AddRemoteDialog busy={busy} onCancel={() => onView({ kind: 'list' })} onAdd={onAdd} />;
  }

  if (view.kind === 'rename') {
    return (
      <RenameRemoteDialog
        name={view.name}
        busy={busy}
        onCancel={() => onView({ kind: 'list' })}
        onRename={(to) => onRename(view.name, to)}
      />
    );
  }

  if (view.kind === 'set-url') {
    return (
      <SetRemoteURLDialog
        name={view.name}
        current={view.current}
        url={view.url}
        plan={view.plan}
        busy={busy}
        planning={planningUrl}
        onCancel={() => onView({ kind: 'list' })}
        onUrlChange={(url) => onPlanSetUrl(view.name, url)}
        onSetUrl={(url) => onSetUrl(view.name, url)}
      />
    );
  }

  if (view.kind === 'remove') {
    return (
      <ConfirmDialog
        open
        destructive
        busy={busy}
        onCancel={() => onView({ kind: 'list' })}
        onConfirm={onConfirmRemove}
        title={`Remove remote ${view.name}?`}
        command={view.command}
        losing={[`the remote ${view.name}`, `every remote-tracking branch under ${view.name}/`]}
        confirmLabel="Remove"
      />
    );
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title="Remotes"
      description="Where this repository fetches from and pushes to."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onClose}>
            Close
          </Button>
          <Button variant="primary" disabled={busy} onClick={() => onView({ kind: 'add' })}>
            Add remote
          </Button>
        </>
      }
    >
      {remotes.length === 0 ? (
        <p className="text-sm text-ink-muted">No remotes configured yet.</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {remotes.map((remote) => (
            <li
              key={remote.name}
              className="flex items-center gap-2 rounded-sm px-2 py-1.5 hover:bg-hover"
            >
              <div className="min-w-0 flex-1">
                <p className="truncate font-mono text-xs text-ink">{remote.name}</p>
                <p className="truncate font-mono text-2xs text-ink-subtle" title={remote.fetch_url}>
                  {remote.fetch_url}
                </p>
              </div>
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() => onView({ kind: 'rename', name: remote.name })}
              >
                Rename
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() =>
                  onView({ kind: 'set-url', name: remote.name, current: remote.fetch_url, url: '' })
                }
              >
                Edit URL…
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() => onProposeRemove(remote.name)}
              >
                Remove…
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Dialog>
  );
}

function AddRemoteDialog({
  busy,
  onCancel,
  onAdd,
}: {
  busy: boolean;
  onCancel: () => void;
  onAdd: (name: string, url: string) => void;
}) {
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const trimmedName = name.trim();
  const trimmedUrl = url.trim();

  return (
    <Dialog
      open
      onClose={onCancel}
      title="Add remote"
      description="A name and a URL. Fetching is a separate step once it is here."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={trimmedName === '' || trimmedUrl === ''}
            onClick={() => onAdd(trimmedName, trimmedUrl)}
          >
            Add
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field
          label="Name"
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="origin"
          autoComplete="off"
          spellCheck={false}
          autoFocus
        />
        <Field
          label="URL"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          placeholder="https://example.com/repo.git"
          autoComplete="off"
          spellCheck={false}
        />
      </div>
    </Dialog>
  );
}

function RenameRemoteDialog({
  name,
  busy,
  onCancel,
  onRename,
}: {
  name: string;
  busy: boolean;
  onCancel: () => void;
  onRename: (to: string) => void;
}) {
  const [to, setTo] = useState(name);
  const trimmed = to.trim();

  return (
    <Dialog
      open
      onClose={onCancel}
      title={`Rename ${name}`}
      description="Remote-tracking branches move with the name."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={trimmed === '' || trimmed === name}
            onClick={() => onRename(trimmed)}
          >
            Rename
          </Button>
        </>
      }
    >
      <Field
        label="New name"
        value={to}
        onChange={(event) => setTo(event.target.value)}
        autoComplete="off"
        spellCheck={false}
        autoFocus
      />
    </Dialog>
  );
}

function SetRemoteURLDialog({
  name,
  current,
  url,
  plan,
  busy,
  planning,
  onCancel,
  onUrlChange,
  onSetUrl,
}: {
  name: string;
  /** The redacted fetch URL, for display only — never prefilled into the field. */
  current: string;
  url: string;
  /** Exact command the daemon answered, or undefined while it is answering. */
  plan: string | undefined;
  busy: boolean;
  planning: boolean;
  onCancel: () => void;
  onUrlChange: (url: string) => void;
  onSetUrl: (url: string) => void;
}) {
  const trimmed = url.trim();
  const canSave = trimmed !== '' && plan !== undefined && !planning;

  return (
    <Dialog
      open
      onClose={onCancel}
      title={`Edit URL for ${name}`}
      description="Where this remote is fetched from. Fetching is a separate step once it is saved."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={busy}
            disabled={!canSave}
            onClick={() => onSetUrl(trimmed)}
          >
            Save
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field
          label="New URL"
          value={url}
          onChange={(event) => onUrlChange(event.target.value)}
          placeholder="https://example.com/repo.git"
          autoComplete="off"
          spellCheck={false}
          autoFocus
          hint={`Current: ${current}`}
        />
        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-ink-muted">yagit will run</p>
          {trimmed === '' ? (
            <p className="text-sm text-ink-muted">Type the new URL.</p>
          ) : planning || plan === undefined ? (
            <Spinner label="Reading what set-url would run" />
          ) : (
            <GitCommand command={plan} />
          )}
        </div>
      </div>
    </Dialog>
  );
}
