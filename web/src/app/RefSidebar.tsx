import type { ReactNode } from 'react';

import type { Head, Ref } from '../api/types';
import { Badge, RefBadge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { Tooltip } from '../components/Tooltip';
import { cx } from '../lib/cx';
import { shortenSha } from '../lib/format';
import type { CheckOutRequest } from './useCheckOut';

/**
 * Branches, remotes and tags, grouped by what they are.
 *
 * git returns them in one flat list because that is what `for-each-ref`
 * answers; grouping is a reading decision and belongs here, not in the API.
 *
 * Two things a row can do, and they are kept apart because they are not the
 * same click. The row itself takes the history to the commit the reference
 * names — reading, reversible, the thing wanted most often. Checking out is a
 * button of its own beside it, so that following a reference to look at it can
 * never move the repository by accident.
 *
 * The rest go behind a menu, and that is where the line is drawn: a button on
 * the row is for the thing wanted often enough to be worth the width, and
 * everything else is one press away rather than in the way. Renaming, merging
 * and deleting a branch are the everything else.
 */

const GROUPS: { kind: Ref['kind']; title: string }[] = [
  { kind: 'branch', title: 'Branches' },
  { kind: 'remote', title: 'Remotes' },
  { kind: 'tag', title: 'Tags' },
  // Notes, bisect marks, whatever another tool wrote under refs/. `git log
  // --all` walks them like any other reference, so dropping them here left
  // commits in the graph whose name appeared nowhere on screen.
  { kind: 'other', title: 'Other' },
];

/**
 * The one reference this list does not draw.
 *
 * `for-each-ref` answers with refs/stash like any other, and it used to land
 * in "Other" for the reason that group exists: a commit in the graph with no
 * name anywhere on screen is worse than an odd row. The stash panel below has
 * since become that name, and a better one — it shows the whole stack, where
 * this row showed only whichever entry happens to be on top of it, under a
 * label that reads like a branch.
 */
const STASH_REF = 'refs/stash';

interface RefSidebarProps {
  refs: Ref[];
  /** Where HEAD sits, or undefined in a repository with no commit yet. */
  head?: Head;
  /** Takes the history to a commit, and selects it. */
  onGoTo: (sha: string) => void;
  /**
   * Checks out a reference, or undefined where nothing can be checked out.
   *
   * Undefined for a bare repository, which has no work tree to check anything
   * out into — the daemon refuses the request, and offering a button for it
   * would be offering an error.
   */
  onCheckOut?: (request: CheckOutRequest) => void;
  /**
   * The `ref` of the request currently in flight, if any.
   *
   * Compared against what checkOutRequestFor builds below rather than against
   * a name read off the row, so the spinner and the request are looking at one
   * string. A tag and a branch can share a short name; only what git is
   * actually being sent tells the two rows apart.
   */
  checkingOut?: string;
  /**
   * Renames a local branch, or undefined where nothing can be renamed.
   *
   * Local branches only, here and below. A tag is not renamed — it is made
   * again somewhere else and the old one deleted — and a remote-tracking
   * branch is a copy of something on another machine, which this cannot move.
   */
  onRenameBranch?: (name: string) => void;
  /** Records where a local branch follows, without pushing. */
  onSetUpstream?: (name: string) => void;
  /** Forgets what a local branch follows. */
  onUnsetUpstream?: (name: string) => void;
  /** Deletes a local branch, after the confirmation the caller puts up. */
  onDeleteBranch?: (name: string) => void;
  /**
   * Merges a local branch into the one HEAD is on, after the confirmation the
   * caller puts up.
   *
   * Undefined only where no repository of this shape could ever merge: a bare
   * one, which has no work tree to merge into. A detached HEAD is a state and
   * not a shape, so it leaves the item on the menu and refused — see
   * branchMenuItems.
   */
  onMergeBranch?: (name: string) => void;
  /**
   * Rebases the current branch onto a local branch, after the confirmation the
   * caller puts up.
   *
   * Undefined for the same reason merge is: a bare repository has no work tree.
   * A detached HEAD leaves the item on the menu and refused.
   */
  onRebaseBranch?: (name: string) => void;
  /**
   * Opens the new-branch dialog.
   *
   * In the panel header rather than on a row, because a new branch does not
   * belong to any of them: it starts where HEAD is, and HEAD is a property of
   * the repository. Putting it on the row of the branch HEAD happens to be on
   * would make it look like a property of that branch.
   */
  onNewBranch?: () => void;
  /**
   * Opens the new-tag dialog.
   *
   * Beside New branch in the panel header: a tag starts at a commit, not at a
   * row of the Tags group, and putting Create on every tag row would look like
   * a property of that tag.
   */
  onNewTag?: () => void;
  /**
   * Why New tag is refused, when it is — a repository with no commit to tag.
   *
   * The sentence rather than a boolean, and the button rather than nothing at
   * all, for the reason the stash panel gives: a control greyed with nothing
   * to say reads as a screen that is broken.
   */
  newTagUnavailableReason?: string;
  /** Opens the delete-tag confirmation for this short name. */
  onDeleteTag?: (name: string) => void;
  /**
   * Opens the push-tag dialog for this short name.
   *
   * Absent when the callback is not wired. When present but the repository
   * has no remote, pass pushUnavailableReason so the item stays and refuses.
   */
  onPushTag?: (name: string) => void;
  /** Why Push… is refused, when it is — usually no remote configured. */
  pushTagUnavailableReason?: string;
}

export function RefSidebar({
  refs,
  head,
  onGoTo,
  onCheckOut,
  checkingOut,
  onRenameBranch,
  onSetUpstream,
  onUnsetUpstream,
  onDeleteBranch,
  onMergeBranch,
  onRebaseBranch,
  onNewBranch,
  onNewTag,
  onDeleteTag,
  onPushTag,
  pushTagUnavailableReason,
  newTagUnavailableReason,
}: RefSidebarProps) {
  const headerActions =
    onNewBranch === undefined && onNewTag === undefined ? undefined : (
      <div className="flex items-center gap-1">
        {onNewBranch !== undefined && (
          <Button size="sm" variant="ghost" onClick={onNewBranch}>
            New branch…
          </Button>
        )}
        {onNewTag !== undefined && (
          <NewTagAction onNewTag={onNewTag} refusal={newTagUnavailableReason} />
        )}
      </div>
    );

  // Everything this list draws, in one pass: refs/stash is the one reference
  // it never shows, and asking each group for it separately would leave the
  // question of whether anything was drawn at all to a fifth filter that
  // disagreed with the four above it.
  const listed = refs.filter((ref) => ref.name !== STASH_REF);
  const drewNothing = listed.length === 0 && head?.detached !== true;

  // Sized to what it holds, and capped: 24rem is about a dozen rows and their
  // headings, which is more than a sidebar is read at a glance. Past the cap
  // the list scrolls inside the panel — which is what keeps a repository with
  // a thousand tags from pushing the stash off the bottom of the column, and
  // the reason this cannot simply be the column's own scrolling.
  return (
    <Panel title="References" className="max-h-96 shrink-0" flush actions={headerActions}>
      <div className="h-full overflow-auto">
        {/* A detached HEAD is on no branch, so `for-each-ref` never mentions
            it and every group below would leave the screen saying nothing
            about where the repository actually is. */}
        {head?.detached === true && (
          <Group title="Detached HEAD">
            <Row
              label="HEAD"
              title={`HEAD, detached at ${head.sha}`}
              sha={head.sha}
              current
              onSelect={() => onGoTo(head.sha)}
            />
          </Group>
        )}

        {/* The panel is the only place a branch, a remote or a tag is named,
            so an empty one reads as a panel that failed to load rather than as
            a repository with nothing in it. Which of the two sentences applies
            is asked of HEAD and not of the list: no HEAD is a repository whose
            first commit has not happened, and it is the case somebody lands on
            straight out of the create flow. */}
        {drewNothing && (
          <EmptyState
            title={head === undefined ? 'No references yet' : 'Nothing to list'}
            description={
              head === undefined
                ? 'This repository has no commits yet, so nothing points anywhere. Make one and the branch it creates appears here.'
                : 'Branches, remotes and tags are what this panel lists, and this repository has none of the three yet. The first one you make appears here.'
            }
            compact
          />
        )}

        {GROUPS.map(({ kind, title }) => {
          const group = listed.filter((ref) => ref.kind === kind);
          if (group.length === 0) {
            return null;
          }

          return (
            <Group key={kind} title={title}>
              {group.map((ref) => {
                const current = isCurrent(ref, head);
                const request = checkOutRequestFor(ref);

                const actions = rowActions({
                  reference: ref,
                  current,
                  detached: head?.detached === true,
                  busy: checkingOut === request.ref,
                  ...(onCheckOut === undefined ? {} : { onCheckOut: () => onCheckOut(request) }),
                  ...(onRenameBranch === undefined ? {} : { onRename: onRenameBranch }),
                  ...(onSetUpstream === undefined ? {} : { onSetUpstream }),
                  ...(onUnsetUpstream === undefined ? {} : { onUnsetUpstream }),
                  ...(onDeleteBranch === undefined ? {} : { onDelete: onDeleteBranch }),
                  ...(onMergeBranch === undefined ? {} : { onMerge: onMergeBranch }),
                  ...(onRebaseBranch === undefined ? {} : { onRebase: onRebaseBranch }),
                  ...(onDeleteTag === undefined ? {} : { onDeleteTag }),
                  ...(onPushTag === undefined ? {} : { onPushTag }),
                  ...(pushTagUnavailableReason === undefined ? {} : { pushTagUnavailableReason }),
                });

                return (
                  <Row
                    key={ref.name}
                    label={ref.short_name}
                    title={ref.name}
                    sha={ref.sha}
                    current={current}
                    onSelect={() => onGoTo(ref.sha)}
                    action={actions.node}
                    actionReachesTheName={actions.wide}
                    // A checkout in flight keeps its button on screen. It is
                    // the one moment the spinner inside it is the only thing
                    // saying the click was heard, and a pointer that drifted
                    // off the row would otherwise take it away.
                    actionPinned={checkingOut === request.ref}
                  >
                    {current && <RefBadge kind="head" name="HEAD" current />}

                    {/* Tracking state, and only when there is something to
                        say. A branch level with its upstream needs no badge;
                        three of them in a row saying "0 / 0" is how a sidebar
                        stops being readable. */}
                    {ref.gone && <Badge tone="danger">gone</Badge>}
                    {ref.ahead > 0 && <Badge tone="success">{`↑${ref.ahead}`}</Badge>}
                    {ref.behind > 0 && <Badge tone="warning">{`↓${ref.behind}`}</Badge>}
                  </Row>
                );
              })}
            </Group>
          );
        })}
      </div>
    </Panel>
  );
}

/**
 * What checking out a row means, in the terms the daemon takes.
 *
 * A local branch is switched to and everything else is detached at, because
 * everything else is not a place HEAD can sit. Branches go by their short
 * name, which is the only form `git switch` accepts; the rest go by their full
 * one, which is the form that cannot be mistaken for another kind of ref with
 * the same short name.
 */
export function checkOutRequestFor(reference: Ref): CheckOutRequest {
  return {
    ref: reference.kind === 'branch' ? reference.short_name : reference.name,
    detach: reference.kind !== 'branch',
    label: reference.short_name,
  };
}

/**
 * Whether a reference is the one HEAD is on.
 *
 * The kind is part of the question, not decoration: a tag and a branch can
 * both be called `main`, and matching on the short name alone would put the
 * marker on whichever of them the list drew.
 */
function isCurrent(reference: Ref, head: Head | undefined): boolean {
  return (
    head !== undefined &&
    !head.detached &&
    reference.kind === 'branch' &&
    reference.short_name === head.name
  );
}

/**
 * The button that makes a tag, offered or refused.
 *
 * Two shapes for the reason the stash panel draws two: a disabled Button drops
 * pointer events, so the browser fires no hover on it and a `title` would be
 * readable by nobody — precisely when the sentence is needed. Tooltip hovers
 * the span around it and reaches the button through aria-describedby.
 *
 * And a native `title` on that same span, which is not belt and braces. This
 * button sits in a Panel header, and Panel is `overflow-hidden`; the bubble
 * hangs `bottom-full`, above a header that is already at the top of its box,
 * so it is clipped away entirely — the one control on the panel that has
 * something to explain is the one whose explanation cannot be seen. The
 * browser draws a `title` outside the page, where nothing clips it. The
 * bubble stays because it is what carries aria-describedby, and the day
 * Tooltip reaches the top layer (a new ADR against ADR 0018's third bullet)
 * the `title` is what comes back off.
 */
function NewTagAction({
  onNewTag,
  refusal,
}: {
  onNewTag: () => void;
  refusal: string | undefined;
}) {
  const button = (
    <Button size="sm" variant="ghost" onClick={onNewTag} disabled={refusal !== undefined}>
      New tag…
    </Button>
  );

  if (refusal === undefined) {
    return button;
  }
  return (
    <span className="inline-flex" title={refusal}>
      <Tooltip label={refusal} align="end">
        {button}
      </Tooltip>
    </span>
  );
}

/**
 * The button that moves the repository.
 *
 * One label for both outcomes, and the difference is in the accessible name
 * rather than in the words on screen. "Check out" is what the user is asking
 * for either way; that a tag or a remote-tracking branch leaves HEAD detached
 * is a fact about git, and the screen says it where the state lasts — the
 * badge above the history, for as long as HEAD is there — rather than in a
 * label that has to be read before every click.
 */
function CheckOutAction({
  reference,
  busy,
  onCheckOut,
}: {
  reference: Ref;
  busy: boolean;
  onCheckOut: () => void;
}) {
  const detaches = reference.kind !== 'branch';

  return (
    <Button
      size="sm"
      onClick={onCheckOut}
      loading={busy}
      aria-label={
        detaches
          ? `Check out ${reference.short_name}, leaving HEAD detached`
          : `Check out ${reference.short_name}`
      }
    >
      Check out
    </Button>
  );
}

/**
 * Everything laid over a row's right edge, or nothing at all, and how far in
 * it reaches.
 *
 * Nothing matters: the overlay it goes in is absolutely positioned over the
 * row, and one containing an empty fragment is an invisible box over the right
 * end of every tag in the list.
 *
 * `wide` is the check-out button, and it is a fact about width rather than
 * about the action: the menu on its own sits over the sha, while the button
 * beside it reaches on into the name. The row is what does something about
 * that, and this is the only place that knows which shape a row got.
 */
function rowActions({
  reference,
  current,
  detached,
  busy,
  onCheckOut,
  onRename,
  onSetUpstream,
  onUnsetUpstream,
  onMerge,
  onRebase,
  onDelete,
  onDeleteTag,
  onPushTag,
  pushTagUnavailableReason,
}: RowActions): { node: ReactNode; wide: boolean } {
  // Nothing to check out on the branch already checked out, and a button that
  // ran `git switch` for it would be a button whose only outcome is "already
  // on 'main'".
  const checkOut =
    onCheckOut === undefined || current ? null : (
      <CheckOutAction reference={reference} busy={busy} onCheckOut={onCheckOut} />
    );

  const items =
    reference.kind === 'tag'
      ? tagMenuItems({ reference, onDeleteTag, onPushTag, pushTagUnavailableReason })
      : branchMenuItems({
          reference,
          current,
          detached,
          onRename,
          onSetUpstream,
          onUnsetUpstream,
          onMerge,
          onRebase,
          onDelete,
        });
  if (checkOut === null && items.length === 0) {
    return { node: undefined, wide: false };
  }

  return {
    node: (
      <>
        {checkOut}
        {items.length > 0 && (
          <Menu label={`More actions for ${reference.short_name}`} items={items} />
        )}
      </>
    ),
    wide: checkOut !== null,
  };
}

/**
 * What a row offers, and who to tell when it is taken.
 *
 * Named rather than positional, and that is not a style preference: three of
 * these callbacks have the same type, so an argument list would let a rename
 * be wired to the delete confirmation and still compile.
 */
interface RowActions {
  reference: Ref;
  /** True on the branch HEAD is on. */
  current: boolean;
  /** True where HEAD is on no branch at all, so there is none to merge into. */
  detached: boolean;
  busy: boolean;
  onCheckOut?: () => void;
  onRename?: (name: string) => void;
  onSetUpstream?: (name: string) => void;
  onUnsetUpstream?: (name: string) => void;
  onMerge?: (name: string) => void;
  onRebase?: (name: string) => void;
  onDelete?: (name: string) => void;
  onDeleteTag?: (name: string) => void;
  onPushTag?: (name: string) => void;
  pushTagUnavailableReason?: string;
}

/**
 * What a tag can do past checking it out.
 *
 * Push first — sending a release is the ordinary next step after creating one
 * — then delete. Force-updating a tag on a remote is not offered.
 */
export function tagMenuItems({
  reference,
  onDeleteTag,
  onPushTag,
  pushTagUnavailableReason,
}: {
  reference: Ref;
  onDeleteTag?: (name: string) => void;
  onPushTag?: (name: string) => void;
  pushTagUnavailableReason?: string;
}): MenuItem[] {
  if (reference.kind !== 'tag') {
    return [];
  }

  const items: MenuItem[] = [];
  if (onPushTag !== undefined) {
    items.push(
      menuItem(
        { id: 'push-tag', label: 'Push…', onSelect: () => onPushTag(reference.short_name) },
        pushTagUnavailableReason,
      ),
    );
  }
  if (onDeleteTag !== undefined) {
    items.push({
      id: 'delete-tag',
      label: 'Delete…',
      danger: true,
      onSelect: () => onDeleteTag(reference.short_name),
    });
  }
  return items;
}

/**
 * What a branch can do past checking it out, behind one button.
 *
 * Empty on a tag or a remote-tracking branch: neither is a local branch, and a
 * menu whose only item is refused teaches the user that the menu is pointless
 * rather than that the action is. They get no button — Menu's own answer to an
 * empty list is a refused trigger, which is right in a list of rows that all
 * have one and wrong on a row where none ever will.
 *
 * The line between refusing an item and dropping it is what the repository can
 * ever do. A missing callback is a capability this repository does not have —
 * a bare one has no work tree, so nothing can be merged into or rebased onto
 * anything — and an action that will never apply belongs off the list.
 * Everything else stays and is refused: delete on the branch HEAD is on, merge
 * and rebase on that same branch and on every branch while HEAD is detached,
 * because git refuses each and the reason is worth reading. A menu that is
 * four items on one row and two on the next teaches nobody where the action
 * went, and one the arrows step over is one a screen reader can never be told
 * about.
 *
 * The order is what the arrows walk and what a screen reader hears, and it
 * runs from the reversible to the ruinous: rename, the merge that only adds,
 * the rebase that writes the branch's commits again, the delete. `danger` is
 * the only thing the menu can say about cost — the plan is not asked for until
 * an item is chosen — so it marks the last two and not the first two.
 *
 * Exported for its tests: a closed menu builds no items, so static markup
 * cannot be asked what is in one.
 */
export function branchMenuItems({
  reference,
  current,
  detached,
  onRename,
  onSetUpstream,
  onUnsetUpstream,
  onMerge,
  onRebase,
  onDelete,
}: Omit<RowActions, 'busy' | 'onCheckOut'>): MenuItem[] {
  if (reference.kind !== 'branch') {
    return [];
  }

  const name = reference.short_name;
  const items: MenuItem[] = [];

  // One callback, one item, and no callback answering for another: a caller
  // that can merge and cannot rename gets the merge. Reading the three as a
  // single gate meant a repository with one capability drew a button with
  // nothing behind it — or, where the gate closed, no button and no way to
  // find out why.
  if (onRename !== undefined) {
    items.push({ id: 'rename', label: 'Rename…', onSelect: () => onRename(name) });
  }

  if (onSetUpstream !== undefined) {
    items.push({
      id: 'set-upstream',
      label:
        reference.upstream !== undefined && reference.upstream !== ''
          ? 'Change upstream…'
          : 'Set upstream…',
      onSelect: () => onSetUpstream(name),
    });
  }

  if (
    onUnsetUpstream !== undefined &&
    reference.upstream !== undefined &&
    reference.upstream !== ''
  ) {
    items.push({
      id: 'unset-upstream',
      label: 'Unset upstream…',
      onSelect: () => onUnsetUpstream(name),
    });
  }

  if (onMerge !== undefined) {
    // Into itself is "Already up to date" with extra steps, and a detached
    // HEAD is on no branch for this to go into.
    items.push(
      menuItem(
        {
          id: 'merge',
          label: 'Merge into current branch…',
          onSelect: () => onMerge(name),
        },
        current
          ? `Merging ${name} into itself does nothing`
          : detached
            ? 'HEAD is detached, so there is no branch to merge into'
            : undefined,
      ),
    );
  }

  if (onRebase !== undefined) {
    // Red beside the delete, and for the same reason: in the ordinary case
    // this writes every commit on the branch again under a new hash and
    // recreates none of the merge commits among them. That it does nothing
    // where the branch already sits on this one is not a reason to draw it
    // as harmless — the menu is read before the plan is asked for, and the
    // one thing it can say here is which items take something away.
    //
    // Onto itself is "Current branch is up to date" with extra steps, and a
    // detached HEAD is on no branch to rebase.
    items.push(
      menuItem(
        {
          id: 'rebase',
          label: 'Rebase current branch onto this…',
          danger: true,
          onSelect: () => onRebase(name),
        },
        current
          ? `Rebasing onto ${name} would rebase it onto itself`
          : detached
            ? 'HEAD is detached, so there is no branch to rebase'
            : undefined,
      ),
    );
  }

  if (onDelete !== undefined) {
    items.push(
      menuItem(
        {
          id: 'delete',
          label: 'Delete…',
          danger: true,
          onSelect: () => onDelete(name),
        },
        current ? `HEAD is on ${name}, so it cannot be deleted` : undefined,
      ),
    );
  }

  return items;
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="sticky top-0 bg-surface px-3 py-1.5 text-2xs font-medium tracking-wide text-ink-subtle uppercase">
        {title}
      </h3>
      <ul>{children}</ul>
    </section>
  );
}

