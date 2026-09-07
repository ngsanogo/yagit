import type { PullStrategy, Remote, WorkingDirectory } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Tooltip } from '../components/Tooltip';
import {
  canForcePush,
  forcePushUnavailableReason,
  pullDescription,
  pushDescription,
  remoteOffers,
  type PushOffer,
} from './remote';

/**
 * Fetch, pull and push, in that order, beside the branch they act on.
 *
 * The order is the order of the decision: a fetch tells you what has happened
 * elsewhere and changes nothing, a pull brings it here, a push sends yours
 * back. Established clients put the same three at the top of the window, and it is
 * right to — they are the only operations in a git client that involve
 * somebody else.
 *
 * Two buttons and a menu, rather than five buttons. The three items behind the
 * menu are the ones wanted rarely and never in a hurry: the two other ways to
 * pull, which exist for the afternoon the fast-forward is refused, and the
 * force push, which exists for the afternoon a rewrite has to go out. That is
 * the same line RefSidebar draws between a row's button and a row's menu.
 *
 * What the buttons carry is the counts. "Pull" is a verb whose object the user
 * has to go and find; "Pull ↓3" is the sentence and the button at once, and it
 * is the one thing on this screen that says the repository has fallen behind.
 */

interface RemoteBarProps {
  /** Undefined while the first status is still being read. */
  status?: WorkingDirectory;
  remotes: Remote[];

  /** Which operation is running, so the button that started it can say so. */
  busy?: 'fetch' | 'pull' | 'push';

  /** The last progress line from the operation that is running, if any. */
  progress?: string;

  onFetch: () => void;
  onPull: (strategy: PullStrategy) => void;
  /** The ordinary push: to the upstream, without forcing. */
  onPush: () => void;
  /** Opens the dialog that names a remote and records it. */
  onPublish: () => void;
  /** Opens the confirmation, which shows the command the daemon answered. */
  onForcePush: () => void;
  /** Opens the remotes list — add, rename, remove. */
  onManageRemotes: () => void;
  /** Opens the add-remote form directly (empty repositories). */
  onAddRemote: () => void;
  /**
   * Whether `remotes` is the answer or the placeholder before it.
   *
   * An empty list means two different things — this repository has no remote,
   * and the query has not come back — and they get opposite bars. Drawing
   * "Add remote" over a repository that has origin, for the one frame before
   * the list lands, offers the wrong action and teaches the wrong thing about
   * the repository.
   */
  known: boolean;
}

export function RemoteBar({
  status,
  remotes,
  busy,
  progress,
  onFetch,
  onPull,
  onPush,
  onPublish,
  onForcePush,
  onManageRemotes,
  onAddRemote,
  known,
}: RemoteBarProps) {
  const offers = remoteOffers(status, remotes.length);

  // Nothing is drawn until the list has answered: see `known` above.
  if (!known) {
    return null;
  }

  // A repository with no remote still needs a way onto the network. The three
  // operation buttons would be permanent refusals; Add remote is the only
  // action that makes sense until at least one exists.
  if (!offers.canFetch) {
    return (
      <div className="flex shrink-0 items-center gap-1.5">
        <Button size="sm" variant="ghost" onClick={onAddRemote}>
          Add remote
        </Button>
      </div>
    );
  }

  const upstream = status?.upstream;

  return (
    <div className="flex shrink-0 flex-col items-end gap-0.5">
      <div className="flex items-center gap-1.5">
        <Tooltip label={fetchDescription(remotes)}>
          <Button size="sm" variant="ghost" loading={busy === 'fetch'} onClick={onFetch}>
            Fetch
          </Button>
        </Tooltip>

        <Tooltip label={pullDescription(offers.pull, upstream)}>
          <Button
            size="sm"
            variant="ghost"
            loading={busy === 'pull'}
            disabled={offers.pull.kind !== 'pull'}
            onClick={() => onPull('ff-only')}
          >
            Pull
            {offers.pull.kind === 'pull' && offers.pull.behind > 0 && (
              <Badge tone="warning" className="ml-1.5">{`↓${offers.pull.behind}`}</Badge>
            )}
          </Button>
        </Tooltip>

        <Tooltip label={pushDescription(offers.push, upstream)}>
          <PushButton
            offer={offers.push}
            busy={busy === 'push'}
            onPush={onPush}
            onPublish={onPublish}
          />
        </Tooltip>

        <Menu
          label="More remote actions"
          items={remoteMenu(offers, onPull, onForcePush, onManageRemotes)}
        />
      </div>

      {busy !== undefined && progress !== undefined && progress !== '' && (
        <p className="max-w-xs truncate font-mono text-2xs text-ink-subtle" title={progress}>
          {progress}
        </p>
      )}
    </div>
  );
}

