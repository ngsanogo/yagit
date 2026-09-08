import { useQuery } from '@tanstack/react-query';

import { ApiError } from '../api/client';
import type { StashDetail } from '../api/types';
import { Badge } from '../components/Badge';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { Spinner } from '../components/Spinner';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { formatExactTime, formatRelativeTime, pluralize, shortenSha } from '../lib/format';
import { ReadOnlyPatch } from './DiffView';
import { stashRef } from './stash';
import { stashQuery } from './useStash';

/**
 * What one stash holds, in the slot a selected commit uses.
 *
 * The same panel shape and the same read-only diff, because it is the same
 * kind of thing to look at: a tree somebody saved, and what it differs from.
 * There is nothing here to stage — a stash is not the work tree — so the
 * actions the changes view puts on a diff are absent, as they are for a commit.
 *
 * It titles itself from what came back rather than from the row that was
 * clicked, and that is not pedantry. The route takes a POSITION, and a push or
 * a drop in another window renumbers every entry: asking for stash@{1} can
 * honestly answer with a different stash than the one under the pointer. Since
 * this is a read, that is harmless — as long as the screen says which stash it
 * is actually showing.
 */

interface StashDetailsProps {
  repositoryId: string;
  /** The position to read. Keyed on it by the caller, so a new one starts fresh. */
  index: number;
  onClose: () => void;
}

export function StashDetails({ repositoryId, index, onClose }: StashDetailsProps) {
  const detail = useQuery(stashQuery(repositoryId, index));

  return (
    <Panel
      title="Stash"
      className="h-full"
      actions={<CloseButton label="Close the stash" onClose={onClose} />}
      flush
    >
      {detail.isPending && (
        <div className="grid h-full place-items-center">
          <Spinner label="Reading the stash" />
        </div>
      )}

      {detail.isError && <Refusal error={detail.error} index={index} />}

      {detail.data !== undefined && <Body detail={detail.data} />}
    </Panel>
  );
}

/**
 * Why the stash is not on screen.
 *
 * The 404 is the one worth naming, and it means something specific here: the
 * stack is shorter than the list this page drew. That happens for an ordinary
 * reason — somebody dropped it, here or in a terminal — so the panel says so
 * rather than reporting that a stash is unreadable.
 */
function Refusal({ error, index }: { error: Error; index: number }) {
  const missing = error instanceof ApiError && error.status === 404;

  return (
    <EmptyState
      title={
        missing ? 'No longer in the stack' : (refusalHeading(error) ?? 'Could not read this stash')
      }
      // A failed read names the command and git's own stderr instead, which
      // is what `detail` carries below. The empty string this used to pass is
      // gone with the prop's requirement.
      description={
        missing
          ? `${stashRef(index)} is not there any more. The list beside this one is what the repository holds now.`
          : undefined
      }
      detail={missing ? undefined : errorDescription(error)}
    />
  );
}

function Body({ detail }: { detail: StashDetail }) {
  const made = new Date(detail.date);
  const message = detail.message === '' ? stashRef(detail.index) : detail.message;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Three quarters of the panel at most and scrolling, for the reason
          the commit panel's header is: one that holds its height inside a
          Panel that clips what leaves it cuts off everything under it and
          gives no sign it has, and one free to take every pixel it asks for
          leaves the diff at nothing. */}
      <header className="flex max-h-[75%] min-h-0 flex-col gap-2 overflow-y-auto border-b border-line px-3 py-2.5">
        <div className="flex min-w-0 items-start gap-2">
          {/* Two lines, then an ellipsis, with the whole message on hover.
              git writes this sentence itself — "On <branch>: <subject>" — so
              its length is the branch's plus the commit's, and a stash made
              on a long branch name wrapped the panel's own header off the
              bottom of it. */}
          <p className="line-clamp-2 min-w-0 flex-1 text-sm font-medium text-ink" title={message}>
            {message}
          </p>
          {/* The position it is at NOW, from the answer. The row that opened
              this may have been drawn against a stack that has since moved. */}
          <code className="shrink-0 font-mono text-2xs text-ink-subtle">
            {stashRef(detail.index)}
          </code>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-2xs text-ink-subtle">
          {detail.branch === '' ? (
            // git records "(no branch)" for a stash made on a detached HEAD.
            // Saying it in words beats drawing a badge naming a branch that
            // does not exist.
            <span>made away from any branch</span>
          ) : (
            <>
              <span>on</span>
              <Badge>{detail.branch}</Badge>
            </>
          )}
          {/* The clock, which the visible form drops: past a week
              formatRelativeTime is a bare date, so the hover used to hand back
              the string it was attached to. A stash is made and put back
              within a day more often than any other object here, and the hour
              is what tells two of them apart. */}
          <span title={formatExactTime(made)}>{formatRelativeTime(made, new Date())}</span>
          <code className="font-mono" title={detail.sha}>
            {shortenSha(detail.sha)}
          </code>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Badge>{pluralize(detail.files.length, 'file')}</Badge>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-auto">
        {detail.files.length === 0 ? (
          <EmptyState
            title="This stash holds nothing"
            description="git makes no such stash, so this one was written by something else."
          />
        ) : (
          <ReadOnlyPatch files={detail.files} />
        )}
      </div>
    </div>
  );
}
