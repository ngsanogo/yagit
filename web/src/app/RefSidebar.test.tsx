import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import type { Head, Ref } from '../api/types';
import { branchMenuItems, checkOutRequestFor, RefSidebar } from './RefSidebar';

/**
 * What each row offers, and what it sends when the offer is taken.
 *
 * The mapping below is the part with consequences: it decides whether a click
 * runs `git switch` or `git switch --detach`, and it is the one place a button
 * could come to mean something other than what it says. The markup assertions
 * beside it cover the two rows that must offer nothing at all — the branch
 * already checked out, and every row of a repository that has no work tree —
 * and branchMenuItems covers what is behind the button, which no markup can
 * show because a closed menu has not built it.
 *
 * Rendered to markup rather than into a browser, like CommitList's tests: this
 * sidebar is a function of its props, and the end-to-end suite is where a
 * click actually lands.
 */

function reference(overrides: Partial<Ref> = {}): Ref {
  return {
    name: 'refs/heads/main',
    short_name: 'main',
    kind: 'branch',
    sha: '0f1e2d3c4b5a69788796a5b4c3d2e1f009182736',
    ahead: 0,
    behind: 0,
    gone: false,
    ...overrides,
  };
}

const onMain: Head = {
  sha: '0f1e2d3c4b5a69788796a5b4c3d2e1f009182736',
  name: 'main',
  detached: false,
};

function sidebar(
  refs: Ref[],
  options: { head?: Head; checkable?: boolean; editable?: boolean } = {},
): string {
  const editable = options.editable !== false;
  return renderToStaticMarkup(
    <RefSidebar
      refs={refs}
      head={options.head ?? onMain}
      onGoTo={() => undefined}
      onCheckOut={options.checkable === false ? undefined : () => undefined}
      onRenameBranch={editable ? () => undefined : undefined}
      onDeleteBranch={editable ? () => undefined : undefined}
      onMergeBranch={editable ? () => undefined : undefined}
      onRebaseBranch={editable ? () => undefined : undefined}
    />,
  );
}

/** The menu of one row, as the arrows would walk it. */
function menuOf(
  overrides: {
    reference?: Ref;
    current?: boolean;
    detached?: boolean;
    mergeable?: boolean;
    rebaseable?: boolean;
    upstreamable?: boolean;
  } = {},
) {
  return branchMenuItems({
    reference: overrides.reference ?? reference(),
    current: overrides.current ?? false,
    detached: overrides.detached ?? false,
    onRename: () => undefined,
    onDelete: () => undefined,
    ...(overrides.upstreamable === false ? {} : { onSetUpstream: () => undefined }),
    ...(overrides.upstreamable === false ? {} : { onUnsetUpstream: () => undefined }),
    ...(overrides.mergeable === false ? {} : { onMerge: () => undefined }),
    ...(overrides.rebaseable === false ? {} : { onRebase: () => undefined }),
  });
}

function item(items: ReturnType<typeof menuOf>, id: string) {
  return items.find((entry) => entry.id === id);
}

describe('what checking out a row asks for', () => {
  it('switches to a local branch by the only name git switch accepts', () => {
    expect(checkOutRequestFor(reference())).toEqual({
      ref: 'main',
      detach: false,
      label: 'main',
    });
  });

  // A remote-tracking branch is not a place HEAD can sit, and its full name is
  // what tells it apart from a local branch that happens to be called the
  // same. `git switch origin/main` is a different failure every time.
  it('detaches at a remote-tracking branch, by its full name', () => {
    const remote = reference({
      name: 'refs/remotes/origin/main',
      short_name: 'origin/main',
      kind: 'remote',
    });

    expect(checkOutRequestFor(remote)).toEqual({
      ref: 'refs/remotes/origin/main',
      detach: true,
      label: 'origin/main',
    });
  });

  it('detaches at a tag, by its full name', () => {
    const tag = reference({ name: 'refs/tags/v1.0', short_name: 'v1.0', kind: 'tag' });

    expect(checkOutRequestFor(tag)).toEqual({
      ref: 'refs/tags/v1.0',
      detach: true,
      label: 'v1.0',
    });
  });

  // Notes, stashes, anything another tool wrote under refs/. None of them is a
  // branch, so none of them is switched to.
  it('detaches at a reference of no known kind', () => {
    const other = reference({
      name: 'refs/notes/commits',
      short_name: 'refs/notes/commits',
      kind: 'other',
    });

    expect(checkOutRequestFor(other).detach).toBe(true);
  });
});

