import type { Ref } from '../api/types';

/**
 * Reading and offering the branch a local branch follows.
 *
 * Its own module for the reason merge, reset, rebase and remote each have one:
 * what it decides is worth a test, and a test of it should not have to mount
 * the workbench to ask a question about two strings.
 */

/** Reads refs/remotes/origin/main into remote and branch names. */
export function parseUpstreamRef(
  full: string | undefined,
): { remote: string; branch: string } | undefined {
  const prefix = 'refs/remotes/';
  if (full === undefined || full === '' || !full.startsWith(prefix)) {
    return undefined;
  }
  const rest = full.slice(prefix.length);
  const slash = rest.indexOf('/');
  if (slash === -1) {
    return undefined;
  }
  return { remote: rest.slice(0, slash), branch: rest.slice(slash + 1) };
}

/**
 * Branch names under one remote that a local branch can be set to follow.
 *
 * `refs/remotes/<remote>/HEAD` is left out, and it is the reason this function
 * is tested. git writes it at clone time as a SYMBOLIC ref — it points at
 * whichever branch the remote calls its default — so it appears in
 * `for-each-ref` beside the real ones and sorts to the top, capital letters
 * first. Offering it means offering `--set-upstream-to=origin/HEAD`, shown to
 * the user as the exact command about to run, for a branch that does not exist
 * on the remote. Every repository that came from `git clone` has one.
 */
export function trackingBranchesOnRemote(refs: readonly Ref[], remote: string): string[] {
  const prefix = `refs/remotes/${remote}/`;
  return refs
    .filter(
      (entry) =>
        entry.kind === 'remote' && entry.name.startsWith(prefix) && entry.name !== `${prefix}HEAD`,
    )
    .map((entry) => entry.short_name.slice(remote.length + 1))
    .sort();
}
