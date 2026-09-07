import { useState } from 'react';

import type { Ref } from '../api/types';
import { Button } from '../components/Button';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { cx } from '../lib/cx';
import { pluralize } from '../lib/format';

/**
 * Which references the graph is drawn from, when the answer is neither "the
 * one I am on" nor "all of them".
 *
 * The middle of ADR 0033. Every ref is the picture that does not exist on a
 * repository with enough tags — git's own is 280 columns — and the current
 * branch alone cannot answer "how far has this topic drifted from main". What
 * is wanted then is two or three refs, chosen, and the graph between them.
 *
 * Checkboxes and not a multi-select, because the list is long and the choice
 * is read as often as it is made: on a repository with a thousand tags the
 * filter below is what makes it usable at all, and a native multiple-select
 * cannot be filtered.
 *
 * At least one, always. The daemon refuses `scope=refs` with none — an empty
 * walk drawn is indistinguishable from an empty repository — so the last
 * ticked box cannot be cleared, and the control says why rather than answering
 * a refusal.
 */
export function RefPicker({
  refs,
  selected,
  onChange,
}: {
  refs: readonly Ref[];
  selected: readonly string[];
  onChange: (selected: string[]) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button size="sm" variant="ghost" onClick={() => setOpen(true)}>
        {selected.length === 0 ? 'Pick references…' : `${pluralize(selected.length, 'reference')}…`}
      </Button>

      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="Draw the graph from"
        description="The graph is walked from the references ticked here, and from nothing else."
      >
        {/* Mounted only while it is open, for the reason the add-repository
            dialog is: a native <dialog> keeps its children in the document
            once closed, so the filter typed last time would still be sitting
            in the box the next time it appeared. */}
        {open && <RefChoices refs={refs} selected={selected} onChange={onChange} />}
      </Dialog>
    </>
  );
}

function RefChoices({
  refs,
  selected,
  onChange,
}: {
  refs: readonly Ref[];
  selected: readonly string[];
  onChange: (selected: string[]) => void;
}) {
  const [filter, setFilter] = useState('');

  const needle = filter.trim().toLowerCase();
  const shown =
    needle === ''
      ? refs
      : refs.filter(
          (ref) =>
            ref.short_name.toLowerCase().includes(needle) ||
            ref.name.toLowerCase().includes(needle),
        );

  // The last one cannot be cleared: see the note on RefPicker. Named as a
  // sentence rather than a boolean so the reason can be shown where the box
  // is, which is the only place somebody clicking it is looking.
  const lastOne = selected.length === 1;

  const toggle = (ref: Ref, ticked: boolean) => {
    if (ticked) {
      onChange([...selected, ref.name]);
      return;
    }
    if (lastOne) {
      return;
    }
    onChange(selected.filter((name) => name !== ref.name));
  };

  return (
    <div className="flex flex-col gap-3">
      <Field
        label="Filter"
        hint="By name. A repository with a thousand tags is why this is here."
        value={filter}
        onChange={(event) => setFilter(event.target.value)}
        placeholder="main, v1., origin/…"
        autoComplete="off"
        spellCheck={false}
      />

      {lastOne && (
        <p className="text-2xs text-ink-subtle">
          One reference has to stay ticked: a graph drawn from none of them is an empty picture,
          which is not the same thing as an empty repository.
        </p>
      )}

      <div className="max-h-72 min-h-0 overflow-auto rounded-md border border-line">
        {shown.length === 0 ? (
          <p className="px-3 py-4 text-xs text-ink-subtle">No reference matches that.</p>
        ) : (
          shown.map((ref) => {
            const ticked = selected.includes(ref.name);
            return (
              <label
                key={ref.name}
                className={cx(
                  'flex cursor-pointer items-center gap-2 px-3 py-1.5 text-xs',
                  'transition-colors transition-instant hover:bg-sunken',
                )}
              >
                <input
                  type="checkbox"
                  checked={ticked}
                  disabled={ticked && lastOne}
                  onChange={(event) => toggle(ref, event.target.checked)}
                  className="accent-accent"
                />
                <span className="min-w-0 flex-1 truncate font-mono text-ink">{ref.short_name}</span>
                <span className="shrink-0 text-2xs text-ink-subtle">{ref.kind}</span>
              </label>
            );
          })
        )}
      </div>
    </div>
  );
}
