import { useMutation } from '@tanstack/react-query';
import { useState } from 'react';

import { api } from '../api/client';
import type { CompareResult, Ref } from '../api/types';
import { Button } from '../components/Button';
import { Dialog } from '../components/Dialog';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';
import { errorDescription } from '../lib/errorDisplay';
import { compareSummary } from './compare';

/**
 * Pick a reference, read how far HEAD has drifted from it, and offer merge or
 * rebase when the other side is a local branch.
 *
 * The counts are the point of this dialog; the buttons hand off to the same
 * plan routes the sidebar already uses, so the confirmation that follows is
 * the same sentence either way.
 */
export function CompareDialog({
  repositoryId,
  refs,
  headName,
  canAct,
  actRefusal,
  busyMerge,
  busyRebase,
  onClose,
  onMerge,
  onRebase,
}: {
  repositoryId: string;
  refs: readonly Ref[];
  /** Short name of the branch HEAD is on, when it is on one. */
  headName: string | undefined;
  canAct: boolean;
  actRefusal: string | undefined;
  busyMerge: boolean;
  busyRebase: boolean;
  onClose: () => void;
  onMerge: (branch: string) => void;
  onRebase: (onto: string) => void;
}) {
  const candidates = refs.filter(
    (ref) => !(ref.kind === 'branch' && headName !== undefined && ref.short_name === headName),
  );
  const [chosen, setChosen] = useState(candidates[0]?.name ?? '');
  const [result, setResult] = useState<CompareResult>();

  const compare = useMutation({
    mutationFn: (ref: string) => api.compare(repositoryId, ref),
    onSuccess: (answer) => setResult(answer),
  });

  const selected = refs.find((ref) => ref.name === chosen);
  const localBranch = selected?.kind === 'branch' ? selected.short_name : undefined;

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
      description="See how far the tip you are on has drifted from another reference."
      size="medium"
    >
      <div className="flex flex-col gap-4">
        <Select
          label="Compare against"
          options={options}
          value={chosen}
          disabled={options.length === 0}
          onChange={(event) => {
            setChosen(event.target.value);
            setResult(undefined);
            compare.reset();
          }}
          hint={
            options.length === 0
              ? 'This repository has no other references to compare with.'
              : undefined
          }
        />

        <div className="flex items-center gap-2">
          <Button
            variant="primary"
            disabled={chosen === '' || compare.isPending}
            loading={compare.isPending}
            onClick={() => compare.mutate(chosen)}
          >
            Compare
          </Button>
          {compare.isPending && <Spinner label="Comparing" />}
        </div>

        {compare.isError && (
          <p className="text-sm text-danger">{errorDescription(compare.error)}</p>
        )}

        {result !== undefined && (
          <div className="flex flex-col gap-3 rounded-md border border-line bg-sunken p-3">
            <p className="text-sm text-ink">{compareSummary(result)}</p>
            {localBranch !== undefined && canAct && (
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={busyMerge || busyRebase}
                  onClick={() => onMerge(localBranch)}
                >
                  Merge {localBranch}…
                </Button>
                <Button
                  size="sm"
                  disabled={busyMerge || busyRebase}
                  onClick={() => onRebase(localBranch)}
                >
                  Rebase onto {localBranch}…
                </Button>
              </div>
            )}
            {localBranch !== undefined && !canAct && actRefusal !== undefined && (
              <p className="text-sm text-ink-muted">{actRefusal}</p>
            )}
            {localBranch === undefined && (
              <p className="text-sm text-ink-muted">
                Merge and rebase need a local branch. This reference is not one.
              </p>
            )}
          </div>
        )}
      </div>
    </Dialog>
  );
}
