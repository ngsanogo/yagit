import type { WorkingDirectory } from '../api/types';

/**
 * What the three network buttons are, for the repository as it stands.
 *
 * A pure function over the status and the number of remotes, kept apart from
 * the bar that draws it for the reason conflict.ts is kept apart from the pane
 * that resolves one: every one of these decisions is a rule about git — a
 * detached HEAD has no branch to push, a branch that follows nothing is
 * published rather than pushed — and rules are worth reading and testing on
 * their own, not spelt out inside a JSX ternary.
 *
 * Each offer that cannot be made carries the sentence saying why. A button
 * that is simply absent teaches nobody where the action went, and one that is
 * greyed with no explanation is worse: it says "not now" and refuses to say
 * when.
 */

/** Pulling: possible, or not, with the reason. */
export type PullOffer = { kind: 'pull'; behind: number } | { kind: 'unavailable'; reason: string };

/**
 * Pushing: the ordinary case, the first time, or not at all.
 *
 * `publish` is a different operation and not a disabled push: it needs a
 * remote chosen by somebody, it records that choice, and it is the only one of
 * the two that can happen on a branch nobody has ever pushed.
 */
export type PushOffer =
  | { kind: 'push'; ahead: number; behind: number }
  | { kind: 'publish'; branch: string }
  | { kind: 'unavailable'; reason: string };

export interface RemoteOffers {
  /** Fetching needs nothing but a remote: it moves no branch and no file. */
  canFetch: boolean;
  pull: PullOffer;
  push: PushOffer;
  /** True when the current branch is ahead of its upstream and behind it too. */
  diverged: boolean;
}

/**
 * Why an operation that needs an upstream is refused on a branch that has none.
 *
 * Pull and force push both land here, and both had a sentence of their own
 * that said the same thing about the branch and differed only in the verb —
 * which is two strings to find when the publish action is renamed, and two
 * that can drift into describing one state two ways in one bar.
 *
 * "Follows no branch on a remote" rather than "is not on a remote yet",
 * because that is the reading this is built from: what is empty is the
 * branch's UPSTREAM. A branch of the same name may well be sitting on the
 * remote already, pushed from another clone or by somebody else, and telling
 * its author it is not there is a sentence they can check and find false.
 */
function withoutAnUpstream(branch: string, verb: string): string {
  return `${branch} follows no branch on a remote, so there is nothing to ${verb}. Publish it first.`;
}

/**
 * Whether a force push is worth offering.
 *
 * Level with the upstream, it would run a command whose only outcome is
 * "Everything up-to-date" — with the two most dangerous flags in this
 * application attached to it. It becomes real exactly when the branch has
 * moved away from what the remote holds, which is what a rewrite looks like
 * from here: an amend leaves one commit ahead and one behind, a reset leaves
 * only the behind.
 */
export function canForcePush(offer: PushOffer): boolean {
  return offer.kind === 'push' && (offer.ahead > 0 || offer.behind > 0);
}

/**
 * Why a force push is refused, when it is.
 *
 * The menu keeps the item and greys it, so this sentence is what the item
 * has to say instead of disappearing. Three different operations that are
 * not a force push, and one sentence each.
 */
export function forcePushUnavailableReason(offer: PushOffer): string {
  if (offer.kind === 'unavailable') {
    return offer.reason;
  }
  if (offer.kind === 'publish') {
    return withoutAnUpstream(offer.branch, 'force-update');
  }
  return 'Nothing here has moved away from the remote.';
}

