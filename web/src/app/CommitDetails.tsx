import { useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import { ApiError } from '../api/client';
import type { FileDiff, HistoryScope, LocatedCommit } from '../api/types';
import { Avatar } from '../components/Avatar';
import { Button } from '../components/Button';
import { Badge, RefBadge } from '../components/Badge';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { Tooltip } from '../components/Tooltip';
import { copyLabel, useClipboard } from '../lib/clipboard';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { formatAbsoluteTime, formatRelativeTime, pluralize, shortenSha } from '../lib/format';
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
              reads. */}
          {onCheckOut !== undefined && (
            <Button
              size="sm"
              onClick={onCheckOut}
              loading={checkingOut}
              aria-label={`Check out ${shortenSha(sha)}, leaving HEAD detached`}
            >
              Check out
            </Button>
          )}

          {onCherryPick !== undefined && (
            <CommitAction
              label="Cherry-pick"
              description={`Cherry-pick ${shortenSha(sha)} onto the current branch`}
              onAct={onCherryPick}
              busy={cherryPicking}
              refusal={cherryPickRefusal}
            />
          )}

          {onRevert !== undefined && (
            <CommitAction
              label="Revert"
              description={`Revert ${shortenSha(sha)} on the current branch`}
              onAct={onRevert}
              busy={reverting}
              refusal={revertRefusal}
            />
          )}

          {onReset !== undefined && (
            <CommitAction
              label="Reset"
              description={`Reset the current branch to ${shortenSha(sha)}`}
              onAct={onReset}
              busy={resetting}
              refusal={resetRefusal}
            />
          )}

          {onRewrite !== undefined && (
            <CommitAction
              label="Rewrite after"
              description={`Plan a rewrite of the commits after ${shortenSha(sha)}`}
              onAct={onRewrite}
              busy={rewriting}
              refusal={rewriteRefusal}
            />
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
      description={
        missing && scope === 'head'
          ? 'This commit is reachable from a reference and from nothing that is checked out. Choose "All references" above to draw it.'
          : ''
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
      <header className="flex shrink-0 flex-col gap-2 border-b border-line px-3 py-2.5">
        <div className="flex min-w-0 items-start gap-2">
          <p className="min-w-0 flex-1 text-sm font-medium text-ink">{detail.subject}</p>
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
          <pre className="max-h-32 overflow-auto font-sans text-xs whitespace-pre-wrap text-ink-muted">
            {detail.body}
          </pre>
        )}

        <div className="flex flex-wrap items-center gap-2 text-2xs text-ink-subtle">
          <Avatar name={detail.author} />
          <span className="text-ink-muted">{detail.author}</span>
          <span title={formatAbsoluteTime(authored)}>
            {formatRelativeTime(authored, new Date())}
          </span>

          {/* Only when the two differ, which is what makes it worth saying: a
              rebase, a cherry-pick, a patch somebody else applied. Printing an
              identical pair on every commit is how a field stops being read. */}
          {detail.committer !== detail.author && (
            <span>
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
            className="py-8"
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
 * Two shapes and not one, so the offered button is exactly the Check out
 * button beside it — same size, same nothing on hover. The refused one is
 * wrapped, because a disabled Button drops pointer events: the browser fires
 * no hover on it and a `title` would be readable by nobody, precisely when the
 * sentence is needed. Tooltip hovers the span around it and reaches the button
 * through aria-describedby. See Tooltip's own comment, and OperationBanner,
 * which refuses its three the same way.
 *
 * One component for both, because all that differs between them is the word on
 * the button and the sentence in its accessible name. A copy per operation is
 * a second place for the Tooltip wrapping to be forgotten, and a refusal
 * nobody can read is exactly the one that had something to say.
 */
function CommitAction({
  label,
  description,
  onAct,
  busy,
  refusal,
}: {
  label: string;
  description: string;
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
    >
      {label}
    </Button>
  );

  if (refusal === undefined) {
    return button;
  }
  return <Tooltip label={refusal}>{button}</Tooltip>;
}
