import { describe, expect, it } from 'vitest';

import type { Ref } from '../api/types';
import { initialSelectedRefs, refsKey } from './historyScope';

function ref(name: string, kind: Ref['kind'], shortName: string): Ref {
  return {
    name,
    short_name: shortName,
    kind,
    sha: 'a'.repeat(40),
    ahead: 0,
    behind: 0,
    gone: false,
  };
}

describe('the cache key for a chosen set of refs', () => {
  it('is empty under the scopes that do not read refs', () => {
    // Otherwise ticking a ref and then switching to the current branch would
    // invalidate a history that never depended on it.
    expect(refsKey('head', ['refs/heads/main'])).toBe('');
    expect(refsKey('all', ['refs/heads/main'])).toBe('');
  });

  it('does not depend on the order the boxes were ticked in', () => {
    expect(refsKey('refs', ['refs/heads/topic', 'refs/heads/main'])).toBe(
      refsKey('refs', ['refs/heads/main', 'refs/heads/topic']),
    );
  });

  it('tells two different sets apart', () => {
    expect(refsKey('refs', ['refs/heads/main'])).not.toBe(
      refsKey('refs', ['refs/heads/main', 'refs/heads/topic']),
    );
  });

  it('counts a ref ticked twice once', () => {
    expect(refsKey('refs', ['refs/heads/main', 'refs/heads/main'])).toBe(
      refsKey('refs', ['refs/heads/main']),
    );
  });
});

describe('what the picker opens on', () => {
  const main = ref('refs/heads/main', 'branch', 'main');
  const topic = ref('refs/heads/topic', 'branch', 'topic');
  const mirror = ref('refs/remotes/origin/main', 'remote', 'origin/main');

  it('is the branch HEAD is on, so the first click is a change and not a repair', () => {
    expect(initialSelectedRefs([topic, main], 'main')).toEqual(['refs/heads/main']);
  });

  it('falls back to a branch when HEAD is detached', () => {
    expect(initialSelectedRefs([topic, main], '')).toEqual(['refs/heads/topic']);
    expect(initialSelectedRefs([topic, main], undefined)).toEqual(['refs/heads/topic']);
  });

  it('takes a remote-tracking ref where there is no local branch at all', () => {
    expect(initialSelectedRefs([mirror], undefined)).toEqual(['refs/remotes/origin/main']);
  });

  it('is empty only where there is nothing, which the caller keeps off screen', () => {
    // The daemon refuses scope=refs with none, so an option that could only
    // be chosen into a refusal is not offered.
    expect(initialSelectedRefs([], 'main')).toEqual([]);
  });
});
