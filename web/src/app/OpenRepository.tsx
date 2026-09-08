import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useId, useMemo, useState, type FormEvent } from 'react';

import { ApiError, api } from '../api/client';
import type { DiscoveredKind, DiscoveredRepository } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { Field } from '../components/Field';
import { GitFailureDetail } from '../components/GitFailureDetail';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { errorSummary } from '../lib/errorDisplay';
import { boundedScanDepth, scanDepthFromInput, scanSkipExplanation } from './discover';

/**
 * One scan of the disk, keyed by what was asked for.
 *
 * A factory rather than a key written out at each call site: the clone panel
 * needs the same scan only to learn the root it reports, and a second
 * hand-written key would be a second walk of a home directory — the most
 * expensive thing the daemon does — for an answer already in the cache.
 *
 * The signal is the query's own, and passing it on is what makes a superseded
 * scan stop: four presses of the spinner arrow are four walks, and three of
 * them are answers nobody is waiting for any more. The daemon drops its walk
 * when the connection closes.
 */
export function discoverQuery(scan: {
  dir?: string;
  depth?: number;
  includeWorktrees: boolean;
  includeSubmodules: boolean;
}) {
  return {
    queryKey: [
      'discover',
      scan.dir ?? '',
      scan.includeWorktrees,
      scan.includeSubmodules,
      scan.depth ?? 0,
    ] as const,
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      api.discoverRepositories({
        ...(scan.dir !== undefined ? { dir: scan.dir } : {}),
        ...(scan.depth !== undefined ? { depth: scan.depth } : {}),
        includeWorktrees: scan.includeWorktrees,
        includeSubmodules: scan.includeSubmodules,
        signal,
      }),
  };
}

/**
 * A path on its way to the daemon, and the way in that asked for it.
 *
 * One action, one mutation — so the request carries its origin rather than
 * having it worked out again afterwards by matching the failed path against
 * the scan list. That matching is wrong wherever the two overlap: a path typed
 * into the field that also appears in the list would light up a row nobody
 * clicked, and leave the box that was typed in silent.
 */
interface OpenRequest {
  path: string;
  from: 'list' | 'form';
}

/** Where a failed open explains itself. */
export type FailurePlacement =
  { on: 'nothing' } | { on: 'row'; path: string; failure: Error } | { on: 'form'; failure: Error };

/**
 * Sends a failure to the control that asked for it.
 *
 * Exactly one place, always the one that was used. An error shown beside a
 * control the user never touched is the failure this screen exists to avoid:
 * it is visible, it is in the wrong place, and the next move is a guess.
 */
export function failurePlacement(
  failure: Error | null,
  request: OpenRequest | undefined,
): FailurePlacement {
  // `variables` outlives the request that set them, so the error is what says
  // whether there is anything to place at all.
  if (failure === null || request === undefined) {
    return { on: 'nothing' };
  }

  return request.from === 'list'
    ? { on: 'row', path: request.path, failure }
    : { on: 'form', failure };
}

/** The failure a scan row draws: its own, or none. */
export function failureOnRow(placement: FailurePlacement, path: string): Error | null {
  return placement.on === 'row' && placement.path === path ? placement.failure : null;
}

/** The failure the path field draws: its own, or none. */
export function failureOnForm(placement: FailurePlacement): Error | null {
  return placement.on === 'form' ? placement.failure : null;
}

/**
 * Whether the open in flight is the one this row asked for.
 *
 * The way in decides the spinner as well as the error. Keyed on the path
 * alone, a path typed into the field that also appears in the list greys out
 * and spins a row nobody clicked, and then re-enables it silently while the
 * explanation lands under the field — one action reported by two controls,
 * which is the fault above with the halves swapped.
 */
export function rowIsOpening(
  pending: boolean,
  request: OpenRequest | undefined,
  path: string,
): boolean {
  return pending && request?.from === 'list' && request.path === path;
}

/**
 * The one place a path reaches the daemon.
 *
 * Everywhere else the client handles opaque identifiers — that is the security
 * boundary. Two ways in, and neither of them weakens it: a list the daemon
 * built by walking inside YAGIT_ROOT, and a path typed by hand, which the
 * daemon checks against that same root before it touches the disk. The page
 * still cannot name a file the daemon did not tell it about, or accept one it
 * would have refused.
 */
