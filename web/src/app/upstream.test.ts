import { describe, expect, it } from 'vitest';

import type { Ref } from '../api/types';
import { parseUpstreamRef, trackingBranchesOnRemote } from './upstream';

function ref(kind: Ref['kind'], name: string, shortName: string): Ref {
  return {
    kind,
    name,
    short_name: shortName,
    sha: '0'.repeat(40),
    ahead: 0,
    behind: 0,
    gone: false,
  };
}

function remoteRef(shortName: string): Ref {
  return ref('remote', `refs/remotes/${shortName}`, shortName);
}

describe('parseUpstreamRef', () => {
  it('splits a remote-tracking ref into its remote and its branch', () => {
    expect(parseUpstreamRef('refs/remotes/origin/main')).toEqual({
      remote: 'origin',
      branch: 'main',
    });
  });

  it('keeps the slashes inside a namespaced branch', () => {
    expect(parseUpstreamRef('refs/remotes/upstream/team/feature/login')).toEqual({
      remote: 'upstream',
      branch: 'team/feature/login',
    });
  });

  it('answers nothing for a branch that follows nothing', () => {
    expect(parseUpstreamRef(undefined)).toBeUndefined();
    expect(parseUpstreamRef('')).toBeUndefined();
    expect(parseUpstreamRef('refs/heads/main')).toBeUndefined();
    // A remote with no branch after it is not a ref anything can follow.
    expect(parseUpstreamRef('refs/remotes/origin')).toBeUndefined();
  });
});

describe('trackingBranchesOnRemote', () => {
  it('offers the branches under the named remote, sorted', () => {
    const refs = [remoteRef('origin/main'), remoteRef('origin/develop')];
    expect(trackingBranchesOnRemote(refs, 'origin')).toEqual(['develop', 'main']);
  });

  it('never offers origin/HEAD', () => {
    // git writes refs/remotes/<remote>/HEAD at clone time as a symbolic ref
    // pointing at the remote's default branch. It sorts to the top — capitals
    // first — so it used to be the first thing offered, and choosing it built
    // `--set-upstream-to=origin/HEAD` for a branch no remote has.
    const refs = [remoteRef('origin/HEAD'), remoteRef('origin/main')];
    expect(trackingBranchesOnRemote(refs, 'origin')).toEqual(['main']);
  });

  it('keeps a branch that merely starts with HEAD', () => {
    // The exclusion is the ref itself, not a prefix: `HEADless` is a legal
    // branch name and somebody has it.
    const refs = [remoteRef('origin/HEADless'), remoteRef('origin/HEAD')];
    expect(trackingBranchesOnRemote(refs, 'origin')).toEqual(['HEADless']);
  });

  it('ignores the other remotes and the local branches', () => {
    const refs = [
      remoteRef('origin/main'),
      remoteRef('fork/main'),
      ref('branch', 'refs/heads/main', 'main'),
    ];
    expect(trackingBranchesOnRemote(refs, 'origin')).toEqual(['main']);
  });

  it('keeps the slashes of a namespaced branch', () => {
    const refs = [remoteRef('origin/team/feature/login')];
    expect(trackingBranchesOnRemote(refs, 'origin')).toEqual(['team/feature/login']);
  });
});
