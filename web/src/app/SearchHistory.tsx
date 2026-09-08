import { useQuery } from '@tanstack/react-query';
import { useEffect, useRef, useState, type FormEvent } from 'react';

import { api } from '../api/client';
import type { HistoryScope, SearchField } from '../api/types';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { QueryErrorState } from '../components/PanelState';
import { SegmentedControl, type Segment } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { useToast } from '../components/ToastHost';
import { counted, shortenSha } from '../lib/format';
import { refsKey } from './historyScope';

/**
 * Searching the history: a query, one of four places to look, and a list of
 * commits to follow.
 *
 * A list and not a filtered graph. The graph draws how commits connect
 * (ADR 0012), and drawing it over the matches alone would put two commits side
 * by side with three others between them — a picture of a history nobody has.
 * Following a result takes the graph to that commit, which is the movement
 * clicking a branch in the sidebar already makes.
 *
 * The query is a literal string in all four fields. `git log --grep` takes a
 * POSIX pattern, so a box that passed it through would find nothing for
 * `fix(api)` and refuse `a(b` with a message about parentheses.
 */

const FIELDS: readonly Segment<SearchField>[] = [
  { value: 'message', label: 'Message' },
  { value: 'author', label: 'Author' },
  { value: 'path', label: 'File' },
  { value: 'content', label: 'Content' },
];

/** What each field looks in, said where somebody is about to type. */
const HINTS: Record<SearchField, string> = {
  message: 'Any part of a commit message. Case-insensitive.',
  author: 'Part of an author’s name or email address. Case-insensitive.',
  path: 'A path in the repository. A directory matches everything under it.',
  content: 'Text a commit added or removed. Exact, and case-sensitive.',
};

