import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState, type FormEvent } from 'react';

import { ApiError, api } from '../api/client';
import type { ClonePlan } from '../api/types';
import { Button } from '../components/Button';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { GitFailureDetail } from '../components/GitFailureDetail';
import { Spinner } from '../components/Spinner';
import { errorSummary } from '../lib/errorDisplay';
import { discoverQuery } from './OpenRepository';

/**
 * Suggested leaf name for a clone destination, taken from the URL's last path
 * segment — the same guess `git clone` itself makes when no path is given.
 *
 * Returns empty when nothing useful can be read: a bare host, a trailing
 * slash with no name, a scheme with no path. The field then stays for the
 * user to fill rather than inventing a directory called "git".
 */
export function suggestedCloneName(url: string): string {
  const trimmed = url.trim().replace(/\/+$/, '');
  if (trimmed === '') {
    return '';
  }

  // The scheme first, so what is left starts at the host. `https://example.com`
  // and `example.com` then look the same, which is what they are: a host and no
  // path.
  const afterScheme = trimmed.replace(/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//, '');

  // What follows the host: after the first `/` for a URL or a local path, after
  // the `:` for the scp-like `git@host:owner/repo.git`. A bare host has
  // neither, and there is no name in it to suggest — a destination called
  // `example.com` is not what "clone this" meant.
  const slash = afterScheme.indexOf('/');
  const colon = afterScheme === trimmed ? afterScheme.indexOf(':') : -1;
  const cut = slash >= 0 ? slash : colon;
  if (cut < 0) {
    return '';
  }

  let leaf =
    afterScheme
      .slice(cut + 1)
      .split('/')
      .pop() ?? '';
  if (leaf.endsWith('.git')) {
    leaf = leaf.slice(0, -'.git'.length);
  }
  return leaf;
}

/**
 * Clones a repository into YAGIT_ROOT and opens it.
 *
 * Plan then confirm, like publish and force-push: the confirmation's content
 * is the exact command. Drawn inline rather than in a nested dialog, because
 * this panel already lives inside the "Add a repository" dialog and a second
 * `<dialog>` on top of one is not a thing the platform handles cleanly.
 *
 * Progress lines from git's stderr ride the request (ADR 0030) and are drawn
 * while the confirm stays busy.
 */
export function CloneRepository({ onOpened }: { onOpened?: (id: string) => void }) {
  const [url, setUrl] = useState('');
  const [path, setPath] = useState('');
  const [pathTouched, setPathTouched] = useState(false);
  const [plan, setPlan] = useState<ClonePlan>();
  const [progress, setProgress] = useState<string[]>([]);
  const queryClient = useQueryClient();

  // The root the daemon last reported on a discover — the only place the
  // interface learns it. Used to suggest a destination under that root when
  // the URL names a repository and nobody has typed a path yet.
  const discover = useQuery(discoverQuery({ includeWorktrees: false, includeSubmodules: false }));
  const root = discover.data?.root;

  const run = useMutation({
    mutationFn: () =>
      api.clone(url.trim(), path.trim(), (line) => {
        setProgress((current) => {
          const next = [...current, line];
          // Keep a short tail: a long clone can write thousands of counter
          // updates, and the panel only needs the recent ones to feel alive.
          return next.length > 40 ? next.slice(-40) : next;
        });
      }),
    onSuccess: async (repository) => {
      setPlan(undefined);
      setUrl('');
      setPath('');
      setPathTouched(false);
      setProgress([]);
      await queryClient.invalidateQueries({ queryKey: ['repositories'] });
      await queryClient.invalidateQueries({ queryKey: ['discover'] });
      onOpened?.(repository.id);
    },
  });

  const askPlan = useMutation({
    mutationFn: () => api.clonePlan(url.trim(), path.trim()),
    onSuccess: (next) => {
      setPlan(next);
      setProgress([]);
      run.reset();
    },
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (url.trim() === '' || path.trim() === '') {
      return;
    }
    askPlan.mutate();
  };

  const onUrlChange = (value: string) => {
    setUrl(value);
    if (pathTouched) {
      return;
    }
    const leaf = suggestedCloneName(value);
    if (leaf === '' || root === undefined) {
      return;
    }
    setPath(`${root.replace(/\/+$/, '')}/${leaf}`);
  };

  const planFailure = askPlan.error;
  const runFailure = run.error;

  if (plan !== undefined) {
    return (
      <div className="flex w-full max-w-xl flex-col gap-4">
        <div className="flex flex-col gap-1">
          <h2 className="text-sm font-semibold text-ink">Clone this repository</h2>
          <p className="text-2xs text-ink-subtle">
            The destination will be created and filled from the remote. Credentials, if any, stay
            with git.
          </p>
        </div>

        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-ink-muted">yagit will run</p>
          <GitCommand command={plan.command} />
        </div>

        {progress.length > 0 && (
          <pre
            className="max-h-40 overflow-auto rounded-sm bg-sunken px-3 py-2 font-mono text-2xs break-words whitespace-pre-wrap text-ink-muted"
            aria-live="polite"
          >
            {progress.join('\n')}
          </pre>
        )}

        {runFailure !== null && <CloneFailure error={runFailure} />}

        <div className="flex items-center justify-end gap-2">
          <Button
            variant="ghost"
            disabled={run.isPending}
            onClick={() => {
              setPlan(undefined);
              setProgress([]);
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
              run.mutate();
            }}
          >
            Clone
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex w-full max-w-xl flex-col gap-4">
      <section className="flex flex-col gap-3" aria-labelledby="clone-heading">
        <div className="flex flex-col gap-1">
          <h2 id="clone-heading" className="text-sm font-semibold text-ink">
            Clone a repository
          </h2>
          <p className="text-2xs text-ink-subtle">
            Copy a remote onto disk under the allowed root, then open it. Credentials stay with git
            — yagit never asks for a password.
          </p>
        </div>

        <form className="flex flex-col gap-3" onSubmit={submit}>
          <Field
            label="URL"
            hint="https, ssh, or a local path git can read."
            value={url}
            onChange={(event) => onUrlChange(event.target.value)}
            placeholder="https://example.com/owner/repo.git"
            autoComplete="off"
            spellCheck={false}
          />
          <Field
            label="Destination"
            hint="Absolute path inside the allowed root. The parent must exist; the folder itself must not."
            value={path}
            onChange={(event) => {
              setPathTouched(true);
              setPath(event.target.value);
            }}
            placeholder={root !== undefined ? `${root}/repo` : '/home/you/repo'}
            autoComplete="off"
            spellCheck={false}
          />
          <div className="flex items-center gap-2">
            <Button
              type="submit"
              variant="primary"
              loading={askPlan.isPending}
              disabled={url.trim() === '' || path.trim() === ''}
            >
              Clone…
            </Button>
            {askPlan.isPending ? <Spinner label="Asking what would run" /> : null}
          </div>
        </form>

        {planFailure !== null && <CloneFailure error={planFailure} />}
      </section>
    </div>
  );
}

/**
 * What went wrong, said once.
 *
 * The daemon sends two accounts of a failed git command: a message, and the
 * command, exit code and stderr beside it. The message usually ENDS with
 * those same three facts rendered as a sentence, so drawing both put the
 * whole of git's stderr on the screen twice — once as prose, once in the
 * block underneath it. `errorSummary` keeps only the half the block cannot
 * say, which is why the paragraph is conditional: when the message was the
 * restatement and nothing else there is nothing left to draw, and an empty
 * line above the block is not an improvement on a duplicated one.
 *
 * The `role="alert"` stays on the wrapper rather than moving to whichever of
 * the two is drawn. Either half can be the only one — a refusal from the
 * daemon has no git failure under it, and a git failure whose message adds
 * nothing has no paragraph over it — and the thing that has to be announced
 * is the failure, not the half that happened to survive.
 */
function CloneFailure({ error }: { error: Error }) {
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
