import { describe, expect, it } from 'vitest';

import type { FileDiff } from '../api/types';
import {
  blobURL,
  imageSidesForRevision,
  imageSidesForWorkingDiff,
  isImageDiff,
  isImagePath,
} from './imageDiff';

const binaryImage: FileDiff = {
  id: 'fingerprint',
  path: 'logo.png',
  binary: true,
  added: false,
  removed: false,
  hunks: [],
};

describe('isImagePath', () => {
  it('accepts the extensions the blob route serves', () => {
    for (const path of ['a.png', 'b.JPG', 'c.jpeg', 'd.gif', 'e.webp', 'f.svg']) {
      expect(isImagePath(path)).toBe(true);
    }
  });

  it('refuses everything else', () => {
    expect(isImagePath('a.txt')).toBe(false);
    expect(isImagePath('logo.png.bak')).toBe(false);
  });
});

describe('isImageDiff', () => {
  it('needs both binary and an image name', () => {
    expect(isImageDiff(binaryImage)).toBe(true);
    expect(isImageDiff({ ...binaryImage, binary: false })).toBe(false);
    expect(isImageDiff({ ...binaryImage, path: 'data.bin' })).toBe(false);
  });

  it('reads the old name on a rename', () => {
    expect(
      isImageDiff({
        ...binaryImage,
        path: 'logo.bin',
        old_path: 'was.png',
      }),
    ).toBe(true);
  });
});

describe('imageSidesForWorkingDiff', () => {
  it('maps staged to HEAD then index', () => {
    expect(imageSidesForWorkingDiff(binaryImage, 'staged')).toEqual([
      { path: 'logo.png', at: 'head', label: 'HEAD' },
      { path: 'logo.png', at: 'index', label: 'staged' },
    ]);
  });

  it('maps unstaged to index then worktree', () => {
    expect(imageSidesForWorkingDiff(binaryImage, 'unstaged')).toEqual([
      { path: 'logo.png', at: 'index', label: 'index' },
      { path: 'logo.png', at: 'worktree', label: 'worktree' },
    ]);
  });

  it('keeps only the new side for an addition', () => {
    expect(imageSidesForWorkingDiff({ ...binaryImage, added: true }, 'unstaged')).toEqual([
      { path: 'logo.png', at: 'worktree', label: 'worktree' },
    ]);
  });

  it('keeps only the old side for a deletion', () => {
    expect(imageSidesForWorkingDiff({ ...binaryImage, removed: true }, 'staged')).toEqual([
      { path: 'logo.png', at: 'head', label: 'HEAD' },
    ]);
  });
});

describe('imageSidesForRevision', () => {
  it('compares the parent to the revision', () => {
    expect(imageSidesForRevision(binaryImage, 'abc')).toEqual([
      { path: 'logo.png', at: 'abc^', label: 'before' },
      { path: 'logo.png', at: 'abc', label: 'after' },
    ]);
  });
});

describe('blobURL', () => {
  it('encodes the path and the place', () => {
    expect(blobURL('repo', { path: 'dir/a.png', at: 'worktree', label: 'worktree' })).toBe(
      '/api/repos/repo/blob?path=dir%2Fa.png&at=worktree',
    );
  });
});