/**
 * What keeps an action out of the way until it is wanted.
 *
 * Opacity and pointer events move together on purpose. Transparent alone
 * leaves a button nobody can see and everybody can click, at the right edge of
 * a row whose own click means something else.
 */
const REVEALED_ON_ATTENTION = [
  'pointer-events-none opacity-0',
  'group-hover/ref:pointer-events-auto group-hover/ref:opacity-100',
  'group-focus-within/ref:pointer-events-auto group-focus-within/ref:opacity-100',
  // And while this row's menu is open. Its popover is in the browser's top
  // layer, which is nowhere near the row in the document — so focus-within is
  // false the whole time the menu has focus, and without this the button that
  // opened it would fade out from under the menu it opened.
  'group-has-[[aria-expanded=true]]/ref:pointer-events-auto',
  'group-has-[[aria-expanded=true]]/ref:opacity-100',
].join(' ');

/**
 * What the name gives up while a check-out button is over it.
 *
 * The overlay is opaque, so the tail of a truncated name does not go
 * half-legible under it — it disappears, ellipsis included, and what is left
 * reads as a whole branch name that happens to be shorter. That is the failure
 * worth fixing: not that characters are hidden, but that nothing says they
 * are. The padding is roughly what the check-out button reaches past the sha,
 * so the same characters are visible either way and the truncation is drawn
 * where the reader can see it. Only while the row is under attention, and only
 * on the rows that get the button — the menu alone sits over the sha and
 * covers no name at all.
 */
