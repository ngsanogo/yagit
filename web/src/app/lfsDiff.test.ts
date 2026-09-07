import { describe, expect, it } from 'vitest';

import type { FileDiff } from '../api/types';
import { describeSize, pointerNote } from './lfsDiff';

const base: FileDiff = {
  id: 'diff',
  path: 'art/cover.psd',
  binary: false,
  added: false,
  removed: false,
  hunks: [],
};

describe('pointerNote', () => {
  it('says nothing about a diff that holds no pointer', () => {
    expect(pointerNote(base)).toBeUndefined();
  });

  it('names both sizes when both sides are pointers', () => {
    const note = pointerNote({
      ...base,
      lfs: { old: { oid: 'sha256:aaa', size: 1024 }, new: { oid: 'sha256:bbb', size: 2048 } },
    });

    expect(note?.title).toBe('Stored with Git LFS');
    expect(note?.detail).toContain('1.00 KiB');
    expect(note?.detail).toContain('2.00 KiB');
  });

  // A mode change on a file stored in LFS: the pointer on both sides is the
  // same object, and saying "it changed" would be the pane inventing a
  // difference the reader then goes looking for.
  it('says the pointer is unchanged when both sides name one object', () => {
    const same = { oid: 'sha256:aaa', size: 4096 };
    const note = pointerNote({ ...base, lfs: { old: same, new: same } });

    expect(note?.detail).toContain('unchanged');
    expect(note?.detail).toContain('4.00 KiB');
  });

  // The two cases that read as a deletion and an addition unless they are
  // named. Getting these the wrong way round would tell somebody their file
  // was deleted when it was moved into LFS.
  it('names a file moved into LFS, where only the new side is a pointer', () => {
    const note = pointerNote({ ...base, lfs: { new: { oid: 'sha256:aaa', size: 10 } } });

    expect(note?.title).toBe('Moved into Git LFS');
    expect(note?.detail).toContain('Nothing was deleted');
  });

  it('names a file taken back out of LFS, where only the old side is a pointer', () => {
    const note = pointerNote({ ...base, lfs: { old: { oid: 'sha256:aaa', size: 10 } } });

    expect(note?.title).toBe('Taken out of Git LFS');
    expect(note?.detail).toContain('Nothing was added');
  });

  // A file ADDED under LFS has only a new side too, and it is not a file that
  // moved anywhere: there is no left-hand side, so a note about what is on it
  // describes a diff nobody is looking at.
  it('does not talk about a left-hand side for a file the diff adds', () => {
    const note = pointerNote({
      ...base,
      added: true,
      lfs: { new: { oid: 'sha256:aaa', size: 2048 } },
    });

    expect(note?.title).toBe('Added, stored with Git LFS');
    expect(note?.detail).toContain('2.00 KiB');
    expect(note?.detail).not.toContain('on the left');
  });

  // And the mirror: a deletion has only an old side, and the right is not the
  // file coming back out of LFS — there is no right at all.
  it('does not claim the file is on the right for a file the diff removes', () => {
    const note = pointerNote({
      ...base,
      removed: true,
      lfs: { old: { oid: 'sha256:aaa', size: 2048 } },
    });

    expect(note?.title).toBe('Deleted, was stored with Git LFS');
    expect(note?.detail).toContain('2.00 KiB');
    expect(note?.detail).not.toContain('the right is the file itself');
  });
});

describe('describeSize', () => {
  it('leaves whole bytes whole', () => {
    expect(describeSize(0)).toBe('0 bytes');
    expect(describeSize(999)).toBe('999 bytes');
  });

  it('climbs the units git itself prints', () => {
    expect(describeSize(1024)).toBe('1.00 KiB');
    expect(describeSize(1024 * 1024)).toBe('1.00 MiB');
    expect(describeSize(3 * 1024 * 1024 * 1024)).toBe('3.00 GiB');
  });
});
