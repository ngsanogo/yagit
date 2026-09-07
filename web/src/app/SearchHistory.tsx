import { useQuery } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { api } from '../api/client';
import type { HistoryScope, SearchField } from '../api/types';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { SegmentedControl, type Segment } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
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

  return (
    <div className="flex min-h-0 flex-col gap-3">
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

      {results.isFetching && (
        <div className="flex justify-center py-6">
          <Spinner label="Searching the history" />
        </div>
      )}

      {results.error !== null && asked !== undefined && (
        <EmptyState
          title="Could not search the history"
          description=""
          detail={errorDescription(results.error)}
          className="py-6"
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
                    className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left hover:bg-sunken focus-visible:focus-ring"
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