describe('which rows offer a checkout', () => {
  it('offers none on the branch that is already checked out', () => {
    const markup = sidebar([reference()]);

    expect(markup).not.toContain('Check out');
  });

  it('offers one on a branch that is not', () => {
    const markup = sidebar([
      reference(),
      reference({ name: 'refs/heads/side', short_name: 'side' }),
    ]);

    expect(markup).toContain('aria-label="Check out side"');
  });

  // The words on the button are the same either way; the accessible name is
  // where the difference is said, because it is a fact about git rather than
  // about the click.
  it('says so when the checkout will leave HEAD detached', () => {
    const markup = sidebar([
      reference({ name: 'refs/tags/v1.0', short_name: 'v1.0', kind: 'tag' }),
    ]);

    expect(markup).toContain('aria-label="Check out v1.0, leaving HEAD detached"');
  });

  // A bare repository has no work tree. The daemon refuses the request, and a
  // button for it would be a button that only ever produces an error.
  it('offers none at all where nothing can be checked out', () => {
    const markup = sidebar([reference({ name: 'refs/heads/side', short_name: 'side' })], {
      checkable: false,
    });

    expect(markup).not.toContain('Check out');
  });
});

describe('which rows offer the actions behind the menu', () => {
  it('gives every local branch a menu, whether or not it is checked out', () => {
    const markup = sidebar([
      reference(),
      reference({ name: 'refs/heads/side', short_name: 'side' }),
    ]);

    expect(markup).toContain('More actions for main');
    expect(markup).toContain('More actions for side');
  });

  it('gives none to a tag or a remote-tracking branch', () => {
    // Neither is a local branch. A menu whose every item is refused teaches
    // the reader that the menu is pointless rather than that the action is.
    const markup = sidebar([
      reference({ name: 'refs/tags/v1', short_name: 'v1', kind: 'tag' }),
      reference({ name: 'refs/remotes/origin/main', short_name: 'origin/main', kind: 'remote' }),
    ]);

    expect(markup).not.toContain('More actions');
  });

  it('gives the branch HEAD is on a menu and no checkout button', () => {
    // The one row where the menu is the only thing there: nothing to check out
    // on the branch already checked out. What that menu HOLDS is the subject
    // of branchMenuItems below — a closed menu builds no items, so static
    // markup cannot be asked.
    const markup = sidebar([reference()], { head: onMain });

    expect(markup).toContain('More actions for main');
    expect(markup).not.toContain('Check out main');
  });

  it('offers no menu at all when nothing can edit a branch', () => {
    expect(sidebar([reference()], { editable: false })).not.toContain('More actions');
  });
});