export function OpenRepository({ onOpened }: { onOpened?: (id: string) => void }) {
  const [path, setPath] = useState('');
  const [filter, setFilter] = useState('');
  const [includeWorktrees, setIncludeWorktrees] = useState(false);
  const [includeSubmodules, setIncludeSubmodules] = useState(false);
  const queryClient = useQueryClient();

  // The scan starts wherever the daemon's own root is, and the field is only
  // state once somebody has typed in it. undefined means "untouched", and the
  // request then omits `dir` entirely so the daemon picks — it reports which
  // directory that was, and the field shows it. `''` is a box somebody emptied
  // on purpose, which is not the same thing and must not be refilled.
  //
  // Copying a default in from an effect instead cost two renders and got the
  // empty case wrong: clearing the box put the root straight back, so the one
  // directory a person could not scan was the one they had just deleted the
  // name of.
  const [typedScanDir, setTypedScanDir] = useState<string>();

  // The depth is the same arrangement, for the same reason: the daemon owns
  // the default and the ceiling and reports both with every scan, so the
  // control draws what the scan actually used instead of a copy of a number
  // that lives in internal/repo.
  const [typedDepth, setTypedDepth] = useState<number | ''>();

  // The ceiling the daemon last reported, kept here rather than read off the
  // scan in flight. Typing in the depth box changes the query key, so
  // discover.data is undefined for as long as the scan that keystroke started
  // is running — which is precisely when there is a typed number to hold
  // inside a ceiling, and precisely when reading one out of the query finds
  // none.
  const [depthLimit, setDepthLimit] = useState<number>();

  // '' is a box somebody emptied, and it asks for no depth at all, so the
  // daemon picks again. Only a number is sent, and never one above the
  // ceiling: the daemon caps silently, and a field disagreeing with the scan
  // beside it is the interface lying about what it has just done.
  const boundedDepth =
    typeof typedDepth === 'number' ? boundedScanDepth(typedDepth, depthLimit) : typedDepth;
  const requestedDepth = typeof boundedDepth === 'number' ? boundedDepth : undefined;

  const discover = useQuery(
    discoverQuery({
      ...(typedScanDir !== undefined ? { dir: typedScanDir } : {}),
      ...(requestedDepth !== undefined ? { depth: requestedDepth } : {}),
      includeWorktrees,
      includeSubmodules,
    }),
  );

  // Adjusted during the render that first sees it, rather than in an effect
  // that would paint the old ceiling once before correcting it.
  if (discover.data !== undefined && discover.data.depth_limit !== depthLimit) {
    setDepthLimit(discover.data.depth_limit);
  }

  const scanDir = typedScanDir ?? discover.data?.scanned_from ?? '';
  const scanDepth = boundedDepth ?? discover.data?.depth ?? '';
  const skipExplanation =
    discover.data === undefined ? undefined : scanSkipExplanation(discover.data);
  const submoduleFailures = discover.data?.submodule_failures ?? [];

  const openRepositories = useQuery({
    queryKey: ['repositories'],
    queryFn: api.listRepositories,
  });

  const open = useMutation({
    mutationFn: (request: OpenRequest) => api.openRepository(request.path),
    onSuccess: async (repository) => {
      setPath('');
      await queryClient.invalidateQueries({ queryKey: ['repositories'] });
      onOpened?.(repository.id);
    },
  });

  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (path.trim() !== '') {
      open.mutate({ path: path.trim(), from: 'form' });
    }
  };

  const openPaths = new Set((openRepositories.data ?? []).map((repository) => repository.path));
  const placement = failurePlacement(open.error, open.variables);
  const formFailure = failureOnForm(placement);

  const visible = useMemo(() => {
    const repos = discover.data?.repos ?? [];
    const needle = filter.trim().toLowerCase();
    if (needle === '') {
      return repos;
    }
    return repos.filter(
      (repository) =>
        repository.name.toLowerCase().includes(needle) ||
        repository.path.toLowerCase().includes(needle),
    );
  }, [discover.data?.repos, filter]);

  return (
    <div className="flex w-full max-w-xl flex-col gap-4">
      <section className="flex flex-col gap-3" aria-labelledby="discover-heading">
        <div className="flex flex-col gap-1">
          <h2 id="discover-heading" className="text-sm font-semibold text-ink">
            Repositories on disk
          </h2>
          <p className="text-2xs text-ink-subtle">
            Git repositories the daemon finds under the allowed root. Pick one, or type a path
            below.
          </p>
        </div>

        <div className="flex flex-wrap items-end gap-2">
          <Field
            label="Scan in"
            // "The allowed root", not "the daemon root", which is what this
            // said. The daemon refuses a path outside it with "path outside
            // the allowed root", so that is the name a person meets at the
            // moment they are most confused — and a hint that used a second
            // name for the same limit invited the reading that there are two
            // of them. Two limits is the doubt that ends with somebody
            // widening YAGIT_ROOT to be safe, and this is the application's
            // one security boundary.
            hint="Only directories inside the allowed root are searched."
            className="min-w-56 flex-1"
            value={scanDir}
            onChange={(event) => setTypedScanDir(event.target.value)}
            placeholder="/home/you"
          />
          <Field
            label={depthLimit === undefined ? 'Depth' : `Depth (1–${depthLimit})`}
            hint="Levels below."
            className="w-24"
            type="number"
            min={1}
            {...(depthLimit !== undefined ? { max: depthLimit } : {})}
            value={scanDepth}
            onChange={(event) => setTypedDepth(scanDepthFromInput(event.target.value))}
          />
          <Button
            type="button"
            variant="secondary"
            loading={discover.isFetching}
            onClick={() => {
              void discover.refetch();
            }}
          >
            Scan again
          </Button>
        </div>

        <div className="flex flex-wrap gap-4 text-xs text-ink-muted">
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              className="accent-accent"
              checked={includeWorktrees}
              onChange={(event) => setIncludeWorktrees(event.target.checked)}
            />
            Include linked worktrees
          </label>
          <label className="inline-flex cursor-pointer items-center gap-2">
            <input
              type="checkbox"
              className="accent-accent"
              checked={includeSubmodules}
              onChange={(event) => setIncludeSubmodules(event.target.checked)}
            />
            Include submodules
          </label>
        </div>

        {discover.isPending && (
          <div className="flex justify-center py-6">
            <Spinner label="Scanning for repositories" />
          </div>
        )}

        {discover.isError && <p className="text-2xs text-danger">{discover.error.message}</p>}

        {submoduleFailures.length > 0 && (
          <div className="flex flex-col gap-3">
            <p className="text-2xs text-danger">
              These repositories are listed without their submodules, because git refused to list
              them.
            </p>
            {submoduleFailures.map((failure) => (
              <div key={failure.path} className="flex flex-col gap-1">
                <p className="truncate font-mono text-2xs text-ink-subtle">{failure.path}</p>
                {failure.git === undefined ? (
                  <p className="text-2xs text-danger">{failure.message}</p>
                ) : (
                  <GitFailureDetail failure={failure.git} />
                )}
              </div>
            ))}
          </div>
        )}

        {discover.isSuccess && discover.data.repos.length === 0 && (
          <p className="text-2xs text-ink-subtle">
            No repositories found under {discover.data.scanned_from}.
            {skipExplanation !== undefined && ` ${skipExplanation}`}
          </p>
        )}

        {discover.isSuccess && discover.data.repos.length > 0 && (
          <div className="flex flex-col gap-2">
            <Field
              label="Filter"
              hint={`${visible.length} of ${discover.data.repos.length} shown`}
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder="Name or path"
            />
            <ul
              className="flex max-h-60 flex-col gap-0.5 overflow-auto rounded-md border border-line bg-sunken p-1"
              aria-label="Discovered repositories"
            >
              {visible.map((repository) => (
                <DiscoveredRow
                  key={repository.path}
                  repository={repository}
                  alreadyOpen={openPaths.has(repository.path)}
                  opening={rowIsOpening(open.isPending, open.variables, repository.path)}
                  error={failureOnRow(placement, repository.path)}
                  onOpen={() => open.mutate({ path: repository.path, from: 'list' })}
                />
              ))}
              {visible.length === 0 && (
                <li className="px-2 py-3 text-center text-2xs text-ink-subtle">
                  Nothing matches &ldquo;{filter.trim()}&rdquo;.
                </li>
              )}
            </ul>
          </div>
        )}
      </section>

      <form onSubmit={submit} className="flex flex-col gap-3 border-t border-line pt-4">
        <div className="flex flex-col gap-1">
          <h2 className="text-sm font-semibold text-ink">Open by path</h2>
          <p className="text-2xs text-ink-subtle">
            For a repository the scan did not find — a worktree elsewhere, a path outside the scan
            folder, or a bare clone in an unusual layout.
          </p>
        </div>

        <div className="flex items-end gap-2">
          <Field
            label="Repository path"
            className="flex-1"
            value={path}
            onChange={(event) => setPath(event.target.value)}
            placeholder="/home/you/project"
            // The summary, not the whole message: the daemon's sentence
            // usually ENDS with the command, exit code and stderr that the
            // block below the field already draws, so the field repeated the
            // whole of git's output in red type above it. The fallback is not
            // a nicety — `error` is what marks the field invalid, and a
            // refusal whose message is the restatement and nothing else must
            // still leave a field saying something is wrong with it.
            {...(formFailure !== null
              ? { error: errorSummary(formFailure) ?? formFailure.message }
              : {})}
          />
          <Button type="submit" variant="primary" loading={open.isPending}>
            Open
          </Button>
        </div>

        {formFailure instanceof ApiError && formFailure.git !== undefined && (
          <GitFailureDetail failure={formFailure.git} />
        )}
      </form>
    </div>
  );
}

