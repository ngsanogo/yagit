import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';

import { api } from '../api/client';
import type { Ref } from '../api/types';
import { Dialog } from '../components/Dialog';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';
import { errorDescription } from '../lib/errorDisplay';
import { compareSummary } from './compare';

/**
 * Pick a reference and read how far HEAD has drifted from it.
 *
 * A reading and nothing else. Merging and rebasing already have their one
 * place — the branch's menu in the sidebar — and a second pair of buttons here
 * would be the same action on two paths. The answer follows the choice: there
 * is nothing to confirm about a count, so no button stands between picking a
 * reference and seeing it.
 */
export function CompareDialog({
  repositoryId,
  refs,
  headName,
  onClose,
}: {
  repositoryId: string;
  refs: readonly Ref[];
  /** Short name of the branch HEAD is on, when it is on one. */
  headName: string | undefined;
  onClose: () => void;
}) {
  // Branches, remote branches and tags: what names a commit to drift from.
  // The other refs a repository holds — notes, a bisect's marks, a stash —
  // are not offered, since a notes ref names a tree and the count would only
  // ever answer with git's refusal to read it as a commit.
  const candidates = refs.filter(
    (ref) =>
      ref.kind !== 'other' &&
      !(ref.kind === 'branch' && headName !== undefined && ref.short_name === headName),
  );
  const [chosen, setChosen] = useState(candidates[0]?.name ?? '');

  const comparison = useQuery({
    queryKey: ['compare', repositoryId, chosen],
    queryFn: () => api.compare(repositoryId, chosen),
    enabled: chosen !== '',
  });

  const options = candidates.map((ref) => ({
    value: ref.name,
    label:
      ref.kind === 'remote'
        ? `${ref.short_name} (remote)`
        : ref.kind === 'tag'
          ? `${ref.short_name} (tag)`
          : ref.short_name,
  }));

  return (
    <Dialog
      open
      onClose={onClose}
      title="Compare with a reference"
      description="How far the tip you are on has drifted from another reference."
      size="medium"
    >
      <div className="flex flex-col gap-4">
        <Select
          label="Compare against"
          options={options}
          value={chosen}
          disabled={options.length === 0}
          onChange={(event) => setChosen(event.target.value)}
          hint={
            options.length === 0
              ? 'This repository has no other references to compare with.'
              : undefined
          }
        />

        {chosen !== '' && (
          <div
            className="flex min-h-12 items-center rounded-md border border-line bg-sunken p-3"
            aria-live="polite"
          >
            {comparison.isPending ? (
              <Spinner label="Comparing" />
            ) : comparison.isError ? (
              <p className="text-sm text-danger">{errorDescription(comparison.error)}</p>
            ) : (
              <p className="text-sm text-ink">{compareSummary(comparison.data)}</p>
            )}
          </div>
        )}
      </div>
    </Dialog>
  );
}