describe('what a branch row offers behind its menu', () => {
  // The order the arrows walk, which is also the order somebody reading with a
  // screen reader hears. It runs from the reversible one to the one that
  // destroys the most: rename, then the merge that only adds, then the rebase
  // that writes the branch's commits again, then the delete.
  it('offers rename, upstream, merge, rebase and delete, in that order', () => {
    expect(menuOf().map((entry) => entry.id)).toEqual([
      'rename',
      'set-upstream',
      'merge',
      'rebase',
      'delete',
    ]);
  });

  it('names change and unset upstream from whether the branch already follows something', () => {
    const without = menuOf();
    expect(item(without, 'set-upstream')?.label).toBe('Set upstream…');
    expect(item(without, 'unset-upstream')).toBeUndefined();

    const withUpstream = menuOf({
      reference: reference({ upstream: 'refs/remotes/origin/main' }),
    });
    expect(item(withUpstream, 'set-upstream')?.label).toBe('Change upstream…');
    expect(item(withUpstream, 'unset-upstream')?.label).toBe('Unset upstream…');
  });

  // Refused rather than dropped: git refuses all three, and the reason is
  // worth reading. A menu that is four items on one row and one on the next
  // teaches nobody where the action went.
  it('refuses merge, rebase and delete on the branch HEAD is on', () => {
    const items = menuOf({ current: true });

    expect(item(items, 'merge')?.disabled).toBe(true);
    expect(item(items, 'merge')?.reason).toContain('into itself');
    expect(item(items, 'rebase')?.disabled).toBe(true);
    expect(item(items, 'rebase')?.reason).toContain('onto itself');
    expect(item(items, 'delete')?.disabled).toBe(true);
    expect(item(items, 'delete')?.reason).toContain('cannot be deleted');
  });

  // The case the sidebar would otherwise say nothing about: HEAD is on no
  // branch, so there is nothing for a merge to go into and nothing for a
  // rebase to move — on every row at once.
  it('keeps merge and rebase on the menu and refuses them while HEAD is detached', () => {
    const items = menuOf({ detached: true });

    expect(item(items, 'merge')).toBeDefined();
    expect(item(items, 'merge')?.disabled).toBe(true);
    expect(item(items, 'merge')?.reason).toContain('detached');
    expect(item(items, 'rebase')).toBeDefined();
    expect(item(items, 'rebase')?.disabled).toBe(true);
    expect(item(items, 'rebase')?.reason).toContain('detached');
    // And the delete is untouched by it: a branch nobody is standing on can
    // still be deleted from a detached HEAD. Offered items omit disabled
    // rather than setting it false — Menu reads `disabled === true`.
    expect(item(items, 'delete')?.disabled).not.toBe(true);
    expect(item(items, 'delete')?.reason).toBeUndefined();
  });

  // A bare repository has no work tree, so no repository in that shape can
  // ever merge or rebase. That is the one case where the items go rather than
  // grey.
  it('drops merge and rebase where the repository could never do one', () => {
    expect(item(menuOf({ mergeable: false, rebaseable: false }), 'merge')).toBeUndefined();
    expect(item(menuOf({ mergeable: false, rebaseable: false }), 'rebase')).toBeUndefined();
    expect(menuOf({ mergeable: false, rebaseable: false }).map((entry) => entry.id)).toEqual([
      'rename',
      'set-upstream',
      'delete',
    ]);
  });

  // Red is the only thing the menu can say about what an item costs: the plan
  // is not asked for until the item is chosen, so this is the last place
  // before the dialog where a rebase can be told apart from a merge.
  it('draws the two that take something away as dangerous', () => {
    const items = menuOf();

    expect(item(items, 'rebase')?.danger).toBe(true);
    expect(item(items, 'delete')?.danger).toBe(true);
    expect(item(items, 'merge')?.danger).toBeUndefined();
    expect(item(items, 'rename')?.danger).toBeUndefined();
  });

  // Each callback answers for itself. Reading the three as one gate meant a
  // caller that could merge and not rename got no button at all — the
  // capability it declared, dropped, silently.
  it('offers whichever actions the caller wired, one for one', () => {
    expect(
      branchMenuItems({
        reference: reference(),
        current: false,
        detached: false,
        onMerge: () => undefined,
      }).map((entry) => entry.id),
    ).toEqual(['merge']);

    expect(
      branchMenuItems({
        reference: reference(),
        current: false,
        detached: false,
        onRename: () => undefined,
      }).map((entry) => entry.id),
    ).toEqual(['rename']);
  });

  // And a row with nothing behind it draws no button, which is Menu's own
  // answer to an empty list.
  it('gives a branch no items where the repository can do nothing to it', () => {
    expect(branchMenuItems({ reference: reference(), current: false, detached: false })).toEqual(
      [],
    );
  });

  it('gives a tag and a remote-tracking branch no items at all', () => {
    const tag = reference({ name: 'refs/tags/v1', short_name: 'v1', kind: 'tag' });
    const remote = reference({
      name: 'refs/remotes/origin/main',
      short_name: 'origin/main',
      kind: 'remote',
    });

    expect(menuOf({ reference: tag })).toEqual([]);
    expect(menuOf({ reference: remote })).toEqual([]);
  });
});
