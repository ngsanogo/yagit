import { useQuery } from '@tanstack/react-query';

import { api } from '../api/client';
import type { BlameLine } from '../api/types';
import { CloseButton } from '../components/CloseButton';
import { EmptyState } from '../components/EmptyState';
import { Panel } from '../components/Panel';
import { QueryErrorState } from '../components/PanelState';
import { Spinner } from '../components/Spinner';
import { refusalHeading } from '../lib/errorDisplay';
import { formatRelativeTime, shortenSha } from '../lib/format';
import { MAX_DRAWN_LINES } from './DiffView';

/**
 * Who last touched each line of a path at a revision.
 *
 * Opens from a file header on a commit's patch. A line's commit is a button
 * into the commit panel; the line number opens that line's history.
 */

export function blameQuery(repositoryId: string, path: string, revision: string) {
  return {
    queryKey: ['blame', repositoryId, path, revision],
    queryFn: () => api.blame(repositoryId, path, revision),
  };
}

export function BlamePanel({
  repositoryId,
  path,
  revision,
  onClose,
  onSelectCommit,
  onLineHistory,
}: {
  repositoryId: string;
  path: string;
  revision: string;
  onClose: () => void;
  onSelectCommit: (sha: string) => void;
  onLineHistory: (line: number) => void;
}) {
  const blame = useQuery(blameQuery(repositoryId, path, revision));

  return (
    <Panel
      title={`Blame — ${path}`}
      className="h-full min-h-0"
      flush
      actions={<CloseButton label="Close the blame" onClose={onClose} />}
    >
      {blame.isPending && (
        <div className="flex h-full items-center justify-center">
          <Spinner label={`Reading blame of ${path}`} />
        </div>
      )}

      {/* The shared failure state, so a blame that could not be read looks
          like every other read that could not be, and so this one gets the way
          out the hand-rolled version had no room for: blame is the query most
          often refused by something transient — an index.lock a terminal is
          holding, a path that has just been rewritten under a rebase — and
          until now the only gesture that asked again was clicking to another
          window and back. The heading stays local because refusalHeading knows
          which refusals have a better sentence than "Could not blame". */}
      {blame.error !== null && (
        <QueryErrorState
          title={refusalHeading(blame.error) ?? `Could not blame ${path}`}
          error={blame.error}
          retry={blame}
        />
      )}

      {blame.data !== undefined && blame.data.lines.length === 0 && (
        <EmptyState title="This file is empty" description="There are no lines to attribute." />
      )}

      {blame.data !== undefined && blame.data.lines.length > 0 && (
        <div className="h-full overflow-auto font-mono text-xs">
          <Capped lines={blame.data.lines.length} />
          <table className="w-full border-collapse">
            <tbody>
              {blame.data.lines.slice(0, MAX_DRAWN_LINES).map((line) => (
                <BlameRow
                  key={line.number}
                  line={line}
                  onSelectCommit={onSelectCommit}
                  onLineHistory={onLineHistory}
                />
              ))}
            </tbody>
          </table>
          <Truncated lines={blame.data.lines.length} />
        </div>
      )}
    </Panel>
  );
}

/**
 * That the attribution is cut, said where the reader begins.
 *
 * The paragraph at the foot of the panel is two thousand rows away — forty
 * thousand pixels on an ordinary row height — so it is read only by somebody
 * who already knows the file continues. The same pair, in the same words, is
 * what the patch pane draws over a capped diff: blame and a patch open in the
 * same slot, and a reader who has learned the sentence in one should not have
 * to learn it again in the other.
 *
 * The numbers are grouped. Four digits of line count run together as `2000`,
 * and the two numbers this sentence compares are read against each other.
 */
function Capped({ lines }: { lines: number }) {
  if (lines <= MAX_DRAWN_LINES) {
    return null;
  }

  return (
    <p className="border-b border-line bg-sunken px-3 py-2 font-sans text-2xs text-ink-muted">
      <span className="text-ink">
        Showing the first {MAX_DRAWN_LINES.toLocaleString('en-GB')} lines
      </span>{' '}
      of {lines.toLocaleString('en-GB')}.
    </p>
  );
}

/** The same fact where the rows stop, for the reader who scrolled to the end. */
function Truncated({ lines }: { lines: number }) {
  if (lines <= MAX_DRAWN_LINES) {
    return null;
  }

  return (
    <p className="border-t border-line px-3 py-2 font-sans text-2xs text-ink-subtle">
      {lines.toLocaleString('en-GB')} lines in this file; the first{' '}
      {MAX_DRAWN_LINES.toLocaleString('en-GB')} are shown.
    </p>
  );
}

/**
 * One line of the file, and who last wrote it.
 *
 * The hover text is a native `title`, and it used to be a Tooltip. This table
 * scrolls inside its panel and Tooltip's bubble stays in the normal flow: it
 * hangs above its own row, so on the rows nearest the top it is painted over
 * the scroller's edge and clipped away — and scrolling cannot bring it back,
 * because those rows are already at the top of the range. Not one bubble in
 * this panel could be read in full, and what that lost is the sentence naming
 * the author, the date and the subject: the date and the subject are on no
 * other part of the row, and the author is in a cell narrow enough to
 * truncate most names. Tooltip's own comment names the trap and sends the
 * caller here: the browser draws a `title` outside the page, where nothing
 * clips it.
 *
 * The line number keeps its accessible name and takes no title. The bubble it
 * used to carry was that name word for word, and a description repeating the
 * name is a sentence read out twice for nothing.
 */
function BlameRow({
  line,
  onSelectCommit,
  onLineHistory,
}: {
  line: BlameLine;
  onSelectCommit: (sha: string) => void;
  onLineHistory: (line: number) => void;
}) {
  const when = formatRelativeTime(new Date(line.date), new Date());
  const label = `${line.author}, ${when} — ${line.subject}`;

  return (
    <tr className="hover:bg-hover">
      <td className="w-0 whitespace-nowrap border-r border-line px-2 py-0.5 text-right align-top">
        <button
          type="button"
          onClick={() => onLineHistory(line.number)}
          aria-label={`History of line ${line.number}`}
          className="rounded-sm text-2xs text-ink-subtle outline-none hover:text-ink focus-visible:focus-ring"
        >
          {line.number}
        </button>
      </td>
      <td className="w-0 whitespace-nowrap border-r border-line px-2 py-0.5 align-top">
        <button
          type="button"
          onClick={() => onSelectCommit(line.sha)}
          title={label}
          className="rounded-sm text-2xs text-ink-muted outline-none hover:text-ink focus-visible:focus-ring"
        >
          {shortenSha(line.sha)}
        </button>
      </td>
      <td className="w-0 max-w-32 truncate border-r border-line px-2 py-0.5 text-2xs text-ink-subtle align-top">
        {line.author}
      </td>
      <td className="whitespace-pre-wrap px-2 py-0.5 text-ink align-top">{line.text}</td>
    </tr>
  );
}