/**
 * The push, in whichever of its two forms applies.
 *
 * A branch that has never been pushed is PUBLISHED: a different word, because
 * it is a different operation — it needs a remote chosen and it records the
 * choice — and a button that said "Push" for both would be one word for two
 * things, the second of which asks a question.
 */
function PushButton({
  offer,
  busy,
  onPush,
  onPublish,
  ...described
}: {
  offer: PushOffer;
  busy: boolean;
  onPush: () => void;
  onPublish: () => void;
  /**
   * Passed through rather than named, and it has to be: Tooltip puts its
   * description on its child with cloneElement, and a child that is a
   * component of this project's rather than a `<button>` swallows the
   * attribute silently — leaving one button in three with no description,
   * which nothing on screen would show.
   */
  'aria-describedby'?: string;
}) {
  if (offer.kind === 'publish') {
    return (
      <Button size="sm" variant="ghost" loading={busy} onClick={onPublish} {...described}>
        Publish branch
      </Button>
    );
  }

  return (
    <Button
      size="sm"
      variant="ghost"
      loading={busy}
      {...described}
      // Nothing ahead is nothing to send, and a button whose only outcome is
      // "Everything up-to-date" teaches the user that the button does nothing.
      // The force push in the menu stays reachable, which is what the case
      // this refuses — a branch reset behind its remote — actually needs.
      disabled={offer.kind !== 'push' || offer.ahead === 0}
      onClick={onPush}
    >
      Push
      {offer.kind === 'push' && offer.ahead > 0 && (
        <Badge tone="success" className="ml-1.5">{`↑${offer.ahead}`}</Badge>
      )}
    </Button>
  );
}

/**
 * The ways to pull that are not the default, and the push that needs
 * permission.
 *
 * Each is refused rather than dropped when it does not apply, for the reason
 * the branch menu refuses a delete: an item that comes and goes teaches nobody
 * where the action went, and a menu the arrows step over is one a screen
 * reader can never be told about.
 */
function remoteMenu(
  offers: ReturnType<typeof remoteOffers>,
  onPull: (strategy: PullStrategy) => void,
  onForcePush: () => void,
  onManageRemotes: () => void,
): MenuItem[] {
  const pullReason = offers.pull.kind !== 'pull' ? offers.pull.reason : undefined;
  const forceReason = canForcePush(offers.push)
    ? undefined
    : forcePushUnavailableReason(offers.push);

  return [
    menuItem({ id: 'merge', label: 'Pull and merge', onSelect: () => onPull('merge') }, pullReason),
    menuItem(
      { id: 'rebase', label: 'Pull and rebase', onSelect: () => onPull('rebase') },
      pullReason,
    ),
    menuItem(
      { id: 'force-push', label: 'Force push…', danger: true, onSelect: onForcePush },
      forceReason,
    ),
    menuItem({ id: 'manage', label: 'Manage remotes…', onSelect: onManageRemotes }),
  ];
}

/**
 * What fetching will reach, named.
 *
 * "Nothing here moves" is the half worth saying: it is the only one of the
 * three that cannot change a file or a branch, and that is what makes it the
 * safe thing to press when you do not yet know what you want.
 */
function fetchDescription(remotes: Remote[]): string {
  const where =
    remotes.length === 1 && remotes[0] !== undefined
      ? remotes[0].name
      : `all ${remotes.length} remotes`;
  return `Read what changed on ${where} — nothing here moves`;
}