function DiscoveredRow({
  repository,
  alreadyOpen,
  opening,
  error,
  onOpen,
}: {
  repository: DiscoveredRepository;
  alreadyOpen: boolean;
  opening: boolean;
  /** The failure of the open this row asked for, whole. */
  error: Error | null;
  onOpen: () => void;
}) {
  const messageId = useId();
  const summary = error === null ? undefined : errorSummary(error);

  return (
    <li className="flex flex-col">
      <button
        type="button"
        disabled={alreadyOpen || opening}
        onClick={onOpen}
        aria-describedby={summary === undefined ? undefined : messageId}
        className={cx(
          'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left transition-colors transition-instant',
          alreadyOpen ? 'cursor-default opacity-60' : 'hover:bg-hover focus-visible:focus-ring',
        )}
      >
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-ink">
          {repository.name}
        </span>
        {repository.bare && <Badge tone="neutral">bare</Badge>}
        {repository.kind !== 'top-level' && <Badge tone="info">{kindLabel(repository.kind)}</Badge>}
        {alreadyOpen && <Badge tone="neutral">open</Badge>}
        <span className="hidden max-w-48 truncate font-mono text-2xs text-ink-subtle md:inline">
          {repository.path}
        </span>
      </button>

      {/*
       * Beside the button, never inside it: GitFailureDetail carries a copy
       * button, and a button within a button is neither valid HTML nor
       * reachable — axe fails it as nested-interactive, under the WCAG 2.1 AA
       * set this project holds itself to.
       *
       * role="alert" where the path field makes do with aria-describedby
       * alone. The row disables itself while the request is in flight, which
       * hands focus back to the document, so by the time the failure lands
       * there is nobody on the row left to be told about it.
       *
       * On the wrapper rather than on the sentence, because the sentence is
       * the half that can be missing. `errorSummary` subtracts what the block
       * underneath already says — the daemon's message for a refused path
       * ends with the same command, exit code and stderr — and when the
       * message was that and nothing else there is no paragraph left to
       * announce, only the block. The failure has to be heard either way.
       */}
      {error !== null && (
        <div role="alert" className="flex flex-col gap-1.5 px-2 pt-0.5 pb-1.5">
          {summary !== undefined && (
            <p id={messageId} className="text-2xs text-danger">
              {summary}
            </p>
          )}
          {error instanceof ApiError && error.git !== undefined && (
            <GitFailureDetail failure={error.git} />
          )}
        </div>
      )}
    </li>
  );
}

function kindLabel(kind: DiscoveredKind): string {
  switch (kind) {
    case 'worktree':
      return 'worktree';
    case 'submodule':
      return 'submodule';
    default:
      return kind;
  }
}
