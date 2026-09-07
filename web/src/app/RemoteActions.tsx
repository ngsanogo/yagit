import { useQuery } from '@tanstack/react-query';
import { useRef, useState } from 'react';

import type { PushPlan, Remote, Repository, WorkingDirectory } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { RemoteBar } from './RemoteBar';
import {
  ForcePushDialog,
  ManageRemotesDialog,
  PublishBranchDialog,
  type ManageRemotesView,
} from './RemoteDialogs';
import { remotesQuery, useRemote } from './useRemote';
import { useRemoteManage } from './useRemoteManage';

/**
 * The network, wired: the bar, its dialogs, and the queries behind them.
 *
 * A component of its own rather than more state in the workbench. What it
 * holds is not the workbench's business — which remote a publish is pointed
 * at, which command the daemon said a force push would run, which remotes
 * dialog is open — and every one of those lives exactly as long as the dialog
 * that asks about it.
 *
 * Publish and force-push open on an answer rather than beside one, which is
 * the shape the discard and the branch delete use: their whole content is a
 * command the daemon assembled. Managing remotes is local to this bar for the
 * same reason: the list is configuration, not a workbench-wide operation.
 */

export function RemoteActions({
  repository,
  status,
}: {
  repository: Repository;
  /** Undefined while the first status is still being read. */
  status: WorkingDirectory | undefined;
}) {
  const toast = useToast();
  const remote = useRemote(repository.id);
  const manage = useRemoteManage(repository.id);

  const remotes = useQuery(remotesQuery(repository.id));

  const [publishing, setPublishing] = useState<PublishState>();
  const [forcing, setForcing] = useState<PushPlan>();
  const [managing, setManaging] = useState<ManageRemotesView>();
  // Which set-url plan request is still current — typing faster than the
  // daemon answers is ordinary.
  const setUrlPlanGeneration = useRef(0);

  const configured = remotes.data ?? [];

  /**
   * Opens the publish dialog on a destination the daemon has described.
   *
   * The remote it starts on is the first in git's own list — which is `origin`
   * on all but the unusual repository, because git sorts by name and that is
   * what a clone calls the place it came from. It is a starting point and not
   * a decision: the dialog shows it, and the select next to it is how it is
   * changed.
   */
  const proposePublish = () => {
    const first = configured[0];
    if (first === undefined) {
      return;
    }
    planPushTo(first.name, false, (plan) => setPublishing({ remote: first.name, plan }));
  };

  /** Re-asks the daemon when the remote in the dialog changes. */
  const choosePublishRemote = (name: string) => {
    // The command on screen must never describe a remote other than the one
    // selected, so it goes while the new answer is on its way rather than
    // standing there wrong.
    setPublishing({ remote: name, plan: undefined });
    planPushTo(name, false, (plan) => setPublishing({ remote: name, plan }));
  };

  const proposeForcePush = () => {
    planPushTo('', true, setForcing);
  };

  function planPushTo(name: string, force: boolean, then: (plan: PushPlan) => void) {
    remote.planPush.mutate(
      { remote: name, force },
      {
        onSuccess: then,
        onError: (error) => {
          toast.push({
            tone: 'danger',
            title: 'Could not read what the push would run',
            detail: errorDescription(error),
          });
        },
      },
    );
  }

  const proposeRemove = (name: string) => {
    manage.planRemove.mutate(name, {
      onSuccess: ({ command }) => setManaging({ kind: 'remove', name, command }),
      onError: (error) => {
        toast.push({
          tone: 'danger',
          title: `Could not read what removing ${name} would run`,
          detail: errorDescription(error),
        });
      },
    });
  };

  const planSetUrl = (name: string, url: string) => {
    const current =
      managing?.kind === 'set-url'
        ? managing.current
        : (configured.find((remote) => remote.name === name)?.fetch_url ?? '');
    const generation = ++setUrlPlanGeneration.current;
    setManaging({ kind: 'set-url', name, current, url, plan: undefined });
    const trimmed = url.trim();
    if (trimmed === '') {
      return;
    }
    manage.planSetUrl.mutate(
      { name, url: trimmed },
      {
        onSuccess: ({ command }) => {
          if (generation !== setUrlPlanGeneration.current) {
            return;
          }
          setManaging({ kind: 'set-url', name, current, url, plan: command });
        },
        onError: (error) => {
          if (generation !== setUrlPlanGeneration.current) {
            return;
          }
          toast.push({
            tone: 'danger',
            title: `Could not read what updating ${name} would run`,
            detail: errorDescription(error),
          });
        },
      },
    );
  };

  // planRemove and planSetUrl are in here too: between clicking and the daemon
  // answering with the command, the rows must not take a second click.
  const manageBusy =
    manage.add.isPending ||
    manage.rename.isPending ||
    manage.setUrl.isPending ||
    manage.planRemove.isPending ||
    manage.remove.isPending;

  return (
    <>
      <RemoteBar
        status={status}
        remotes={configured}
        busy={busyOperation(remote)}
        progress={remote.progress}
        onFetch={() => remote.fetchFrom.mutate('')}
        onPull={(strategy) => remote.pull.mutate(strategy)}
        onPush={() =>
          remote.push.mutate({ remote: '', force: false, label: pushLabel(status, configured) })
        }
        onPublish={proposePublish}
        onForcePush={proposeForcePush}
        onManageRemotes={() => setManaging({ kind: 'list' })}
        onAddRemote={() => setManaging({ kind: 'add' })}
        known={remotes.isFetched}
      />

      {publishing !== undefined && status !== undefined && (
        <PublishBranchDialog
          branch={status.branch}
          remotes={configured}
          chosen={publishing.remote}
          plan={publishing.plan}
          busy={remote.push.isPending}
          onChoose={choosePublishRemote}
          onCancel={() => setPublishing(undefined)}
          onPublish={() => {
            const plan = publishing.plan;
            if (plan === undefined) {
              return;
            }
            remote.push.mutate(
              {
                remote: publishing.remote,
                force: false,
                label: `${status.branch} to ${publishing.remote}`,
                lease: { local_branch: plan.local_branch, ref: plan.ref },
              },
              { onSuccess: () => setPublishing(undefined) },
            );
          }}
        />
      )}

      {forcing !== undefined && (
        <ForcePushDialog
          plan={forcing}
          busy={remote.push.isPending}
          onCancel={() => setForcing(undefined)}
          onConfirm={() =>
            remote.push.mutate(
              {
                remote: '',
                force: true,
                label: forcing.local_branch,
                lease: { local_branch: forcing.local_branch, ref: forcing.ref },
              },
              // Settled rather than succeeded: a refused force push has said
              // everything it has to say in its toast, and leaving the dialog
              // up would ask the same question a second time over a lease that
              // is not going to be there either.
              { onSettled: () => setForcing(undefined) },
            )
          }
        />
      )}

      {managing !== undefined && (
        <ManageRemotesDialog
          remotes={configured}
          view={managing}
          busy={manageBusy}
          planningUrl={manage.planSetUrl.isPending}
          onClose={() => setManaging(undefined)}
          onView={setManaging}
          onAdd={(name, url) =>
            manage.add.mutate({ name, url }, { onSuccess: () => setManaging({ kind: 'list' }) })
          }
          onRename={(from, to) =>
            manage.rename.mutate({ from, to }, { onSuccess: () => setManaging({ kind: 'list' }) })
          }
          onPlanSetUrl={planSetUrl}
          onSetUrl={(name, url) =>
            manage.setUrl.mutate({ name, url }, { onSuccess: () => setManaging({ kind: 'list' }) })
          }
          onProposeRemove={proposeRemove}
          onConfirmRemove={() => {
            if (managing.kind !== 'remove') {
              return;
            }
            manage.remove.mutate(managing.name, {
              onSuccess: () => setManaging({ kind: 'list' }),
            });
          }}
        />
      )}
    </>
  );
}

