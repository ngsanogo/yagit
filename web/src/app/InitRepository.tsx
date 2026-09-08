import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { ApiError, api } from '../api/client';
import type { InitPlan } from '../api/types';
import { Button } from '../components/Button';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { GitFailureDetail } from '../components/GitFailureDetail';
import { Spinner } from '../components/Spinner';
import { errorSummary } from '../lib/errorDisplay';
import { leafOf } from '../lib/path';
import { discoverQuery } from './OpenRepository';

/**
 * Makes an empty repository under YAGIT_ROOT and opens it.
 *
 * Plan then confirm, the same shape as the clone beside it and for the same
 * reason: the confirmation's content is the exact command. What differs is one
 * field. `git init` reads the first branch's name from `init.defaultBranch`,
 * which is a setting on the daemon's machine — so the plan comes back with the
 * name filled in and the command pins it (ADR 0021), rather than a button
 * making `main` on one machine and `master` on the next.
 */
export function InitRepository({ onOpened }: { onOpened?: (id: string) => void }) {
  const [path, setPath] = useState('');
  const [branch, setBranch] = useState('');
  const [plan, setPlan] = useState<InitPlan>();
  const queryClient = useQueryClient();

  // The root the daemon last reported on a discover — the only place the
  // interface learns it, and what the placeholder below is built from.
  const discover = useQuery(discoverQuery({ includeWorktrees: false, includeSubmodules: false }));
  const root = discover.data?.root;

  const run = useMutation({
    mutationFn: (approved: InitPlan) => api.init(approved.path, approved.branch),
    onSuccess: async (repository) => {
      setPlan(undefined);
      setPath('');
      setBranch('');
      await queryClient.invalidateQueries({ queryKey: ['repositories'] });
      await queryClient.invalidateQueries({ queryKey: ['discover'] });
      onOpened?.(repository.id);
    },
  });

  const askPlan = useMutation({
    mutationFn: () => api.initPlan(path.trim(), branch.trim()),
    onSuccess: (next) => {
      setPlan(next);
      // The daemon settled the branch name; the field shows what was settled,
      // so going Back and forward again asks about the same thing.
      setBranch(next.branch);
      run.reset();
    },
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (path.trim() === '') {
      return;
    }
    askPlan.mutate();
  };

  const planFailure = askPlan.error;
  const runFailure = run.error;

  if (plan !== undefined) {
    return (
      <div className="flex w-full max-w-xl flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h2 className="text-sm font-semibold text-ink">Make this repository</h2>
          <p className="text-2xs text-ink-subtle">
            {leafOf(plan.path)} will be created with no commits, on a branch called {plan.branch}.
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-ink-muted">yagit will run</p>
          <GitCommand command={plan.command} />
        </div>

        {runFailure !== null && <InitFailure error={runFailure} />}

        <div className="flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            disabled={run.isPending}
            onClick={() => {
              setPlan(undefined);
              run.reset();
            }}
          >
            Back
          </Button>
          <Button
            variant="primary"
            loading={run.isPending}
            disabled={run.isPending}
            onClick={() => {
              run.mutate(plan);
            }}
          >
            Create
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex w-full max-w-xl flex-col gap-4">
      <section className="flex flex-col gap-3" aria-labelledby="init-heading">
        <div className="flex flex-col gap-1">
          <h2 id="init-heading" className="text-sm font-semibold text-ink">
            Make a repository
          </h2>
          <p className="text-2xs text-ink-subtle">
            An empty repository under the allowed root, opened here once it exists. Nothing is
            committed and no remote is added.
          </p>
        </div>

        <form className="flex flex-col gap-3" onSubmit={submit}>
          <Field
            label="Folder"
            hint="Absolute path inside the allowed root. The parent must exist; the folder itself must not."
            value={path}
            onChange={(event) => setPath(event.target.value)}
            placeholder={root !== undefined ? `${root}/repo` : '/home/you/repo'}
            autoComplete="off"
            spellCheck={false}
          />
          <Field
            label="First branch"
            hint="Left empty, the daemon uses this machine's init.defaultBranch."
            value={branch}
            onChange={(event) => setBranch(event.target.value)}
            placeholder="main"
            autoComplete="off"
            spellCheck={false}
          />
          <div className="flex items-center gap-2">
            <Button
              type="submit"
              variant="primary"
              loading={askPlan.isPending}
              disabled={path.trim() === ''}
            >
              Create…
            </Button>
            {askPlan.isPending ? <Spinner label="Asking what would run" /> : null}
          </div>
        </form>

        {planFailure !== null && <InitFailure error={planFailure} />}
      </section>
    </div>
  );
}

/**
 * What went wrong, said once.
 *
 * The twin of CloneRepository's — same doubling, same subtraction. See the
 * note there for why the paragraph disappears rather than emptying, and
 * `errorSummary` in lib/errorDisplay for what it takes away.
 */
function InitFailure({ error }: { error: Error }) {
  const summary = errorSummary(error);

  return (
    <div role="alert" className="flex flex-col gap-1 text-xs text-danger">
      {summary !== undefined && <span>{summary}</span>}
      {error instanceof ApiError && error.git !== undefined ? (
        <GitFailureDetail failure={error.git} />
      ) : null}
    </div>
  );
}
