import { useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import { ApiError } from '../api/client';
import type { FileDiff, HistoryScope, LocatedCommit } from '../api/types';
import { Avatar } from '../components/Avatar';
import { Button } from '../components/Button';
import { Badge, RefBadge } from '../components/Badge';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { Tooltip } from '../components/Tooltip';
import { copyLabel, useClipboard } from '../lib/clipboard';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { formatExactTime, formatRelativeTime, pluralize, shortenSha } from '../lib/format';
import { decorationBadge } from './CommitList';
import { refsKey } from './historyScope';
import { signatureNote } from './signature';
import { ReadOnlyPatch } from './DiffView';

/**
 * The commit a row in the history was selected on: its message, who wrote it,
 * and what it changed.
 *
 * Read-only, and it reuses the diff the changes view uses with its actions
 * removed. A commit is history — there is nothing in it to stage, and a
 * screen that offered would be offering something the daemon has no route
 * for.
 */

/**
 * The one description of what the daemon knows about a commit.
 *
 * It has two readers: this panel, and the history view, which fetches through
 * the same cache to learn which row a reference points at. Two descriptions
 * would be two cache keys, and one click would ask the daemon twice.
 *
 * The scope is in the key because it is in the answer: `row` is a position in
 * one walk, so the same commit read under another scope is a different answer
 * to a different question — and under `refs` the chosen set is the scope, so
 * it is in the key too (docs/adr/0033).
 */
export function commitQuery(
  repositoryId: string,
  sha: string,
  scope: HistoryScope,
  refs: readonly string[] = [],
) {
  return {
    queryKey: ['commit', repositoryId, scope, refsKey(scope, refs), sha],
    queryFn: () => api.showCommit(repositoryId, sha, scope, refs),
    // A commit's content is a hash of itself, so no interval and no window
    // focus can teach this anything. What is NOT immutable is what points at
    // it — a branch moved onto it, a tag added, the commit itself rewritten
    // away — and that arrives as a repository event, which invalidates this
    // key by name.
    staleTime: Infinity,
  };
}

interface CommitDetailsProps {
  repositoryId: string;
  sha: string;
  scope: HistoryScope;
  /** The chosen references, under `scope=refs` and empty otherwise. */
  refs?: readonly string[];
  onClose: () => void;
  /**
   * Checks this commit out, or undefined where nothing can be.
   *
   * Undefined for a bare repository: there is no work tree to check it out
   * into, and the daemon refuses the request rather than pretending otherwise.
   */
  onCheckOut?: () => void;
  /** Whether that checkout is running. */
  checkingOut?: boolean;
  /**
   * Cherry-picks this commit onto the branch HEAD is on, or undefined where no
   * repository of this shape could ever do it: a bare one, which has no work
   * tree to pick into.
   *
   * A detached HEAD is a state and not a shape, so it leaves the button on the
   * panel and refused — see cherryPickRefusal. branchMenuItems draws the same
   * line for merge and rebase, and for the same reason: an action that
   * disappears teaches nobody where it went.
   */
  onCherryPick?: () => void;
  /** Whether that cherry-pick plan is being read. */
  cherryPicking?: boolean;
  /**
   * Why cherry-picking is refused right now, or undefined when it is offered.
   *
   * The sentence rather than a boolean, for the reason OperationBanner's
   * blockedBy answers with one: a button greyed with nothing to say reads as a
   * screen that is broken rather than as a repository that is not ready.
   */
  cherryPickRefusal?: string;
  /**
   * Reverts this commit on the branch HEAD is on, or undefined where no
   * repository of this shape could ever do it — same bare-repository rule as
   * onCherryPick.
   */
  onRevert?: () => void;
  /** Whether that revert plan is being read. */
  reverting?: boolean;
  /** Why reverting is refused right now, or undefined when it is offered. */
  revertRefusal?: string;
  /**
   * Resets the branch HEAD is on to this commit, or undefined where no
   * repository of this shape could ever do it — same bare-repository rule as
   * onCherryPick.
   */
  onReset?: () => void;
  /** Whether that reset plan is being read. */
  resetting?: boolean;
  /** Why resetting is refused right now, or undefined when it is offered. */
  resetRefusal?: string;
  /**
   * Opens a plan over the commits AFTER this one, or undefined where no
   * repository of this shape could ever do it — same bare-repository rule as
   * onCherryPick.
   *
   * After, and not including: the commit clicked is what the rewritten ones
   * are replayed on top of, which is `git rebase --interactive <commit>` and
   * the reason the button does not say this commit's name.
   */
  onRewrite?: () => void;
  /** Whether that range is being read. */
  rewriting?: boolean;
  /** Why rewriting is refused right now, or undefined when it is offered. */
  rewriteRefusal?: string;
  /** Opens the history of a path in this commit's tree. */
  onFileHistory?: (path: string) => void;
  /** Opens the blame of a path at this commit. */
  onBlame?: (path: string) => void;
}

export function CommitDetails({
  repositoryId,
  sha,
  scope,
  refs = [],
  onClose,
  onCheckOut,
  checkingOut = false,
  onCherryPick,
  cherryPicking = false,
  cherryPickRefusal,
  onRevert,
  reverting = false,
  revertRefusal,
  onReset,
  resetting = false,
  resetRefusal,
  onRewrite,
  rewriting = false,
  rewriteRefusal,
  onFileHistory,
  onBlame,
}: CommitDetailsProps) {
  const detail = useQuery(commitQuery(repositoryId, sha, scope, refs));
  const rewrites = commitMenuItems({
    onReset,
    resetting,
    resetRefusal,
    onRewrite,
    rewriting,
    rewriteRefusal,
  });

  return (
    <Panel
      title="Commit"
      className="h-full"
      actions={
        <>
          {/* A commit is not a branch, so this always leaves HEAD detached.
              Said in the accessible name rather than on the button, and shown
              for as long as it lasts by the badge above the history — a label
              that has to be read before every click is a label nobody
              reads. The word on it carries no ellipsis because this is the
              one action here that acts on the click rather than asking
              first. */}
          {onCheckOut !== undefined && (
            <Button
              size="sm"
              onClick={onCheckOut}
              loading={checkingOut}
              aria-label={`Check out ${shortenSha(sha)}, leaving HEAD detached`}
              title="Puts this commit in the work tree, on no branch."
            >
              Check out
            </Button>
          )}

          {onCherryPick !== undefined && (
            <CommitAction
              label="Cherry-pick…"
              description={`Cherry-pick ${shortenSha(sha)} onto the current branch`}
              hint="Copies this change onto your branch as a new commit."
              onAct={onCherryPick}
              busy={cherryPicking}
              refusal={cherryPickRefusal}
            />
          )}

          {onRevert !== undefined && (
            <CommitAction
              label="Revert…"
              description={`Revert ${shortenSha(sha)} on the current branch`}
              hint="Adds a commit that undoes this one."
              onAct={onRevert}
              busy={reverting}
              refusal={revertRefusal}
            />
          )}

          {rewrites.length > 0 && (
            <Menu label={`More actions for ${shortenSha(sha)}`} items={rewrites} />
          )}

          <CloseButton label="Close the commit" onClose={onClose} />
        </>
      }
      flush
    >
      {detail.isPending && (
        <div className="grid h-full place-items-center">
          <Spinner label="Reading the commit" />
        </div>
      )}

      {detail.isError && <Refusal error={detail.error} scope={scope} />}

      {detail.data !== undefined && (
        <Body detail={detail.data} onFileHistory={onFileHistory} onBlame={onBlame} />
      )}
    </Panel>
  );
}

/**
 * git's verdict on the signature, where it has one.
 *
 * Three tones and not two, because "signed" is not a yes-or-no: a good
 * signature from a key this machine does not trust, an expired key and a
 * signature git had no key to check are none of them a forgery and none of
 * them a clean verification. signatureNote is where the eight letters become
 * those words, so the mapping can be asserted without a browser.
 *
 * The signer's name is printed only when git read one that is not already in
 * the author line — which is the case worth seeing, a commit signed by
 * somebody other than the person it is attributed to. Printing it on every
 * verified commit would be a second author line under the first.
 */
function SignatureBadge({
  verdict,
  signer,
  author,
}: {
  verdict: string;
  signer?: string;
  author: string;
}) {
  const note = signatureNote(verdict);
  if (note === undefined) {
    return null;
  }

  const named = signer !== undefined && signer !== '' && !author.includes(signer);

  return (
    <>
      <Badge tone={note.tone}>{note.label}</Badge>
      {named && <span>by {signer}</span>}
    </>
  );
}

/**
 * Why the commit is not on screen.
 *
 * The 404 is the one worth naming. Under the default scope it is an ordinary
 * thing to ask for — a branch nobody has merged is in the reference list and
 * in no walk from HEAD — and there is something to do about it, so the panel
 * says which walk was searched rather than reporting that the commit does not
 * exist. It does exist; the picture being drawn does not reach it.
 */
function Refusal({ error, scope }: { error: Error; scope: HistoryScope }) {
  const missing = error instanceof ApiError && error.status === 404;

  return (
    <EmptyState
      // A commit that regenerated a lockfile answers 413, and its message is
      // git's about a command that was stopped. "Could not read this commit"
      // over that says the commit is unreadable, when what is unreadable is
      // its patch.
      title={
        missing
          ? 'Not in the history being drawn'
          : (refusalHeading(error) ?? 'Could not read this commit')
      }
      // Only the 404 under the default scope has a next action to name. Every
      // other refusal answers with git's own account of it, which is `detail`
      // below — the empty string this used to pass went with the prop's
      // requirement.
      description={
        missing && scope === 'head'
          ? 'This commit is reachable from a reference and from nothing that is checked out. Choose "All references" above to draw it.'
          : undefined
      }
      detail={missing ? undefined : errorDescription(error)}
    />
  );
}

function Body({
  detail,
  onFileHistory,
  onBlame,
}: {
  detail: LocatedCommit;
  onFileHistory?: (path: string) => void;
  onBlame?: (path: string) => void;
}) {
  const clipboard = useClipboard();
  const authored = new Date(detail.date);
  const committed = new Date(detail.committer_date);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Three quarters of the panel at most, and it scrolls whatever will
          not fit. A header that holds its height inside a Panel that clips
          what leaves it is a header that is silently cut off: on a 720px
          window the parents, the file count and the entire patch sat below
          the panel's bottom edge, with no scrollbar and no ellipsis to say
          so. The ceiling is what stops the repair from becoming the same
          fault pointing the other way — a header free to ask for every pixel
          it wants leaves the patch at no height at all, and the patch is what
          reading a commit means.

          A share of the panel and not a count of pixels, because the number
          that is right in a tall pane is the whole of a short one. Three
          quarters rather than a half: the ordinary header — a subject, an
          author, a parent and the files badge — is under that at every height
          this panel takes, so the ceiling only meets the commit with a long
          body or a dozen references, and the quarter it holds back is what
          keeps a diff on the screen when it does. */}
      <header className="flex max-h-3/4 min-h-0 flex-col gap-2 overflow-y-auto border-b border-line px-3 py-2.5">
        <div className="flex min-w-0 items-start gap-2">
          {/* Two lines, then an ellipsis, with the whole subject on hover.
              Reverts and merges write long ones, and beside a sha that holds
              its width an ordinary 178-character subject wrapped to thirty
              lines and pushed everything under it out of the panel. The text
              is untouched — line-clamp is paint, not content — so a screen
              reader still reads the subject in full. */}
          <p
            className="line-clamp-2 min-w-0 flex-1 text-sm font-medium text-ink"
            title={detail.subject}
          >
            {detail.subject}
          </p>
          {/* The whole SHA, and a way to take it. It is the string you carry
              to a terminal, so an abbreviation you cannot copy is the one
              thing this pane must not show. */}
          <code className="shrink-0 font-mono text-2xs break-all text-ink-subtle">
            {detail.sha}
          </code>
          <Button size="sm" variant="ghost" onClick={() => void clipboard.copy(detail.sha)}>
            {copyLabel(clipboard.state, 'Copy SHA')}
          </Button>
        </div>

        {detail.body !== '' && (
          // pre-wrap: a commit body is a written document with paragraphs and
          // indented lists, and collapsing its whitespace would rewrite it.
          //
          // Capped, and therefore focusable. Eight lines is what the box
          // shows and a body worth reading is longer, so without a tab stop
          // the ninth line is reachable by pointer alone — WCAG 2.1.1, and
          // invisible to the axe gate because every fixture commit is written
          // with a one-line `-m`. `group` rather than `region`: the panel
          // around this is already a named landmark, and nesting a second one
          // inside it puts a scroll box into the list a screen reader offers
          // as the parts of the screen.
          <pre
            tabIndex={0}
            role="group"
            aria-label="Commit message body"
            className="max-h-32 overflow-auto font-sans text-xs whitespace-pre-wrap text-ink-muted outline-none focus-visible:focus-ring"
          >
            {detail.body}
          </pre>
        )}

        <div className="flex flex-wrap items-center gap-2 text-2xs text-ink-subtle">
          {/* Decorative, because the name is written out beside it. The
              initials are visible text and join the accessible name of
              whatever contains them, so this line was heard as "AL Ada
              Lovelace" — two letters that mean nothing, in front of the fact
              they encode. Where the chip stands alone it is the only thing
              naming the author and it stays announced. */}
          <Avatar name={detail.author} decorative />
          <span className="text-ink-muted">{detail.author}</span>
          {/* The clock, which the visible form does not carry: past a week
              formatRelativeTime is a bare date, so this hover used to hand
              back the string it is attached to. A working day's commits share
              a date and the list's order is topological rather than
              chronological, so the hour is the one thing that puts two of them
              in order — and the daemon has been sending it all along. */}
          <span title={formatExactTime(authored)}>{formatRelativeTime(authored, new Date())}</span>

          {/* Only when the two differ, which is what makes it worth saying: a
              rebase, a cherry-pick, a patch somebody else applied. Printing an
              identical pair on every commit is how a field stops being read. */}
          {detail.committer !== detail.author && (
            <span title={formatExactTime(committed)}>
              committed by {detail.committer}, {formatRelativeTime(committed, new Date())}
            </span>
          )}

          {detail.refs.map((ref) => (
            <RefBadge key={ref} {...decorationBadge(ref)} />
          ))}

          {/* Only where there is something to say. Most commits in most
              repositories are unsigned, and a badge on every one of them is a
              badge nobody reads — signatureNote answers nothing for that
              ordinary case, and nothing for a daemon too old to have sent a
              verdict at all. */}
          <SignatureBadge
            verdict={detail.signature}
            signer={detail.signer}
            author={detail.author}
          />
        </div>

        {/* The parents, which are what make a merge a merge — and the one
            thing about a commit that the patch above cannot show. A heading
            rather than a label, so the block is reachable as a landmark and
            reads as a section to anyone who is not looking at the layout. */}
        <div className="flex flex-wrap items-baseline gap-2 text-2xs text-ink-subtle">
          <h3 className="font-medium tracking-wide uppercase">
            {detail.parents.length === 1 ? 'Parent' : 'Parents'}
          </h3>
          {detail.parents.length === 0 ? (
            <span className="text-ink-muted">None: this is a root commit.</span>
          ) : (
            detail.parents.map((parent) => (
              <code key={parent} className="font-mono text-ink-muted" title={parent}>
                {shortenSha(parent)}
              </code>
            ))
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Badge>{summarise(detail.files)}</Badge>
          {detail.against_first_parent && (
            // A merge has one answer per parent and git prints none of them by
            // default. This one is the first parent's, and saying so is the
            // difference between a useful default and a quiet half-truth.
            <Badge tone="info">merge — shown against its first parent</Badge>
          )}
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-auto">
        {detail.files.length === 0 ? (
          <EmptyState
            title="This commit changed nothing"
            description={
              detail.against_first_parent
                ? 'A merge that resolved to its first parent leaves no difference against it.'
                : 'An empty commit records a moment rather than a change.'
            }
          />
        ) : (
          <ReadOnlyPatch
            files={detail.files}
            {...(onFileHistory === undefined ? {} : { onFileHistory })}
            {...(onBlame === undefined ? {} : { onBlame })}
          />
        )}
      </div>
    </div>
  );
}

/** "3 files" — what the commit touched, counted once. */
function summarise(files: FileDiff[]): string {
  return pluralize(files.length, 'file');
}

/**
 * One of the commit panel's git actions — Cherry-pick, Revert — offered or
 * refused.
 *
 * Two shapes and not one. The refused one is wrapped, because a disabled
 * Button drops pointer events: the browser fires no hover on it and a `title`
 * would be readable by nobody, precisely when the sentence is needed. Tooltip
 * hovers the span around it and reaches the button through aria-describedby.
 * See Tooltip's own comment, and OperationBanner, which refuses its three the
 * same way.
 *
 * The offered one explains itself through a native `title`, and that is not a
 * cheaper Tooltip. This row is a Panel's header: Panel is `overflow-hidden`,
 * and a bubble hangs above its anchor, so a Tooltip on a button six pixels
 * from the panel's top edge is painted outside the panel and clipped away
 * unread — the trap Tooltip's own comment names for scroll containers, met
 * from the other side. A native tooltip is drawn by the browser and nothing
 * on the page can clip it, which is why this codebase already uses `title`
 * for hover text inside anything that clips.
 *
 * That is why the refused one carries a `title` on its wrapping span as well
 * as the bubble, and the two are not a belt and braces. The bubble is the half
 * a screen reader hears, and aria-describedby is the only route into a control
 * that has dropped its pointer events; the `title` is the half a pointer can
 * read, because the bubble is painted above this header and clipped away like
 * any other. Neither of the two reaches both readers on its own.
 *
 * The sentence differs from the accessible name rather than repeating it. The
 * name says which commit the action is for, because that is what a screen
 * reader needs and the sha beside the button cannot say; the hint says what
 * the operation does, because that is what nobody looking at the screen was
 * ever told. Hanging the same string on both would make a description that is
 * read out twice and listened to once.
 *
 * The label ends in an ellipsis: the click opens a confirmation rather than
 * acting, which is the promise every menu item that opens a dialog already
 * makes.
 *
 * One component for both, because all that differs between them is the word on
 * the button and the sentence in its accessible name. A copy per operation is
 * a second place for the Tooltip wrapping to be forgotten, and a refusal
 * nobody can read is exactly the one that had something to say.
 */
function CommitAction({
  label,
  description,
  hint,
  onAct,
  busy,
  refusal,
}: {
  label: string;
  description: string;
  hint: string;
  onAct: () => void;
  busy: boolean;
  refusal: string | undefined;
}) {
  const button = (
    <Button
      size="sm"
      onClick={onAct}
      loading={busy}
      disabled={refusal !== undefined}
      aria-label={description}
      // Only while it can be hovered. A refused Button has no pointer events,
      // so its own `title` would never fire — the refusal is hung on the span
      // around it instead, below.
      {...(refusal === undefined ? { title: hint } : {})}
    >
      {label}
    </Button>
  );

  if (refusal === undefined) {
    return button;
  }

  // The span is what both of them hang on: it is the element that still has
  // pointer events once the button has given them up.
  return (
    <span className="inline-flex" title={refusal}>
      <Tooltip label={refusal}>{button}</Tooltip>
    </span>
  );
}

/**
 * The two operations that move the branch, behind one button.
 *
 * They used to sit in the header as two more chips exactly like Check out —
 * five controls of identical weight, in which the one that moves a branch and
 * the one that writes every commit after this one again were drawn as the one
 * that checks a commit out. A menu is the vocabulary this design system
 * already has for that, and the sidebar puts merge, rebase and delete behind
 * one for the same reason: it gives the row a reading order, and it costs a
 * second, deliberate click on the two operations worth hesitating over.
 *
 * Ordered from the reversible to the ruinous, which is the order the arrow
 * keys walk and a screen reader hears — the rule branchMenuItems states.
 *
 * Neither item names the commit, where the buttons they replace spelled it
 * into every accessible name. The trigger they hang from does — "More actions
 * for 132fc55" — and it is read on the way in, so repeating the sha on each
 * item would only be the same seven characters heard three times. It is what
 * the sidebar's branch menu already does with the branch name.
 *
 * `danger` marks the rewrite and not the reset, and the line is what each one
 * always does. A rewrite always writes the commits after this one again under
 * new hashes; a reset is soft, mixed or hard, the mode is chosen in the dialog
 * this opens, and reset.ts is explicit that only hard discards anything.
 * Reddening it would say "this destroys work" about an operation that usually
 * does not, and a warning that cries wolf is one nobody reads on the day it
 * means it.
 *
 * Refused while its own plan is being read. The Button these replace refused
 * its second click through `loading`; without this, reopening the menu during
 * the read would fire a second plan for the same commit — and a grey item
 * with nothing to say is how people learn that a menu is broken, so it says
 * what the first click is doing.
 *
 * Exported for its tests: a closed menu builds no items, so static markup
 * cannot be asked what is in one.
 */
export function commitMenuItems({
  onReset,
  resetting,
  resetRefusal,
  onRewrite,
  rewriting,
  rewriteRefusal,
}: {
  onReset: (() => void) | undefined;
  resetting: boolean;
  resetRefusal: string | undefined;
  onRewrite: (() => void) | undefined;
  rewriting: boolean;
  rewriteRefusal: string | undefined;
}): MenuItem[] {
  const items: MenuItem[] = [];

  if (onReset !== undefined) {
    items.push(
      menuItem(
        { id: 'reset', label: 'Reset the current branch to this…', onSelect: onReset },
        resetRefusal ?? (resetting ? 'Reading what this reset would do' : undefined),
      ),
    );
  }

  if (onRewrite !== undefined) {
    items.push(
      menuItem(
        {
          id: 'rewrite',
          label: 'Rewrite the commits after this…',
          danger: true,
          onSelect: onRewrite,
        },
        rewriteRefusal ?? (rewriting ? 'Reading what this rewrite would do' : undefined),
      ),
    );
  }

  return items;
}