/** The remote a publish is pointed at, and what the daemon says it would run. */
interface PublishState {
  remote: string;
  /** Undefined while the daemon is answering. */
  plan: PushPlan | undefined;
}

/**
 * Which of the three is running.
 *
 * One at a time is not enforced and does not need to be: each button shows its
 * own spinner, and git takes its own locks. What this decides is only which
 * button says so.
 */
function busyOperation(
  remote: ReturnType<typeof useRemote>,
): 'fetch' | 'pull' | 'push' | undefined {
  if (remote.fetchFrom.isPending) {
    return 'fetch';
  }
  if (remote.pull.isPending) {
    return 'pull';
  }
  if (remote.push.isPending) {
    return 'push';
  }
  return undefined;
}

/**
 * How the ordinary push is named in its toast.
 *
 * Read off the status rather than off a plan, because the ordinary push does
 * not ask for one — there is nothing to confirm. The upstream is what the
 * status calls it, "origin/main", which is also the name the sidebar draws.
 */
function pushLabel(status: WorkingDirectory | undefined, remotes: Remote[]): string {
  if (status === undefined) {
    return 'the current branch';
  }
  if (status.upstream !== undefined && status.upstream !== '') {
    return `${status.branch} to ${status.upstream}`;
  }
  const first = remotes[0];
  return first === undefined ? status.branch : `${status.branch} to ${first.name}`;
}