const NAME_YIELDS_TO_ACTION = [
  'group-hover/ref:pr-16',
  'group-focus-within/ref:pr-16',
  'group-has-[[aria-expanded=true]]/ref:pr-16',
].join(' ');

interface RowProps {
  label: string;
  /** The whole name, for the tooltip: the label is truncated. */
  title: string;
  sha: string;
  /** Marks the one row HEAD is on, for a screen reader as well as the eye. */
  current: boolean;
  onSelect: () => void;
  /** Badges: the HEAD marker, then whatever the tracking state has to say. */
  children?: ReactNode;
  /** The row's other clicks, laid over its right edge. */
  action?: ReactNode;
  /** Whether that action is wide enough to cover the name. See rowActions. */
  actionReachesTheName?: boolean;
  /** Keeps that action on screen whether or not the row is hovered. */
  actionPinned?: boolean;
}

function Row({
  label,
  title,
  sha,
  current,
  onSelect,
  children,
  action,
  actionReachesTheName = false,
  actionPinned = false,
}: RowProps) {
  return (
    <li className="group/ref relative">
      <button
        type="button"
        onClick={onSelect}
        aria-current={current ? true : undefined}
        // A height rather than padding around a line box. The row that carries
        // the HEAD badge is 2px taller than the rest of them, because the badge
        // is bordered and the bare text beside it is not — and it is always the
        // current branch, so the one row a reader looks for first is the one
        // that puts the column of shas out of step.
        className={cx(
          'flex h-8 w-full items-center gap-2 px-3 text-left outline-none',
          'transition-colors transition-instant hover:bg-hover focus-visible:focus-ring',
        )}
      >
        <span
          className={cx(
            'min-w-0 flex-1 truncate font-mono text-xs text-ink-muted',
            actionReachesTheName && NAME_YIELDS_TO_ACTION,
          )}
          title={title}
        >
          {label}
        </span>

        {children}

        <span className="font-mono text-2xs text-ink-subtle">{shortenSha(sha)}</span>
      </button>

      {/* Laid over the row's right edge rather than taking a column of its
          own. A sidebar is 288 pixels wide and holds a name that must not be
          truncated to nothing; reserving room for a button that is wanted on
          one row in twenty would spend that width on every other row.

          Hidden by opacity, never by `display`, and that is the accessibility
          of it: a button that is not rendered cannot be tabbed to, so a
          keyboard user would have no way to check anything out at all. It is
          transparent until the row is hovered or something inside it has
          focus — and inert until then too, so a click landing near the right
          edge of a row reaches the row rather than an invisible button. */}
      {action !== undefined && (
        <div
          className={cx(
            'absolute inset-y-0 right-2 flex items-center gap-1',
            'transition-opacity transition-instant',
            actionPinned ? 'opacity-100' : REVEALED_ON_ATTENTION,
          )}
        >
          {action}
        </div>
      )}
    </li>
  );
}