export function remoteOffers(
  status: WorkingDirectory | undefined,
  remoteCount: number,
): RemoteOffers {
  const nothingConfigured = 'This repository has no remote configured.';

  if (remoteCount === 0) {
    return {
      canFetch: false,
      pull: { kind: 'unavailable', reason: nothingConfigured },
      push: { kind: 'unavailable', reason: nothingConfigured },
      diverged: false,
    };
  }

  if (status === undefined) {
    // The status has not answered yet. Fetching is still true — it depends on
    // the remotes alone — and the other two wait rather than guessing, because
    // the guess would be a Push button on a detached HEAD.
    //
    // Written without a trailing "…", which is not a detail here. What the
    // mark means everywhere else in this interface is that the control asks a
    // question before it acts — Delete…, Force push…, Unset upstream…,
    // Search… — and no spinner label in the application carries it, including
    // the workbench's own "Reading the work tree" for this very wait.
    // These two reasons wore it for "in progress" instead, so one mark carried
    // two opposite promises, and one sentence appeared on one screen spelt two
    // ways.
    const reading = 'Reading the work tree';
    return {
      canFetch: true,
      pull: { kind: 'unavailable', reason: reading },
      push: { kind: 'unavailable', reason: reading },
      diverged: false,
    };
  }

  if (status.detached) {
    // Neither operation has a meaning here. Pulling would merge into a commit
    // nothing points at, and pushing would have to invent a branch name.
    const detached = 'HEAD is detached, so there is no branch to pull into or push from.';
    return {
      canFetch: true,
      pull: { kind: 'unavailable', reason: detached },
      push: { kind: 'unavailable', reason: detached },
      diverged: false,
    };
  }

  if (status.unborn || status.branch === '') {
    const nothing = 'This branch has no commit yet.';
    return {
      canFetch: true,
      pull: { kind: 'unavailable', reason: nothing },
      push: { kind: 'unavailable', reason: nothing },
      diverged: false,
    };
  }

  const following = status.upstream !== undefined && status.upstream !== '';
  if (!following) {
    return {
      canFetch: true,
      pull: { kind: 'unavailable', reason: withoutAnUpstream(status.branch, 'pull') },
      push: { kind: 'publish', branch: status.branch },
      diverged: false,
    };
  }

  return {
    canFetch: true,
    pull: { kind: 'pull', behind: status.behind },
    push: { kind: 'push', ahead: status.ahead, behind: status.behind },
    diverged: status.ahead > 0 && status.behind > 0,
  };
}

/**
 * What the Pull button says it will do, in a sentence.
 *
 * The counts rather than the word alone, because they are the whole of the
 * decision: "bring 3 commits from origin/main" is an action, "Pull" is a verb
 * whose object the user has to go and find.
 *
 * Level with the upstream it says something else, and it is not "nothing to
 * do": `git pull` fetches before it integrates, so it is the one of the three
 * that is never a no-op. The count says what was true at the last fetch, and
 * pressing this is how somebody who has not fetched finds out it has moved.
 */
export function pullDescription(offer: PullOffer, upstream: string | undefined): string {
  if (offer.kind !== 'pull') {
    return offer.reason;
  }
  const from = upstream === undefined || upstream === '' ? 'the upstream' : upstream;
  if (offer.behind === 0) {
    return `Fetch ${from} and bring in anything new`;
  }
  return `Bring in ${plural(offer.behind, 'commit')} from ${from}`;
}

/** The same, for the Push button. */
export function pushDescription(offer: PushOffer, upstream: string | undefined): string {
  if (offer.kind === 'unavailable') {
    return offer.reason;
  }
  if (offer.kind === 'publish') {
    return `${offer.branch} has never been pushed — publishing records where it went`;
  }

  const to = upstream === undefined || upstream === '' ? 'the upstream' : upstream;
  if (offer.ahead === 0) {
    return `Nothing to send to ${to}`;
  }
  if (offer.behind > 0) {
    // The case worth its own sentence: git will refuse this push, and it is
    // right to. Saying so here is cheaper than the round trip that finds out,
    // and it names the fix.
    return `${to} has ${plural(offer.behind, 'commit')} this branch does not — pull first`;
  }
  return `Send ${plural(offer.ahead, 'commit')} to ${to}`;
}

function plural(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? '' : 's'}`;
}
