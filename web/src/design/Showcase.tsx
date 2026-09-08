import { useEffect, useId, useState } from 'react';
import type { ReactNode } from 'react';

import { ApiError } from '../api/client';
import type { CommitRow, FileDiff, FileStatus, GitExecution, GitFailure } from '../api/types';
import { CommitRowView } from '../app/CommitList';
import { DiffView } from '../app/DiffView';
import { ROW_HEIGHT } from '../app/geometry';
import type { PageFailure } from '../app/useHistory';
import { Avatar, AVATAR_SIZE } from '../components/Avatar';
import { Badge, RefBadge } from '../components/Badge';
import { Button } from '../components/Button';
import { CommandLogPanel } from '../components/CommandLogPanel';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { EmptyState } from '../components/EmptyState';
import { Field } from '../components/Field';
import { FileStatusMark } from '../components/FileStatusMark';
import { GitCommand } from '../components/GitCommand';
import { Kbd } from '../components/Kbd';
import { Menu, menuItem, type MenuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { Centered, QueryErrorState } from '../components/PanelState';
import { SegmentedControl } from '../components/SegmentedControl';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';
import { Tabs } from '../components/Tabs';
import { Toast } from '../components/Toast';
import { Tooltip } from '../components/Tooltip';
import { cx } from '../lib/cx';
import { shortenPath } from '../lib/path';
import { commandModifier } from '../lib/platform';
import { LANE_COLOR_COUNT } from './tokens';
import { MoonGlyph, SunGlyph, SystemGlyph } from '../components/ThemeGlyphs';

/**
 * The design system showcase page.
 *
 * It exists for two reasons. First, because a design system drifts when
 * nobody ever sees it whole: every component appears here in all of its
 * states, so a regression stands out. Second, because it is the only place
 * the set can be judged together — a button on its own always looks fine;
 * it takes the full page to reveal the inconsistencies.
 *
 * Two of the bands below draw a component out of `app/` rather than out of
 * `components/`: the history's row and the diff's pane. That is a coupling and
 * it is the deliberate one. A row and a diff line are where this project's
 * layout decisions actually land — four column tracks that have to agree
 * across three states, a kind tint and a state background that have to
 * compose — and a hand-written copy of either here would be a picture of what
 * somebody believed the product drew. That belief is exactly what failed: a
 * two-line commit row spending two thirds of its width on nothing survived
 * twelve phases of work because this page had no commit row on it at all. The
 * arrow points one way — nothing under `app/` imports this file — so the cost
 * is a bundle that carries both screens, which it already did.
 */
/**
 * Which confirmation the page has open, and there is never more than one.
 *
 * Three of them, because the shape of the box is the lesson: the destructive
 * arm names what git takes away, the bounded arm names a consequence that is
 * not a loss, and the third is what "yagit will run, in this order" looks like
 * when an operation really is two commands.
 */
type ConfirmKind = 'destructive' | 'bounded' | 'ordered';

export function Showcase() {
  const [theme, setTheme] = useState<'dark' | 'light'>(initialTheme);
  const [confirmOpen, setConfirmOpen] = useState<ConfirmKind | null>(null);
  const [segment, setSegment] = useState<'history' | 'changes'>('changes');
  const [activeTab, setActiveTab] = useState('yagit');
  const [scope, setScope] = useState('head');
  const [logPressed, setLogPressed] = useState(true);
  const [selectedCommit, setSelectedCommit] = useState(SELECTED_SAMPLE_SHA);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  return (
    <div className="min-h-screen bg-canvas">
      <header className="sticky top-0 z-40 border-b border-line bg-canvas/85 backdrop-blur-md">
        <div className="mx-auto flex max-w-6xl items-center justify-between gap-4 px-8 py-4">
          <div className="flex items-baseline gap-3">
            <h1 className="text-lg font-semibold tracking-tight text-ink">yagit</h1>
            <span className="text-xs text-ink-subtle">Design system</span>
          </div>

          <div className="flex items-center gap-3">
            {/* Two states, where the workbench's control has three. This one is
                the page's own instrument rather than a copy of the product's:
                it is driven by `?theme=light` so a screenshot run can reach
                both themes without a click, and it names the theme it switches
                TO because that is what the e2e suite presses. The three states
                of the shipping control are drawn further down, as glyphs. */}
            <Button
              size="sm"
              onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
              leading={theme === 'dark' ? <SunGlyph /> : <MoonGlyph />}
            >
              {theme === 'dark' ? 'Light' : 'Dark'}
            </Button>
          </div>
        </div>
      </header>

      <main className="mx-auto flex max-w-6xl flex-col gap-14 px-8 py-12">
        <Section
          title="Type scale"
          note="Seven steps. Inter for the interface, JetBrains Mono for anything a machine wrote."
        >
          <div className="flex flex-col gap-4">
            {TYPE_STEPS.map((step) => (
              <div key={step.name} className="flex items-baseline gap-6 border-b border-line pb-3">
                <code className="w-24 shrink-0 font-mono text-2xs text-ink-subtle">
                  {step.name}
                </code>
                <span className="w-16 shrink-0 font-mono text-2xs text-ink-subtle tabular">
                  {step.size}
                </span>
                <span className={cx('truncate text-ink', step.className)}>
                  Rebase feature onto main
                </span>
              </div>
            ))}
            <div className="flex items-baseline gap-6">
              <code className="w-24 shrink-0 font-mono text-2xs text-ink-subtle">font-mono</code>
              <span className="w-16 shrink-0 font-mono text-2xs text-ink-subtle">14px</span>
              <span className="font-mono text-sm text-ink">60ae86f1e54f — 0O 1lI</span>
            </div>
          </div>
        </Section>

        <Section
          title="Spacing"
          note="A single 4px step. Everything else is a multiple of it, so components written months apart still line up."
        >
          <div className="flex flex-wrap items-end gap-6">
            {[1, 2, 3, 4, 6, 8, 12, 16].map((step) => (
              <div key={step} className="flex flex-col items-center gap-2">
                <div
                  className="rounded-sm bg-accent"
                  style={{ width: step * 4, height: step * 4 }}
                />
                <code className="font-mono text-2xs text-ink-subtle">{step}</code>
                <span className="font-mono text-2xs text-ink-subtle tabular">{step * 4}px</span>
              </div>
            ))}
          </div>
        </Section>

        <Section
          title="Surfaces"
          note="The ramp climbs steadily: each surface sits one step above the one it rests on. Six swatches, and all six have to be distinguishable — in the light theme two of them were the same white, under this very sentence."
        >
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {SURFACE_TOKENS.map((token) => (
              <Swatch key={token} token={token} className="h-16 border border-line" />
            ))}
          </div>
        </Section>

        <Section
          title="Text and accent"
          note="The hover and the press of a filled button are tokens, not a brightness filter: a filter knows nothing about which theme it is in, and the two most important buttons on a screen answered the same gesture in opposite directions because of it. These are the three values, side by side, which is the only form of them a screenshot can catch."
        >
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
            {['ink', 'ink-muted', 'ink-subtle', 'line', 'line-strong'].map((token) => (
              <Swatch key={token} token={token} className="h-16 border border-line" />
            ))}
            {['accent', 'accent-hover', 'accent-active'].map((token) => (
              <Swatch key={token} token={token} className="h-16" />
            ))}
          </div>
          <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
            {['danger', 'danger-hover', 'danger-active'].map((token) => (
              <Swatch key={token} token={token} className="h-16" />
            ))}
            {['accent-soft', 'accent-ink'].map((token) => (
              <Swatch key={token} token={token} className="h-16 border border-line" />
            ))}
          </div>
        </Section>

        <Section
          title="Status"
          note="Every status has a solid color for text and icons, and a soft one for backgrounds. Writing status text on a background of the same intensity would be unreadable."
        >
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {(['success', 'warning', 'danger', 'info'] as const).map((tone) => (
              <div key={tone} className="flex flex-col gap-2">
                <Swatch token={tone} className="h-12" />
                <Swatch token={`${tone}-soft`} className="h-12" />
              </div>
            ))}
          </div>

          {/* The conflict pair, which is a status like the four above and was
              on no band of this page. Two sides of one hunk have to be told
              apart at a glance and neither may read as the winner, so they are
              a pair rather than a good colour and a bad one. */}
          <div className="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-4">
            {(['ours', 'theirs'] as const).map((side) => (
              <div key={side} className="flex flex-col gap-2">
                <Swatch token={side} className="h-12" />
                <Swatch token={`${side}-soft`} className="h-12" />
              </div>
            ))}
          </div>
        </Section>

        <Section
          title="File status"
          note="The names are git's own, so the colors can be learned once."
        >
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {['added', 'modified', 'deleted', 'renamed', 'untracked', 'conflicted'].map((token) => (
              <Swatch key={token} token={token} className="h-12" />
            ))}
          </div>
        </Section>

        <Section
          title="Graph lanes"
          note={`${LANE_COLOR_COUNT} colors at identical lightness and chroma in OKLCH, so no branch looks more important than another. Their order is chosen, not sorted: neighboring lanes are at least 75° of hue apart.`}
        >
          <div className="grid grid-cols-5 gap-3 lg:grid-cols-10">
            {Array.from({ length: LANE_COLOR_COUNT }, (_, index) => (
              <Swatch
                key={index}
                token={`lane-${index + 1}`}
                label={`${index + 1}`}
                className="h-12"
              />
            ))}
          </div>
          <div className="mt-5 rounded-lg border border-line bg-surface p-4">
            <LanePreview />
          </div>
        </Section>

        <Section
          title="Author chips"
          note="The same ten lane tokens, read as ink on a wash of themselves rather than as a stroke on the canvas. They sit directly under the lanes because that is the harder of the two constraints: a hue light enough to draw a two-letter monogram is not automatically a hue a 2px curve stays visible at, and the palette has to satisfy both."
        >
          <div className="flex flex-wrap items-center gap-6">
            <Labelled label={`alone · ${AVATAR_SIZE}px`}>
              <div className="flex items-center gap-2">
                {SAMPLE_AUTHORS.map((name) => (
                  <Avatar key={name} name={name} />
                ))}
              </div>
            </Labelled>

            {/* The pairing the commit row and the commit pane both use. The
                chip is visible text, so beside a written-out name it joined the
                accessible name of the row: "AL feat: the second lane". Hidden
                where the fact is already written, announced where it is the
                only thing naming the author — which is the band on the left. */}
            <Labelled label="decorative · beside the name">
              <span className="flex items-center gap-2 text-2xs text-ink-subtle">
                <Avatar name="Grace Hopper" decorative />
                <span className="text-ink-muted">Grace Hopper</span>
              </span>
            </Labelled>
          </div>
        </Section>

        <Section title="Radius and elevation">
          <div className="flex flex-wrap gap-8">
            <div className="flex flex-wrap items-end gap-4">
              {RADIUS_STEPS.map((step) => (
                <div key={step.name} className="flex flex-col items-center gap-2">
                  <div
                    className={cx('size-14 border border-line-strong bg-surface', step.className)}
                  />
                  <code className="font-mono text-2xs text-ink-subtle">{step.name}</code>
                </div>
              ))}
            </div>

            <div className="flex flex-wrap items-end gap-4">
              {SHADOW_STEPS.map((step) => (
                <div key={step.name} className="flex flex-col items-center gap-2">
                  <div className={cx('size-14 rounded-lg bg-raised', step.className)} />
                  <code className="font-mono text-2xs text-ink-subtle">{step.name}</code>
                </div>
              ))}
            </div>
          </div>
        </Section>

        <Section
          title="Motion"
          note="Three durations, one easing family. Hover a square to see its curve."
        >
          <div className="flex flex-wrap gap-4">
            {MOTION_STEPS.map((step) => (
              <div key={step.name} className="flex flex-col gap-2">
                <div className="w-56 overflow-hidden rounded-md border border-line bg-sunken p-2">
                  <div
                    className={cx(
                      'h-8 w-8 rounded-sm bg-accent transition-transform hover:translate-x-40',
                      step.className,
                    )}
                  />
                </div>
                <code className="font-mono text-2xs text-ink-subtle">
                  {step.name} · {step.value}
                </code>
              </div>
            ))}
          </div>
        </Section>

        <Section
          title="Buttons"
          note="One primary action per screen. Danger is reserved for what destroys work."
        >
          <div className="flex flex-col gap-4">
            {(['md', 'sm'] as const).map((size) => (
              <div key={size} className="flex flex-wrap items-center gap-3">
                <code className="w-8 font-mono text-2xs text-ink-subtle">{size}</code>
                <Button size={size} variant="primary">
                  Commit
                </Button>
                <Button size={size} variant="secondary">
                  Fetch
                </Button>
                <Button size={size} variant="ghost">
                  Cancel
                </Button>
                <Button size={size} variant="danger">
                  Discard changes
                </Button>
              </div>
            ))}
          </div>
        </Section>

        <Section
          title="Button states"
          note="Busy and refused are two states, not two examples of one. A busy button keeps full contrast, its label, its focus ring and its place in the tab order — it is there to be read while you wait, and a browser blurs whatever it disables, which used to eject a keyboard user to the top of the document for the length of a push. A refused button dims to 45% and drops pointer events. Hover and press are drawn from tokens by every variant, danger included; the pointer is the only instrument for them here, and the three-step ramps above are the same values held still."
        >
          {/* The grid is four columns of controls wide, so it scrolls inside
              itself rather than pushing a horizontal scrollbar under the whole
              page on a narrow window. */}
          <div className="flex flex-col gap-3 overflow-x-auto">
            <div className="flex items-center gap-3">
              <span className="w-20 shrink-0" />
              {BUTTON_STATE_COLUMNS.map((column) => (
                <code key={column} className="w-40 shrink-0 font-mono text-2xs text-ink-subtle">
                  {column}
                </code>
              ))}
            </div>

            {BUTTON_VARIANTS.map((variant) => (
              <div key={variant} className="flex items-center gap-3">
                <code className="w-20 shrink-0 font-mono text-2xs text-ink-subtle">{variant}</code>
                <span className="flex w-40 shrink-0">
                  <Button variant={variant}>{BUTTON_LABELS[variant]}</Button>
                </span>
                <span className="flex w-40 shrink-0">
                  <Button variant={variant} loading>
                    {BUTTON_LABELS[variant]}
                  </Button>
                </span>
                <span className="flex w-40 shrink-0">
                  <Button variant={variant} disabled>
                    {BUTTON_LABELS[variant]}
                  </Button>
                </span>
              </div>
            ))}
          </div>

          {/* A ghost button that is a toggle rather than an action, which the
              header's command-log control is and nothing on this page was. The
              pressed state is drawn as well as announced: the state used to
              reach assistive technology and nobody else, so a reader who opened
              the log, scrolled, and looked back at the header had no way to
              tell whether it was still open. */}
          <div className="mt-5 flex flex-wrap items-center gap-4">
            <Labelled label="aria-pressed">
              <Button
                variant="ghost"
                onClick={() => setLogPressed(!logPressed)}
                aria-pressed={logPressed}
                className={logPressed ? 'bg-selected text-ink' : undefined}
              >
                Command log
              </Button>
            </Labelled>

            <Labelled label="leading glyph">
              <Button variant="secondary" leading={<SunGlyph />}>
                Fetch all remotes
              </Button>
            </Labelled>
          </div>
        </Section>

        <Section
          title="Theme glyphs"
          note="Three, because the theme has three states and one of them is not a colour. A reader who is letting the machine decide is in a state neither the sun nor the moon can stand for, and drawing one of them anyway claims a choice nobody made. The workbench's control walks system → light → dark and names the state it is IN, not the one a press would bring."
        >
          <div className="flex flex-wrap items-center gap-6">
            {THEME_GLYPHS.map((entry) => (
              <Labelled key={entry.name} label={entry.name}>
                <span className="flex size-9 items-center justify-center rounded-md border border-line bg-surface text-ink-muted">
                  {entry.glyph}
                </span>
              </Labelled>
            ))}
          </div>
        </Section>

        <Section title="References and badges">
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center gap-2">
              <RefBadge kind="head" name="main" current />
              <RefBadge kind="branch" name="feature/lane-assignment" />
              <RefBadge kind="remote" name="origin/main" />
              <RefBadge kind="tag" name="v1.4.0" />
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Badge>3 files</Badge>
              <Badge tone="accent">ahead 2</Badge>
              <Badge tone="success">+128</Badge>
              <Badge tone="danger">−41</Badge>
              <Badge tone="warning">behind 5</Badge>
              <Badge tone="info">detached HEAD</Badge>
            </div>
          </div>
        </Section>

        <Section
          title="Keyboard keys"
          note="The modifier is asked of the browser, never assumed: every shortcut here is taken with either Command or Control, and only the hint was ever wrong — it named a key half the readers of this page do not have. A glyph gets the word beside it, because one screen reader announces ⌘ as “place of interest sign” and the next says nothing at all. A key whose label is already a word gets no second copy."
        >
          <div className="flex flex-wrap items-center gap-6 text-xs text-ink-muted">
            <span className="flex items-center gap-1.5">
              <Kbd label={modifier.name}>{modifier.label}</Kbd>
              <Kbd label="Enter">↵</Kbd>
              <span>commits the staged changes</span>
            </span>
            <span className="flex items-center gap-1.5">
              <Kbd label={modifier.name}>{modifier.label}</Kbd>
              <Kbd>S</Kbd>
              <span>saves the file being edited</span>
            </span>
            <span className="flex items-center gap-1.5">
              <Kbd>Tab</Kbd>
              <span>takes the suggested message</span>
            </span>
            {/* Both keys, and both announced. On the keyboards of one whole
                platform the key in the backspace position is labelled "delete"
                and reports `Backspace`, so promising only Delete documented a
                two-key chord and left the key under the reader's finger
                unmentioned — while it closed repositories without asking. */}
            <span className="flex items-center gap-1.5">
              <Kbd>Delete</Kbd>
              <Kbd>Backspace</Kbd>
              <span>close the focused tab</span>
            </span>
          </div>
        </Section>

        <Section
          title="Tooltip"
          note="Pure CSS: no measurement, no portal. What that costs is a bubble that stays in the normal flow, so it is clipped by the first scroll container above it — inside a panel body, a list or a diff, the answer is a native title, which the browser draws outside the page and nothing cuts. It is also the only thing a refused control can explain itself with: a disabled button drops pointer events, so the hover has to land on the span around it."
        >
          <div className="flex flex-wrap items-center gap-6">
            <Tooltip label="Fetch all remotes">
              <Button size="sm" variant="ghost">
                Hover me
              </Button>
            </Tooltip>

            <Tooltip label="main is already up to date with origin/main" align="end">
              <span className="inline-flex">
                <Button size="sm" variant="secondary" disabled>
                  Pull
                </Button>
              </span>
            </Tooltip>

            <Labelled label="spinner">
              <Spinner />
            </Labelled>
          </div>
        </Section>

        <Section
          title="File statuses"
          note="git's own letter, in the colour its kind of change owns. The letter is the meaning; the colour is only faster."
        >
          <div className="flex flex-wrap items-center gap-4">
            {SAMPLE_FILES.map((file) => (
              <span key={file.path} className="flex items-center gap-2">
                {/* Each sample on the side its change is on. Drawing a
                    staged addition against the work tree asks the mark for a
                    side that has no code, and it answers M — which is how
                    two of these six lost their colour. */}
                <FileStatusMark file={file} side={file.staged ? 'index' : 'work_tree'} />
                <span className="font-mono text-xs text-ink-muted">{file.path}</span>
              </span>
            ))}
          </div>
        </Section>

        <Section title="Fields">
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Branch name" placeholder="feature/…" defaultValue="feature/graph-lanes" />
            <Field
              label="Remote URL"
              placeholder="git@github.com:…"
              hint="Uses the SSH agent already running on your machine."
            />
            <Field
              label="Branch name"
              defaultValue="feature/lane assignment"
              error="Branch names cannot contain spaces."
            />
          </div>

          <MessageBox />
        </Section>

        <Section
          title="Select"
          note="One choice out of a list whose length the repository decides — the remote to publish to. Native, so the open list is the operating system's and every assistive technology already knows it."
        >
          <div className="grid gap-4 sm:grid-cols-3">
            <Select
              label="Remote"
              options={[
                { value: 'origin', label: 'origin' },
                { value: 'upstream', label: 'upstream' },
              ]}
              hint="Where the branch will be published."
            />
            <Select
              label="Remote"
              options={[{ value: 'origin', label: 'origin' }]}
              disabled
              defaultValue="origin"
            />
          </div>
        </Section>

        <Section
          title="Tabs"
          note="Open repositories. One level, never nested. The second line is the path, cut at the FRONT — two checkouts of one project differ in the directory above them and agree on everything after it, so trimming the tail takes exactly the characters that tell them apart. The whole path is on the hover, which is the only copy a bare repository has: its line says “bare” and names nothing."
        >
          {/* Narrow on purpose, and on bg-surface. The strip overflows past
              five or six repositories, and it did so with nothing on screen
              saying it had — the last tab cut mid-word against a hard edge,
              which reads as a rendering fault. The fade that answers that is
              drawn `from-surface`, so a container of any other colour turns it
              from a dissolve into a band. */}
          <div className="max-w-lg overflow-hidden rounded-lg border border-line bg-surface">
            <Tabs
              items={SAMPLE_TABS}
              activeId={activeTab}
              onSelect={setActiveTab}
              onClose={() => undefined}
            />
          </div>
        </Section>

        <Section
          title="Segmented control"
          note="One choice out of a few, all of them on screen. A radio group: the options exclude one another, and the arrows move between them. Not tabs — tabs open and close, and each holds a different object. A ghost button standing beside one is drawn here because that is the pairing that goes wrong: an unfilled control with a glyph in it reads as one more unselected segment."
        >
          {/* The two places it is actually used, rather than an invented pair.
              A design system that demonstrates a control for a feature the
              product does not have teaches the wrong thing twice. */}
          <div className="flex flex-wrap items-center gap-6">
            <SegmentedControl
              label="What to show of this repository"
              value={segment}
              onChange={setSegment}
              segments={[
                { value: 'history', label: 'History' },
                { value: 'changes', label: 'Changes', badge: <Badge tone="accent">3</Badge> },
              ]}
            />
            <SegmentedControl
              label="Refs the graph is drawn from"
              segments={[
                { value: 'head', label: 'Current branch' },
                { value: 'all', label: 'All references' },
              ]}
              value={scope}
              onChange={setScope}
            />
            <Button size="sm" variant="ghost" leading={<SearchGlyph />}>
              Search…
            </Button>
          </div>
        </Section>

        <Section
          title="Row grounds"
          note="Three grounds and the rule for them: hover lightens, and selected lightens further, so the two are never the same paint. A destructive label carries its colour only where the ground keeps it readable — --color-danger over --color-selected is 4.27:1 in the light theme, under the floor this project gates on, which is why a row's Discard is ink at rest and takes its tint with a ground of its own. Both lists are drawn, because the pairing that hid an invisible hover for a release was a hoverable row on bg-sunken, and that pairing was nowhere on this page."
        >
          <div className="grid gap-5 lg:grid-cols-2">
            <Labelled label="rows on bg-surface">
              <div className="overflow-hidden rounded-md border border-line bg-surface">
                {SAMPLE_ROWS.map((row) => (
                  <GroundRow key={row.path} path={row.path} selected={row.selected} />
                ))}
              </div>
            </Labelled>

            {/* OpenRepository's discovered list, which is the real instance:
                a sunken well inside a dialog, with rows that light up. */}
            <Labelled label="rows on bg-sunken">
              <ul className="flex flex-col gap-0.5 rounded-md border border-line bg-sunken p-1">
                {SAMPLE_DISCOVERED.map((entry) => (
                  <li key={entry.path} className="flex flex-col">
                    <button
                      type="button"
                      className={cx(
                        'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left',
                        'transition-colors transition-instant',
                        'hover:bg-hover focus-visible:focus-ring outline-none',
                      )}
                    >
                      <span className="min-w-0 flex-1 truncate text-sm font-medium text-ink">
                        {entry.name}
                      </span>
                      {entry.bare && <Badge>bare</Badge>}
                      <span className="max-w-48 truncate font-mono text-2xs text-ink-subtle">
                        {entry.path}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </Labelled>
          </div>
        </Section>

        <Section
          title="Row actions"
          note="What a row can do, past the one or two things that fit beside it. A menu button, and a menu in the browser's top layer — the lists these open over scroll inside their panels, and anything drawn in that flow is clipped by it."
        >
          {/* Two rows, because the states worth seeing are per item and per
              row: the plain action, the destructive one, and the one that is
              refused here and offered one row below. A single row would show
              a menu; it takes two to show a menu that means something. */}
          <div className="max-w-md overflow-hidden rounded-md border border-line bg-surface">
            <MenuRow name="main" sha="a2801ba" current />
            <MenuRow name="feature/lane-assignment" sha="4e9c0d1" />
            <MenuRow name="HEAD" sha="9f3ae70" detached />
          </div>
        </Section>

        <Section
          title="Commit rows"
          note="Four column tracks, drawn by the same three constants in every state a page can be in. The subject takes what is left over — it is the only column whose useful length has no bound — and the other three are fixed, because “when did this land” and “who has been working here” are answered by running an eye down a column. The skeleton reproduces those tracks rather than guessing at them: a track it got wrong is width the subject grows by and hands back the instant the page arrives, which is the sideways twitch a fixed row height exists to prevent. The failed page says so on the one row a reader is looking at, and the rest of it holds space."
        >
          <div className="flex flex-col gap-5">
            <Labelled label="wide">
              <HistoryPreview selected={selectedCommit} onSelect={setSelectedCommit} />
            </Labelled>

            {/* The same rows, narrow enough that the columns start giving
                ground. Which one yields first is a decision written in a
                flex-basis and invisible at any single width: the subject asks
                for more than it is guaranteed, because flexbox shares a
                shortfall in proportion to what each item asked for, and the
                column that asked for less would keep less. */}
            <Labelled label="narrow">
              <div className="max-w-xl">
                <HistoryPreview selected={selectedCommit} onSelect={setSelectedCommit} />
              </div>
            </Labelled>
          </div>
        </Section>

        <Section
          title="Capped text"
          note="Two things that go together whenever a panel holds writing somebody else did. A title is clamped to two lines beside siblings that must not shrink, so a subject nobody edited cannot push the sha off the row; a body is capped in height, and the moment a box scrolls it needs a tab stop, or the ninth line is reachable by pointer alone."
        >
          <div className="max-w-2xl rounded-lg border border-line bg-surface p-3">
            <div className="flex items-start gap-3">
              <p
                className="line-clamp-2 min-w-0 flex-1 text-sm font-medium text-ink"
                title={SAMPLE_LONG_SUBJECT}
              >
                {SAMPLE_LONG_SUBJECT}
              </p>
              <code className="shrink-0 font-mono text-2xs break-all text-ink-subtle">
                60ae86f1e54f
              </code>
              <Button size="sm" variant="ghost" className="shrink-0">
                Copy SHA
              </Button>
            </div>

            {/* group rather than region: the panel around this is already a
                named landmark, and nesting a second one inside it puts a
                scroll box into the list a screen reader offers as the parts of
                the screen. */}
            <pre
              tabIndex={0}
              role="group"
              aria-label="Commit message body"
              className="mt-3 max-h-32 overflow-auto font-sans text-xs whitespace-pre-wrap text-ink-muted outline-none focus-visible:focus-ring"
            >
              {SAMPLE_BODY}
            </pre>
          </div>
        </Section>

        <Section
          title="Git commands"
          note="yagit never runs a destructive command without showing it first. The same component renders it in the log and in the confirmation. The two arms of that dialog are different shapes on purpose: the red panel names what git takes away, and a confirmation whose consequence is bounded rather than lost says so in a sentence instead — reaching for the red panel over `git branch --unset-upstream` tells the reader something false about their own repository."
        >
          <div className="flex flex-col gap-4">
            <GitCommand command="git rebase --interactive --autostash origin/main" />
            <div className="flex flex-wrap gap-3">
              <Button variant="danger" onClick={() => setConfirmOpen('destructive')}>
                Open a destructive confirmation
              </Button>
              <Button variant="secondary" onClick={() => setConfirmOpen('bounded')}>
                Open a bounded confirmation
              </Button>
              <Button variant="secondary" onClick={() => setConfirmOpen('ordered')}>
                Open a two-command confirmation
              </Button>
            </div>
          </div>
        </Section>

        <Section
          title="Notifications"
          note="An error notification carries git's raw stderr. 'Something went wrong' is not a message. One control per toast and no more: a notification is read by somebody who was looking at something else, and a second choice on it is a second thing to read before they can go back."
        >
          <div className="flex flex-col gap-5">
            <Labelled label="tones">
              <div className="flex flex-col gap-3">
                <Toast tone="info" title="Fetching from origin" onDismiss={() => undefined} />
                <Toast
                  tone="success"
                  title="Pushed 3 commits to origin/main"
                  onDismiss={() => undefined}
                />
                <Toast
                  tone="warning"
                  title="main follows no branch on a remote"
                  detail="Publish it before pulling, or set an upstream from the reference menu."
                  onDismiss={() => undefined}
                />
                <Toast
                  tone="danger"
                  title="Push rejected"
                  detail={
                    '! [rejected]        main -> main (non-fast-forward)\n' +
                    "error: failed to push some refs to 'github.com:ngsanogo/yagit.git'"
                  }
                  action={
                    <Button size="sm" variant="secondary">
                      Pull and rebase
                    </Button>
                  }
                  onDismiss={() => undefined}
                />
              </div>
            </Labelled>

            {/* What the corner looks like at the cap the host imposes, with the
                newest card carrying a run of failures collapsed into it. Both
                halves are the same decision: a stack that grew with every
                identical failure would push its own dismiss buttons off the top
                of the window, over the header where Fetch, Pull and Push are. */}
            <Labelled label="the stack, at its cap">
              <div className="flex max-w-md flex-col gap-2 rounded-lg border border-dashed border-line p-3">
                <Toast
                  tone="success"
                  title="Stashed 4 files"
                  action={
                    <Button size="sm" variant="secondary">
                      Undo
                    </Button>
                  }
                  onDismiss={() => undefined}
                />
                <Toast
                  tone="danger"
                  title="Could not fetch from origin"
                  repeats={3}
                  detail="fatal: could not read Username for 'https://github.com': terminal prompts disabled"
                  onDismiss={() => undefined}
                />
              </div>
            </Labelled>
          </div>
        </Section>

        <Section
          title="Panel states"
          note="Every panel in the workbench waits, and every panel fails. Both belong to the design system rather than to each panel, because a failure that looks different from the failure beside it is a workbench where the reader has to learn which panels recover. `compact` is for the sidebar column, where the generous padding would be most of the panel — a variant rather than a padding passed in from outside, because both land on the element together and the stylesheet, not the caller, decides between them."
        >
          <div className="grid gap-4 lg:grid-cols-2">
            <Panel title="History" flush>
              <Centered>
                <Spinner label="Loading the history" />
              </Centered>
            </Panel>

            {/* compact, and with the way out. This is the shape a sidebar
                panel gets: a few rows tall, where the generous padding would be
                most of the panel and the failure it is reporting would be
                below the fold. */}
            <Panel title="References" flush>
              <QueryErrorState
                title="Could not read the references"
                error={SAMPLE_PLAIN_ERROR}
                compact
                retry={IDLE_RETRY}
              />
            </Panel>

            {/* The other half: a full-height panel, and a failure that came
                from git rather than from the daemon — so it carries the exact
                command, the exit code and the raw stderr. */}
            <Panel title="Stashes" flush>
              <QueryErrorState title="Could not read the stashes" error={SAMPLE_GIT_ERROR} />
            </Panel>

            <Panel title="Changes" flush>
              <EmptyState
                title="No file matches"
                description="Nothing in this repository's changes contains “parser”."
                action={<Button size="sm">Clear the filter</Button>}
              />
            </Panel>
          </div>
        </Section>

        <Section
          title="Panels"
          note="A titled panel is a named region, not a bare section, so “History”, “Commit” and “References” are places a screen reader can jump to. Its header is where a mixed action row lives — the plain action, the ones that open a dialog and end in an ellipsis, and the rest behind a menu. The menu is in the top layer, which is why it is the one control in that header a panel's own overflow cannot cut."
        >
          <div className="grid gap-4 lg:grid-cols-2">
            <CommandLogPanel executions={SAMPLE_EXECUTIONS} />

            <Panel
              title="Commit"
              actions={
                <>
                  <Button size="sm" variant="secondary">
                    Stage all
                  </Button>
                  <Button size="sm" variant="ghost">
                    Amend…
                  </Button>
                  <Button size="sm" variant="ghost">
                    Sign off…
                  </Button>
                  <Menu label="More actions for the commit message" items={COMMIT_MENU_ITEMS} />
                </>
              }
              flush
            >
              <EmptyState
                title="Nothing to commit"
                description="The work tree matches the last commit. Edit a file and it will appear here."
                action={<Button size="sm">Open in editor</Button>}
              />
            </Panel>

            <Panel title="Submodules" flush>
              {/* The same state at the padding a few-row panel can afford. Both
                  are drawn because the difference is only visible as a pair. */}
              <EmptyState title="No submodules" description="This repository has none." compact />
            </Panel>

            <Panel title="Worktrees" flush>
              <EmptyState title="No linked worktrees" compact />
            </Panel>
          </div>
        </Section>

        <Section
          title="Diff lines"
          note="Two backgrounds on every line and they own different things: the row carries the STATE — hovered, chosen — and the span inside it carries the KIND. One element with both on it is not a choice at all but a question put to the cascade, and the cascade answered it wrong: a chosen added line painted as an ordinary added one. Layered, a chosen line keeps its green under an accent wash. Hover a line to see the first layer and click one to see the second; the marks inside a line are the third, drawn only where they carry information — whitespace that differs from the line it replaced, and trailing whitespace, which is invisible by construction. Scroll it for the sticky hunk header, and for the two buttons on it, which sit at 80% until the hunk is under the pointer: zero opacity was a feature nothing on screen mentioned, and the moment they are drawn at all the contrast floor is what decides how faint they may go."
        >
          {/* Shorter than the patch on purpose, so the pane scrolls: the hunk
              header is sticky over bg-sunken, and a box tall enough to hold
              everything never shows what that is for. */}
          <div className="h-72 overflow-hidden rounded-lg border border-line bg-surface">
            <DiffView
              diff={SAMPLE_DIFF}
              side="unstaged"
              busy={false}
              // Nothing behind this page stages anything, and `false` is what
              // says so: the selection stays where the reader put it rather
              // than being cleared as though a patch had gone to the daemon.
              onApply={() => false}
            />
          </div>
        </Section>
      </main>

      <ConfirmDialog
        open={confirmOpen === 'destructive'}
        onCancel={() => setConfirmOpen(null)}
        onConfirm={() => setConfirmOpen(null)}
        title="Reset main to origin/main?"
        command="git reset --hard origin/main"
        losing={['3 local commits', 'all uncommitted changes in 7 files']}
        confirmLabel="Reset --hard"
        destructive
      />

      {/* The other arm, and the reason both are on this page. Only the
          destructive one was ever drawn here, so the red panel became the
          shape anybody reached for — including over commands that destroy
          nothing. This one has a bounded consequence rather than a loss, and
          it says so in a sentence, with a primary confirm and no red box. */}
      <ConfirmDialog
        open={confirmOpen === 'bounded'}
        onCancel={() => setConfirmOpen(null)}
        onConfirm={() => setConfirmOpen(null)}
        title="Delete feature/lane-assignment?"
        description="Only the local branch is removed. origin/feature/lane-assignment stays where it is, and the commits stay reachable from it."
        command="git branch --delete feature/lane-assignment"
        confirmLabel="Delete branch"
      />

      {/* Two commands, because some operations really are two. The heading
          changes with the count — “yagit will run, in this order” — and that
          wording has had no reference on this page. */}
      <ConfirmDialog
        open={confirmOpen === 'ordered'}
        onCancel={() => setConfirmOpen(null)}
        onConfirm={() => setConfirmOpen(null)}
        title="Discard 5 selected changes?"
        command={[
          'git restore --worktree -- internal/graph/graph.go internal/git/status.go',
          'git clean --force -- notes.txt',
        ]}
        losing={['the edits in 2 tracked files', 'the untracked file notes.txt']}
        confirmLabel="Discard"
        destructive
      />
    </div>
  );
}

/**
 * Initial theme, read from the URL: `?theme=light`.
 *
 * Screenshots have to cover both themes without a human having to click
 * anything, otherwise the light theme's visual check never happens.
 */
const initialTheme: 'dark' | 'light' =
  new URLSearchParams(window.location.search).get('theme') === 'light' ? 'light' : 'dark';

/**
 * The modifier this reader's keyboard actually has.
 *
 * Read once at module scope, like the theme above: it is a fact about the
 * browser, not a value that changes while the page is open, and asking it per
 * render would be asking `navigator` on every keystroke in the fields below.
 */
const modifier = commandModifier();

/* ------------------------------------------------------------------------- *
 * Pieces internal to the showcase. They are not part of the design system:
 * they exist to present it, and are used on this page only.
 * ------------------------------------------------------------------------- */

/**
 * One reference — a name, its state, its short sha — with its actions behind a
 * menu.
 *
 * Three rows, three states of the same menu. The branch HEAD is on cannot be
 * deleted; git refuses it, so that item is refused rather than dropped — a
 * menu whose length depends on where HEAD is teaches nobody where the action
 * went. A detached HEAD is not a branch at all and has nothing to offer, so
 * its button is refused instead of vanishing: rows that lose a control stop
 * lining up with each other.
 */
function MenuRow({
  name,
  sha,
  current,
  detached,
}: {
  name: string;
  sha: string;
  current?: boolean;
  detached?: boolean;
}) {
  const actions =
    detached === true
      ? []
      : [
          { id: 'rename', label: 'Rename…', onSelect: () => undefined },
          menuItem(
            {
              id: 'delete',
              label: 'Delete…',
              danger: true,
              onSelect: () => undefined,
            },
            current === true ? `HEAD is on ${name}, so it cannot be deleted` : undefined,
          ),
        ];

  return (
    <div className="flex items-center gap-2 border-b border-line px-3 py-1.5 last:border-b-0">
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-ink-muted">{name}</span>
      {current === true && <RefBadge kind="head" name="HEAD" current />}
      {detached === true && <Badge tone="warning">detached</Badge>}
      <span className="font-mono text-2xs text-ink-subtle">{sha}</span>
      <Menu label={`More actions for ${name}`} items={actions} />
    </div>
  );
}

/**
 * A row of a list, on the ground its state gives it.
 *
 * Two actions, and they are not drawn the same way. The one that moves a file
 * between the lists is always there — a control that only exists under the
 * pointer is a feature nothing on screen mentions. The one that destroys work
 * waits for the row to be under attention, and it waits with `pointer-events`
 * as well as with opacity: transparent alone leaves a button nobody can see
 * and everybody can click, sitting in what reads as blank space on a row whose
 * own click means something else.
 */
function GroundRow({ path, selected }: { path: string; selected?: boolean }) {
  return (
    <div
      className={cx(
        'group/row flex items-center gap-2 border-b border-line px-3 py-1.5 last:border-b-0',
        'transition-colors transition-instant',
        selected === true ? 'bg-selected' : 'hover:bg-hover',
      )}
    >
      <span className="min-w-0 flex-1 truncate font-mono text-xs text-ink">{path}</span>
      <Button size="sm" variant="ghost">
        Stage
      </Button>
      <Button
        size="sm"
        variant="ghost"
        className={cx(
          'pointer-events-none opacity-0',
          'group-hover/row:pointer-events-auto group-hover/row:opacity-100',
          'group-focus-within/row:pointer-events-auto group-focus-within/row:opacity-100',
          // The tint arrives with a ground of its own rather than resting on
          // the row's. That is measured, not aesthetic: this ink over
          // --color-selected is under the floor, and over --color-danger-soft
          // it clears it in both themes.
          'hover:bg-danger-soft hover:text-danger',
        )}
      >
        Discard…
      </Button>
    </div>
  );
}

/**
 * The history's rows, in every state a page of them can be in.
 *
 * Rendered from the product's own row component rather than from a copy of it,
 * which is the whole point of the band: what has to be seen is that the four
 * tracks agree across the three states, and a copy would agree with itself
 * whatever the product did.
 *
 * One row is the list's single stop in the tab order and the arrows do the
 * rest — the same roving pattern the tablist and the menu use. Drawn twice at
 * two widths, so the two lists are two stops, which is what two lists are.
 */
function HistoryPreview({
  selected,
  onSelect,
}: {
  selected: string;
  onSelect: (sha: string) => void;
}) {
  return (
    <div className="overflow-hidden rounded-md border border-line bg-surface">
      {SAMPLE_COMMITS.map((commit, row) => (
        <div key={commit.sha} style={{ height: ROW_HEIGHT }}>
          <CommitRowView
            row={row}
            commit={commit}
            failure={undefined}
            reports={false}
            indent={undefined}
            tabbable={row === 0}
            selected={commit.sha === selected}
            onSelect={onSelect}
            onFocusRow={() => undefined}
          />
        </div>
      ))}

      <div style={{ height: ROW_HEIGHT }}>
        <CommitRowView
          row={SAMPLE_COMMITS.length}
          commit={undefined}
          failure={undefined}
          reports={false}
          indent={undefined}
          tabbable={false}
          selected={false}
          onSelect={onSelect}
          onFocusRow={() => undefined}
        />
      </div>

      <div style={{ height: ROW_HEIGHT }}>
        <CommitRowView
          row={SAMPLE_COMMITS.length + 1}
          commit={undefined}
          failure={SAMPLE_PAGE_FAILURE}
          reports
          indent={undefined}
          tabbable={false}
          selected={false}
          onSelect={onSelect}
          onFocusRow={() => undefined}
        />
      </div>
    </div>
  );
}

/**
 * The commit message box, which is the one control on the commit screen the
 * design system does not draw.
 *
 * `resize-y` hands the grip to the browser, so its look belongs to the
 * platform and to no token here. That is the trade worth judging on this page
 * rather than in a review comment: three rows is where a message starts and
 * not where every message fits, and the alternative — a drag handle of this
 * project's own — is code and a hit target in exchange for a control the
 * platform already ships.
 */
function MessageBox() {
  const id = useId();

  return (
    <div className="flex max-w-lg flex-col gap-1.5">
      <label htmlFor={id} className="text-xs font-medium text-ink-muted">
        Commit message
      </label>
      <textarea
        id={id}
        rows={3}
        defaultValue={
          'feat(design): show the states no instrument can see\n\nThe page had no commit row on it at all.'
        }
        className={cx(
          'w-full resize-y rounded-md border border-line-strong bg-sunken px-2.5 py-2',
          'font-mono text-xs text-ink placeholder:text-ink-subtle',
          'transition-colors transition-instant outline-none',
          'focus-visible:focus-ring hover:border-ink-subtle',
        )}
      />
    </div>
  );
}

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-semibold text-ink">{title}</h2>
        {note !== undefined && <p className="max-w-3xl text-xs text-ink-muted">{note}</p>}
      </div>
      {children}
    </section>
  );
}

/** A group inside a section, under the same mono label a swatch carries. */
function Labelled({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <code className="font-mono text-2xs text-ink-subtle">{label}</code>
      {children}
    </div>
  );
}

function Swatch({
  token,
  label,
  className,
}: {
  token: string;
  label?: string;
  className?: string;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <div
        className={cx('flex items-end justify-end rounded-md p-1.5', className)}
        style={{ backgroundColor: `var(--color-${token})` }}
      >
        {label !== undefined && (
          <span className="font-mono text-2xs font-semibold text-canvas">{label}</span>
        )}
      </div>
      <code className="font-mono text-2xs text-ink-subtle">{token}</code>
    </div>
  );
}

/** The glyph on the button beside the history's filter. */
function SearchGlyph() {
  return (
    <svg width="12" height="12" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <circle cx="6" cy="6" r="4" stroke="currentColor" strokeWidth="1.3" />
      <path d="m9.2 9.2 3 3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
    </svg>
  );
}

/**
 * A preview of the lanes as the graph draws them: curves that split and
 * converge. The graph is SVG as well (docs/adr/0003), so this stands in for it
 * only to judge the palette on the shape that will carry it.
 */
function LanePreview() {
  return (
    <svg viewBox="0 0 320 96" className="h-24 w-full" role="img" aria-label="Graph lane colors">
      {Array.from({ length: 6 }, (_, index) => {
        const y = 12 + index * 14;
        return (
          <path
            key={index}
            d={`M0 ${y} C 90 ${y}, 110 48, 160 48 C 210 48, 230 ${y}, 320 ${y}`}
            fill="none"
            strokeWidth="2"
            stroke={`var(--color-lane-${index + 1})`}
          />
        );
      })}
      {[0, 160, 320].map((x) => (
        <circle
          key={x}
          cx={x === 0 ? 8 : x === 320 ? 312 : x}
          cy={x === 160 ? 48 : 12}
          r="3.5"
          fill="var(--color-lane-1)"
        />
      ))}
    </svg>
  );
}

const TYPE_STEPS = [
  { name: 'text-2xs', size: '11px', className: 'text-2xs' },
  { name: 'text-xs', size: '12.5px', className: 'text-xs' },
  { name: 'text-sm', size: '14px', className: 'text-sm' },
  { name: 'text-base', size: '16px', className: 'text-base' },
  { name: 'text-lg', size: '20px', className: 'text-lg' },
  { name: 'text-xl', size: '26px', className: 'text-xl' },
  { name: 'text-2xl', size: '34px', className: 'text-2xl' },
];

/*
 * The classes are spelled out in full rather than composed on the fly.
 * Tailwind scans the source text to know which utilities to emit:
 * `rounded-${radius}` looks like nothing it can recognize, so the class does
 * not exist at all in the shipped CSS.
 */
const RADIUS_STEPS = [
  { name: 'sm', className: 'rounded-sm' },
  { name: 'md', className: 'rounded-md' },
  { name: 'lg', className: 'rounded-lg' },
  { name: 'xl', className: 'rounded-xl' },
];

const SHADOW_STEPS = [
  { name: 'raised', className: 'shadow-raised' },
  { name: 'popover', className: 'shadow-popover' },
  { name: 'dialog', className: 'shadow-dialog' },
];

const SURFACE_TOKENS = ['canvas', 'sunken', 'surface', 'raised', 'hover', 'selected'];

const MOTION_STEPS = [
  { name: 'instant', value: '90ms', className: 'transition-instant' },
  { name: 'fast', value: '150ms', className: 'transition-fast' },
  { name: 'slow', value: '260ms', className: 'transition-slow' },
];

const BUTTON_VARIANTS = ['primary', 'secondary', 'ghost', 'danger'] as const;

/** The three states drawn per variant, in the order the grid puts them. */
const BUTTON_STATE_COLUMNS = ['rest', 'busy · aria-busy', 'refused · disabled'];

/**
 * One label per variant, and each one is the action that variant is for.
 *
 * "Commit" on a ghost button and "Cancel" on a danger one would be four
 * examples of a control rather than four uses of it, and the rule this page
 * teaches is which variant an action gets.
 */
const BUTTON_LABELS: Record<(typeof BUTTON_VARIANTS)[number], string> = {
  primary: 'Commit',
  secondary: 'Fetch',
  ghost: 'Cancel',
  danger: 'Discard',
};

const THEME_GLYPHS = [
  { name: 'system', glyph: <SystemGlyph /> },
  { name: 'light', glyph: <SunGlyph /> },
  { name: 'dark', glyph: <MoonGlyph /> },
];

/**
 * Names chosen to exercise the chip rather than to decorate the page.
 *
 * Two words, four words, and one that is a single lowercase word — that last
 * one is what `git config user.name` holds on plenty of machines, and it is
 * what breaks an initials function written for a forename and a surname.
 */
const SAMPLE_AUTHORS = [
  'Ada Lovelace',
  'Grace Brewster Murray Hopper',
  'Alan Turing',
  'Barbara Liskov',
  'Katherine Johnson',
  'ada',
];

/**
 * Repository tabs as the workbench builds them: a name, and the path shortened
 * from the front, with the whole of it on the hover.
 *
 * Enough of them to overflow the strip in the box they are drawn in, because
 * the fades at each edge only exist when there is something behind them, and a
 * reference that never overflows is a reference for a state the product spends
 * most of its life in.
 */
const SAMPLE_CHECKOUTS: { path: string; label: string; bare?: boolean }[] = [
  { path: '/home/me/work/yagit', label: 'yagit' },
  { path: '/home/me/work/api', label: 'api' },
  { path: '/home/me/spike/api', label: 'api' },
  { path: '/home/me/work/design-tokens', label: 'design-tokens' },
  { path: '/srv/mirrors/kernel.git', label: 'kernel', bare: true },
  { path: '/home/me/dotfiles', label: 'dotfiles' },
];

const SAMPLE_TABS = SAMPLE_CHECKOUTS.map((entry, index) => ({
  // The path is the identity in the product; here the index keeps the two
  // checkouts called "api" apart without pretending an id exists.
  id: index === 0 ? 'yagit' : `tab-${index}`,
  label: entry.label,
  detail: entry.bare === true ? 'bare' : shortenPath(entry.path),
  detailInFull: entry.path,
}));

const SAMPLE_ROWS: { path: string; selected?: boolean }[] = [
  { path: 'internal/graph/graph.go' },
  { path: 'web/src/design/Showcase.tsx', selected: true },
  { path: 'docs/adr/0033-a-ref-may-hold-a-comma.md' },
];

const SAMPLE_DISCOVERED: { name: string; path: string; bare: boolean }[] = [
  { name: 'yagit', path: '/home/me/work/yagit', bare: false },
  { name: 'design-tokens', path: '/home/me/work/design-tokens', bare: false },
  { name: 'kernel', path: '/srv/mirrors/kernel.git', bare: true },
];

const SAMPLE_LONG_SUBJECT =
  'refactor(history): give the row four fixed column tracks so the author, the date and the sha start at the same x whatever the subject does';

const SAMPLE_BODY = `The metadata line used to flow after the subject, which moved the date's
left edge by up to 152 pixels between two adjacent rows. The tabular figures
on that date were paid for and aligned nothing: tabular figures only line up
digits that already start at the same x.

Fixing the three trailing columns and letting the subject take what is left
is what makes a column scannable. The skeleton row reproduces the same three,
so nothing moves sideways as a page lands.`;

/**
 * The actions of a commit panel, which is where all three tones of a menu item
 * appear together.
 *
 * Ordinary, destructive, and refused-with-a-reason. Refused rather than
 * removed: the arrows still land on it and a screen reader still announces it,
 * because the only way to read a menu is to walk it — and "why can I not sign
 * this" is exactly the question the item is there to answer.
 */
const COMMIT_MENU_ITEMS: MenuItem[] = [
  { id: 'copy', label: 'Copy the message', onSelect: () => undefined },
  menuItem({
    id: 'clear',
    label: 'Discard the message…',
    danger: true,
    onSelect: () => undefined,
  }),
  menuItem(
    { id: 'sign', label: 'Sign this commit…', onSelect: () => undefined },
    'No signing key is configured for this repository',
  ),
];

/**
 * One file per kind of change, so every status colour is on the page at once.
 *
 * The set is not decorative: it is what the changes panel has to tell apart at
 * a glance, and six colours are only distinguishable side by side.
 */
const SAMPLE_FILES: FileStatus[] = [
  {
    path: 'internal/graph/graph.go',
    kind: 'ordinary',
    index: '.',
    work_tree: 'M',
    staged: false,
    unstaged: true,
  },
  {
    path: 'internal/git/status.go',
    kind: 'ordinary',
    index: 'A',
    work_tree: '.',
    staged: true,
    unstaged: false,
  },
  {
    path: 'docs/old-plan.md',
    kind: 'ordinary',
    index: '.',
    work_tree: 'D',
    staged: false,
    unstaged: true,
  },
  {
    path: 'web/src/app/Workbench.tsx',
    kind: 'renamed',
    old_path: 'web/src/app/Shell.tsx',
    index: 'R',
    work_tree: '.',
    staged: true,
    unstaged: false,
    score: 92,
  },
  {
    path: 'notes.txt',
    kind: 'untracked',
    index: '.',
    work_tree: '.',
    staged: false,
    unstaged: true,
  },
  {
    path: 'internal/api/server.go',
    kind: 'unmerged',
    index: 'U',
    work_tree: 'U',
    staged: false,
    unstaged: true,
    conflict: 'both modified',
  },
];

/**
 * Commits chosen for their subjects' lengths, not for their content.
 *
 * A one-word subject, a conventional-commit subject, and one long enough to
 * need the ellipsis: the column tracks are only provable against subjects that
 * disagree about how much room they want. One is a merge, because the "merge"
 * word beside a subject is the only channel a screen reader has for a fact the
 * graph draws as two lines leaving one dot.
 *
 * The dates are fixed rather than relative to now, so two screenshots of this
 * page taken a day apart differ in nothing but what changed.
 */
const SAMPLE_COMMITS: CommitRow[] = [
  {
    sha: '60ae86f1e54f2b7c0a9d3e5f8b1c4a7d0e2f6b93',
    parents: ['4e9c0d1aa3b25c6f7d8e9a0b1c2d3e4f5a6b7c8d'],
    author: 'Ada Lovelace',
    date: '2026-03-10T09:12:00Z',
    subject: 'feat(graph): assign lanes in the daemon so the browser draws only what it is told',
    refs: ['HEAD -> main', 'origin/main'],
    lane: 0,
  },
  {
    sha: '4e9c0d1aa3b25c6f7d8e9a0b1c2d3e4f5a6b7c8d',
    parents: [
      'a2801ba9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3',
      '9f3ae70b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f',
    ],
    author: 'Grace Brewster Murray Hopper',
    date: '2026-03-09T17:40:00Z',
    subject: 'Merge branch feature/lane-assignment',
    refs: [],
    lane: 0,
  },
  {
    sha: 'a2801ba9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3',
    parents: ['9f3ae70b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f'],
    author: 'Alan Turing',
    date: '2026-03-09T11:05:00Z',
    subject: 'docs: why',
    refs: ['tag: v1.4.0'],
    lane: 1,
  },
];

/** The row the band opens with selected, so the third ground is on screen. */
const SELECTED_SAMPLE_SHA = '4e9c0d1aa3b25c6f7d8e9a0b1c2d3e4f5a6b7c8d';

/**
 * A git failure shaped exactly as the daemon sends one: a sentence belonging
 * to the route that failed, with git's own account joined onto it by Go's ": ".
 *
 * Assembled from the parts rather than typed out twice, and that is not
 * fastidiousness — it is the only way this page draws what the product draws.
 * `errorSummary` subtracts git's restatement from the message so the block
 * underneath is not the sentence above it repeated, and it does that by
 * comparing the two strings. A hand-written message that ended one character
 * short of the restatement would fail the comparison and draw both, which is
 * the 406-pixel toast this project already shipped once — and the reference
 * page would be teaching that shape rather than catching it.
 */
function gitError(context: string, failure: GitFailure): ApiError {
  const said = failure.stderr.trim();
  const restatement = `${failure.command}: exit code ${failure.exit_code}: ${said}`;
  return new ApiError(500, `${context}: ${restatement}`, failure);
}

/** The failure a panel draws: git ran, git refused, and it said why. */
const SAMPLE_GIT_ERROR = gitError('reading the stash list', {
  command: 'git stash list --format=…',
  args: ['stash', 'list'],
  exit_code: 128,
  stderr: 'fatal: not a git repository: .git',
});

/** The other shape of failure: the daemon stopped, and git never ran. */
const SAMPLE_PLAIN_ERROR = new Error('The daemon stopped answering. Start it again with ./do up.');

/**
 * A page of the history that failed instead of arriving, and the way back to
 * it — one action for the whole page, on the one row the reader is looking at.
 */
const SAMPLE_PAGE_FAILURE: PageFailure = {
  first: SAMPLE_COMMITS.length + 1,
  count: 200,
  error: gitError('reading commits 400 to 600', {
    command: 'git log --skip=400 --max-count=200 --topo-order --pretty=format:…',
    args: ['log'],
    exit_code: 128,
    stderr: "fatal: bad revision 'refs/heads/gone'",
  }),
  retrying: false,
  retry: () => undefined,
};

/** A query that has failed and is not being asked again right now. */
const IDLE_RETRY = { refetch: () => Promise.resolve(), isFetching: false };

/**
 * A diff written for the three drawings that have no other home: an intra-line
 * changed run, an indent that changed from a tab to spaces, and a trailing
 * space.
 *
 * The pairing is inferred by the pane, and inferred narrowly — a run of
 * removed lines followed by a run of added ones of the SAME length is taken as
 * line-for-line replacement — so each pair here is one removed line against
 * one added line. The unmodified lines around them are the point: a tab glyph's
 * width only means something beside a line whose indent did not change.
 */
const SAMPLE_DIFF: FileDiff = {
  id: 'showcase-diff',
  path: 'internal/repo/repo.go',
  binary: false,
  added: false,
  removed: false,
  hunks: [
    {
      old_start: 12,
      old_lines: 6,
      new_start: 12,
      new_lines: 6,
      heading: 'func (r *Repository) Head()',
      lines: [
        {
          kind: 'context',
          text: 'func (r *Repository) Head() (string, error) {',
          index: 0,
          old_line: 12,
          new_line: 12,
          no_newline: false,
        },
        {
          kind: 'removed',
          text: '\tref, err := r.run("symbolic-ref", "HEAD")',
          index: 1,
          old_line: 13,
          new_line: 0,
          no_newline: false,
        },
        {
          kind: 'added',
          text: '    ref, err := r.run("symbolic-ref", "--quiet", "HEAD")',
          index: 2,
          old_line: 0,
          new_line: 13,
          no_newline: false,
        },
        {
          kind: 'context',
          text: '\tif err != nil {',
          index: 3,
          old_line: 14,
          new_line: 14,
          no_newline: false,
        },
        {
          kind: 'removed',
          text: '\t\treturn "", err  ',
          index: 4,
          old_line: 15,
          new_line: 0,
          no_newline: false,
        },
        {
          kind: 'added',
          text: '\t\treturn "", fmt.Errorf("head: %w", err)',
          index: 5,
          old_line: 0,
          new_line: 15,
          no_newline: false,
        },
        {
          kind: 'context',
          text: '\t}',
          index: 6,
          old_line: 16,
          new_line: 16,
          no_newline: false,
        },
      ],
    },
    {
      old_start: 40,
      old_lines: 7,
      new_start: 40,
      new_lines: 8,
      heading: 'func (r *Repository) Open()',
      lines: [
        {
          kind: 'context',
          text: '\t// The root boundary is checked once, here.',
          index: 7,
          old_line: 40,
          new_line: 40,
          no_newline: false,
        },
        {
          kind: 'context',
          text: '\tresolved, err := filepath.EvalSymlinks(path)',
          index: 8,
          old_line: 41,
          new_line: 41,
          no_newline: false,
        },
        {
          kind: 'removed',
          text: '\tif !strings.HasPrefix(resolved, root) {',
          index: 9,
          old_line: 42,
          new_line: 0,
          no_newline: false,
        },
        {
          kind: 'added',
          text: '\tif !within(root, resolved) {',
          index: 10,
          old_line: 0,
          new_line: 42,
          no_newline: false,
        },
        {
          kind: 'context',
          text: '\t\treturn nil, ErrOutsideRoot',
          index: 11,
          old_line: 43,
          new_line: 43,
          no_newline: false,
        },
        {
          kind: 'context',
          text: '\t}',
          index: 12,
          old_line: 44,
          new_line: 44,
          no_newline: false,
        },
        {
          kind: 'added',
          text: '',
          index: 13,
          old_line: 0,
          new_line: 45,
          no_newline: false,
        },
        {
          kind: 'context',
          text: '\treturn open(resolved)',
          index: 14,
          old_line: 45,
          new_line: 46,
          no_newline: true,
        },
      ],
    },
  ],
};

const SAMPLE_EXECUTIONS: GitExecution[] = [
  {
    id: '1',
    command: 'git for-each-ref --sort=version:refname --format=…',
    exit_code: 0,
    duration_ms: 4,
    stderr: '',
    started_at: '2026-03-10T12:00:00Z',
  },
  {
    id: '2',
    command: 'git log --all --topo-order --pretty=format:…',
    exit_code: 0,
    duration_ms: 61,
    stderr: '',
    started_at: '2026-03-10T12:00:00Z',
  },
  {
    id: '3',
    command: 'git checkout feature/lane-assignment',
    exit_code: 1,
    duration_ms: 12,
    stderr:
      'error: Your local changes to the following files would be overwritten by checkout:\n\tinternal/lanes/lanes.go\nPlease commit your changes or stash them before you switch branches.',
    started_at: '2026-03-10T12:00:01Z',
  },
];
