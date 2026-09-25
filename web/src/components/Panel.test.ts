import { describe, expect, it } from 'vitest';

import { splitTitle } from './Panel';

describe('splitTitle', () => {
  it('leaves a bare name whole', () => {
    expect(splitTitle('References')).toEqual(['References', undefined]);
  });

  it('splits the name from what it counts', () => {
    expect(splitTitle('History — 17')).toEqual(['History', '17']);
    expect(splitTitle('Changes — 1 staged, 2 unstaged')).toEqual([
      'Changes',
      '1 staged, 2 unstaged',
    ]);
  });

  it('puts the two halves back together as the string it was given', () => {
    for (const title of [
      'History — 17',
      'Stashes — 1',
      'Commit',
      'Changes — 0 staged, 2 unstaged',
    ]) {
      const [name, count] = splitTitle(title);
      expect(count === undefined ? name : `${name} — ${count}`).toBe(title);
    }
  });

  it('has nothing to say about no title', () => {
    expect(splitTitle(undefined)).toEqual([undefined, undefined]);
  });
});
