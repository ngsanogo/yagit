import type { FileStatus } from '../api/types';

/**
 * What yagit would call this commit, from what is staged.
 *
 * A commit that adds one file is "Add src/parser.ts" nine times in ten, and
 * typing that out is work the interface can do. So it does — as a PROPOSAL. It
 * is drawn in the box as placeholder text and one key accepts it; it is never
 * committed on the user's behalf. That line is deliberate, and it is where
 * this differs from the message git prepares for a merge: that one is git's
 * own sentence about the user's own operation and goes in as real text, while
 * this is a guess made from a file list, and a guess nobody read is not a
 * commit message.
 *
 * The rules below come from what the staged files ARE, never from their
 * content. Nothing here opens a diff: a summariser that read the patch would
 * be wrong in interesting ways instead of obvious ones, and an obviously wrong
 * suggestion is one the user corrects rather than one they ship.
 */

/**
 * How the subject reads for one file, and for many.
 *
 * The imperative mood, because that is the mood git's own messages are in —
 * "Merge branch", "Revert", "Initial commit" — and because a subject completes
 * the sentence "applying this commit will …".
 */
type Verb = 'add' | 'update' | 'delete' | 'rename' | 'copy' | 'retype';

const PLURAL_VERB: Record<Verb, string> = {
  add: 'Add',
  update: 'Update',
  delete: 'Delete',
  rename: 'Rename',
  copy: 'Copy',
  retype: 'Change the type of',
};

/**
 * What a staged file has had done to it.
 *
 * The INDEX code is the authority and the work-tree code is ignored, and that
 * is the whole of it: a commit records the index. A file added and then edited
 * again shows "AM", and what the commit will contain is the addition.
 */
function verbOf(file: FileStatus): Verb {
  if (file.kind === 'renamed') {
    return 'rename';
  }
  if (file.kind === 'copied') {
    return 'copy';
  }
  switch (file.index) {
    case 'A':
      return 'add';
    case 'D':
      return 'delete';
    case 'T':
      // git's type change: a file became a symlink, or a directory became a
      // submodule. Neither "update" nor "add" describes it, and the difference
      // is exactly the sort a reader of the log needs.
      return 'retype';
    default:
      return 'update';
  }
}

/**
 * The suggestion for a set of staged files, or the empty string when there is
 * nothing to suggest.
 *
 * Empty is a real answer and the caller acts on it: nothing staged is nothing
 * to describe, and a placeholder reading "Update 0 files" would be the
 * interface talking to itself.
 */
export function suggestCommitMessage(staged: readonly FileStatus[]): string {
  if (staged.length === 0) {
    return '';
  }

  const [first] = staged;
  if (staged.length === 1 && first !== undefined) {
    return oneFile(first);
  }

  return manyFiles(staged);
}

function oneFile(file: FileStatus): string {
  const verb = verbOf(file);

  if ((verb === 'rename' || verb === 'copy') && file.old_path !== undefined) {
    // "Move" rather than "Rename" when the name did not change, which is what
    // a rename between directories is. Saying "Rename a/x.ts to b/x.ts" sends
    // the reader looking for a new name that is not there.
    const moved = verb === 'rename' && basename(file.old_path) === basename(file.path);
    return `${moved ? 'Move' : PLURAL_VERB[verb]} ${file.old_path} to ${file.path}`;
  }

  return `${PLURAL_VERB[verb]} ${file.path}`;
}

function manyFiles(staged: readonly FileStatus[]): string {
  const verbs = new Set(staged.map(verbOf));

  // One verb for all of them, or the neutral one. "Update" covers a mixed set
  // honestly enough — the commit updates the repository — where naming the
  // most common of them would say "Add 5 files" about a commit that deleted
  // two.
  const verb: Verb = verbs.size === 1 ? ([...verbs][0] ?? 'update') : 'update';

  const where = commonDirectory(staged.map((file) => file.path));
  const files = `${staged.length} files`;

  return where === ''
    ? `${PLURAL_VERB[verb]} ${files}`
    : `${PLURAL_VERB[verb]} ${files} in ${where}`;
}

/**
 * The deepest directory every path is inside, or the empty string when that is
 * the repository root.
 *
 * Named because it is the difference between "Update 4 files" and "Update 4
 * files in internal/git", and the second is a subject somebody can scan a log
 * with. The root is left unsaid rather than written out: every path in the
 * repository is in it, so naming it adds a word and no information.
 *
 * Whole components, never a common string prefix. `src/apple.ts` and
 * `src/apricot.ts` share the five characters "src/ap", which names no
 * directory and would produce "in src/ap".
 */
export function commonDirectory(paths: readonly string[]): string {
  const [first, ...rest] = paths;
  if (first === undefined) {
    return '';
  }

  let common = directoryOf(first)
    .split('/')
    .filter((part) => part !== '');

  for (const path of rest) {
    const parts = directoryOf(path)
      .split('/')
      .filter((part) => part !== '');

    let shared = 0;
    while (shared < common.length && shared < parts.length && common[shared] === parts[shared]) {
      shared += 1;
    }
    common = common.slice(0, shared);

    if (common.length === 0) {
      return '';
    }
  }

  return common.join('/');
}

function directoryOf(path: string): string {
  const cut = path.lastIndexOf('/');
  return cut < 0 ? '' : path.slice(0, cut);
}

function basename(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1);
}