export function SearchHistory({
  repositoryId,
  scope,
  refs = [],
  onChoose,
}: {
  repositoryId: string;
  scope: HistoryScope;
  /** The chosen references, under `scope=refs` and empty otherwise. */
  refs?: readonly string[];
  onChoose: (sha: string) => void;
}) {
  const toast = useToast();
  const [field, setField] = useState<SearchField>('message');
  const [draft, setDraft] = useState('');

  // What was actually asked for, which is not what is in the box: a search
  // runs `git log` over the whole history, and running one per keystroke would
  // spawn a process for every letter of a word nobody has finished typing.
  const [asked, setAsked] = useState<{ query: string; field: SearchField }>();

  const results = useQuery({
    // The chosen set is in the key with the scope, because "in this history"
    // is most of what a search means: the same words over another set of refs
    // is another question, and answering it from this one's results would find
    // commits the graph beside it cannot scroll to.
    queryKey: [
      'search',
      repositoryId,
      scope,
      refsKey(scope, refs),
      asked?.field,
      asked?.query,
    ] as const,
    queryFn: () =>
      api.search(repositoryId, asked?.query ?? '', asked?.field ?? 'message', scope, refs),
    enabled: asked !== undefined,
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (draft.trim() === '') {
      return;
    }
    setAsked({ query: draft.trim(), field });
  };

  const { data, error, isFetching } = results;

  /*
   * The press this pane has already answered.
   *
   * Not a guard against saying the same thing twice — every press makes a new
   * `asked`, so pressing Search twice is announced twice, and somebody who
   * cannot see the list pressing again is somebody checking they pressed it
   * at all. What it holds back is the settle nobody asked for. Every query in
   * this application refetches when the window regains focus, because the
   * repository is also being worked in a terminal and that is the only thing
   * covering what happened while the tab was in the background; a search left
   * open across that round trip would read its count out again the moment the
   * user came back to the browser. An announcement that arrives without a
   * press is how a live region teaches somebody to switch it off.
   */
  const announced = useRef<typeof asked>(undefined);

  /*
   * What the search did, in words.
   *
   * Pressing Search and then hearing nothing is the whole complaint: focus
   * stays on the button, the spinner that replaces the list is decoration
   * (its role="status" wraps an aria-hidden glyph and so has nothing to read
   * out), and a query still running is indistinguishable from one that
   * matched nothing. The count is the answer to both questions at once.
   *
   * Said through the host's region rather than a `role="status"` of this
   * pane's own, and that is not tidiness: a live region only reliably
   * announces a change made after it is in the document, and every region
   * this pane could mount arrives at the same moment as the text inside it.
   * The host's has been there since the page loaded.
   *
   * Settled rather than successful, so a query answered from the cache is
   * still reported: an answer that arrives instantly is the one most likely
   * to be mistaken for no answer at all.
   */
  useEffect(() => {
    if (asked === undefined || isFetching || announced.current === asked) {
      return;
    }
    if (error !== null) {
      announced.current = asked;
      toast.announce('Could not search the history');
      return;
    }
    if (data === undefined) {
      // Enabled and not yet in flight: the key changed on this render and the
      // fetch has not been marked. Nothing has been answered, so nothing is
      // remembered either and the next settle is still this press's first.
      return;
    }
    announced.current = asked;
    if (data.commits.length === 0) {
      toast.announce('Nothing matched');
      return;
    }
    const matched = counted(data.commits.length, 'commit', 'matches', 'match');
    toast.announce(data.truncated ? `More than ${matched}` : matched);
  }, [asked, data, error, isFetching, toast]);

  return (
    <div
      className="flex min-h-0 flex-col gap-3"
      // The pane says it is working as well as saying what it found. A search
      // walks the whole history, so the wait is long enough to be mistaken
      // for a result — and a reader who cannot see the spinner has, until the
      // announcement lands, no way to tell "still going" from "nothing here".
      aria-busy={results.isFetching || undefined}
    >
      <SegmentedControl
        label="Where to look"
        segments={FIELDS}
        value={field}
        onChange={(next) => {
          setField(next);
          // The question changed; the results below answered the previous one.
          setAsked(undefined);
        }}
      />

      <form className="flex items-end gap-2" onSubmit={submit}>
        <div className="min-w-0 flex-1">
          <Field
            label="Search"
            hint={HINTS[field]}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </div>
        <Button type="submit" variant="primary" disabled={draft.trim() === ''}>
          Search
        </Button>
      </form>

      {/* Not while a failure is on screen. The wait is carried there instead,
          by the Retry button's own spinner: a second attempt that unmounted
          the block it was pressed in would drop keyboard focus to the body,
          and drawing both would put two spinners on one small pane saying the
          same thing. */}
      {results.isFetching && results.error === null && (
        <div className="flex justify-center py-6">
          <Spinner label="Searching the history" />
        </div>
      )}

      {/* The shared failure state, so a search that could not run looks like
          every other read that could not — and so it offers the way out, which
          matters more here than in a panel: the pane is inside a dialog, and
          the alternative recovery this application has (leaving the window and
          coming back) is a gesture that dismisses the dialog on the way.
          `compact` because the padding is a variant rather than a class from
          outside; the `py-6` this used to pass never reached the element. */}
      {results.error !== null && asked !== undefined && (
        <QueryErrorState
          title="Could not search the history"
          error={results.error}
          compact
          retry={results}
        />
      )}

      {!results.isFetching && results.data !== undefined && (
        <>
          {results.data.commits.length === 0 ? (
            <EmptyState
              title="Nothing matched"
              description="No commit in this walk matches. The command that asked is below."
            />
          ) : (
            <ul className="min-h-0 flex-1 overflow-auto rounded-sm border border-line">
              {results.data.commits.map((commit) => (
                <li key={commit.sha} className="border-b border-line last:border-b-0">
                  <button
                    type="button"
                    // `--color-hover` and not `--color-sunken`: a result row
                    // darkened under the pointer where every other list in the
                    // workbench lightens, and the token that darkens is the
                    // one fields and input areas are painted with.
                    className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left transition-colors transition-instant hover:bg-hover focus-visible:focus-ring"
                    onClick={() => onChoose(commit.sha)}
                  >
                    <span className="w-full truncate text-xs text-ink">{commit.subject}</span>
                    <span className="text-2xs text-ink-subtle">
                      <span className="font-mono">{shortenSha(commit.sha)}</span> · {commit.author}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}

          {results.data.truncated && (
            <p className="text-2xs text-ink-subtle">
              More than {results.data.commits.length} commits match. Narrow the query to see the
              rest.
            </p>
          )}

          <div className="flex flex-col gap-1.5">
            <p className="text-xs font-medium text-ink-muted">yagit ran</p>
            <GitCommand command={results.data.command} />
          </div>
        </>
      )}
    </div>
  );
}
