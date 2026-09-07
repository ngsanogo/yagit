import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';

import type { FileStatus, GitExecution } from '../api/types';
import { Avatar } from '../components/Avatar';
import { Badge, RefBadge } from '../components/Badge';
import { Button } from '../components/Button';
import { CommandLogPanel } from '../components/CommandLogPanel';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { EmptyState } from '../components/EmptyState';
import { Field } from '../components/Field';
import { FileStatusMark } from '../components/FileStatusMark';
import { GitCommand } from '../components/GitCommand';
import { Kbd } from '../components/Kbd';
import { Menu, menuItem } from '../components/Menu';
import { Panel } from '../components/Panel';
import { SegmentedControl } from '../components/SegmentedControl';
import { Select } from '../components/Select';
import { Spinner } from '../components/Spinner';
import { Tabs } from '../components/Tabs';
import { Toast } from '../components/Toast';
import { Tooltip } from '../components/Tooltip';
import { cx } from '../lib/cx';
import { LANE_COLOR_COUNT } from './tokens';
import { MoonGlyph, SunGlyph } from '../components/ThemeGlyphs';

/**
 * The design system showcase page.
 *
 * It exists for two reasons. First, because a design system drifts when
 * nobody ever sees it whole: every component appears here in all of its
 * states, so a regression stands out. Second, because it is the only place
 * the set can be judged together — a button on its own always looks fine;
 * it takes the full page to reveal the inconsistencies.
 */
export function Showcase() {
  const [theme, setTheme] = useState<'dark' | 'light'>(initialTheme);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [segment, setSegment] = useState<'history' | 'changes'>('changes');
  const [activeTab, setActiveTab] = useState('yagit');
  const [scope, setScope] = useState('head');

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
          note="The ramp climbs steadily: each surface sits one step above the one it rests on."
        >
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {SURFACE_TOKENS.map((token) => (
              <Swatch key={token} token={token} className="h-16 border border-line" />
            ))}
          </div>
        </Section>

        <Section title="Text and accent">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
            {['ink', 'ink-muted', 'ink-subtle', 'line', 'line-strong'].map((token) => (
              <Swatch key={token} token={token} className="h-16 border border-line" />
            ))}
            {['accent', 'accent-hover', 'accent-soft'].map((token) => (
              <Swatch key={token} token={token} className="h-16" />
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
                <Button size={size} variant="primary" loading>
                  Pushing
                </Button>
                <Button size={size} variant="secondary" disabled>
                  Nothing to pull
                </Button>
              </div>
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
            <div className="flex flex-wrap items-center gap-4">
              <Avatar name="Ada Lovelace" />
              <Avatar name="Grace Hopper" />
              <Avatar name="Alan Turing" />
              <Avatar name="Barbara Liskov" />
              <Avatar name="Katherine Johnson" />
              <span className="flex items-center gap-1.5 text-xs text-ink-muted">
                Press <Kbd>⌘</Kbd> <Kbd>K</Kbd> to search
              </span>
              <Tooltip label="Fetch all remotes">
                <Button size="sm" variant="ghost">
                  Hover me
                </Button>
              </Tooltip>
              <Spinner />
            </div>
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

        <Section title="Tabs" note="Open repositories. One level, never nested.">
          <div className="rounded-lg border border-line bg-canvas p-px">
            <Tabs
              items={[
                { id: 'yagit', label: 'yagit', detail: 'main' },
                { id: 'linux', label: 'linux', detail: 'v6.9-rc2' },
                { id: 'dotfiles', label: 'dotfiles', detail: 'detached' },
              ]}
              activeId={activeTab}
              onSelect={setActiveTab}
              onClose={() => undefined}
            />
          </div>
        </Section>

        <Section
          title="Segmented control"
          note="One choice out of a few, all of them on screen. A radio group: the options exclude one another, and the arrows move between them. Not tabs — tabs open and close, and each holds a different object."
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
          title="Git commands"
          note="yagit never runs a destructive command without showing it first. The same component renders it in the log and in the confirmation."
        >
          <div className="flex flex-col gap-4">
            <GitCommand command="git rebase --interactive --autostash origin/main" />
            <div>
              <Button variant="danger" onClick={() => setConfirmOpen(true)}>
                Open a destructive confirmation
              </Button>
            </div>
          </div>
        </Section>

        <Section
          title="Notifications"
          note="An error notification carries git's raw stderr. 'Something went wrong' is not a message."
        >
          <div className="flex flex-col gap-3">
            <Toast
              tone="success"
              title="Pushed 3 commits to origin/main"
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
                <>
                  <Button size="sm" variant="secondary">
                    Pull and rebase
                  </Button>
                  <Button size="sm" variant="ghost">
                    Show details
                  </Button>
                </>
              }
              onDismiss={() => undefined}
            />
          </div>
        </Section>

        <Section title="Panels">
          <div className="grid gap-4 lg:grid-cols-2">
            <CommandLogPanel executions={SAMPLE_EXECUTIONS} />
            <Panel
              title="Working directory"
              actions={
                <Button size="sm" variant="ghost">
                  Refresh
                </Button>
              }
              flush
            >
              <EmptyState
                title="Nothing to commit"
                description="Your working directory matches HEAD. Edit a file and it will show up here."
                action={<Button size="sm">Open in editor</Button>}
              />
            </Panel>
          </div>
        </Section>
      </main>

      <ConfirmDialog
        open={confirmOpen}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={() => setConfirmOpen(false)}
        title="Reset main to origin/main?"
        command="git reset --hard origin/main"
        losing={['3 local commits', 'all uncommitted changes in 7 files']}
        confirmLabel="Reset --hard"
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
