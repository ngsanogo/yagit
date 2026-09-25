import type { ReactNode } from 'react';

import { cx } from '../lib/cx';
import { BranchIcon, RemoteIcon, TagIcon } from './Icons';

/**
 * Git ref badges.
 *
 * One kind, one color, always the same. HEAD carries the accent because
 * it is the only marker whose position moves under the user's feet, so it has
 * to be found instantly.
 */
export type RefKind = 'head' | 'branch' | 'remote' | 'tag';

const REF_CLASSES: Record<RefKind, string> = {
  head: 'text-ref-head border-ref-head/45 bg-ref-head/12',
  branch: 'text-ref-branch border-ref-branch/45 bg-ref-branch/12',
  remote: 'text-ref-remote border-ref-remote/40 bg-ref-remote/10',
  tag: 'text-ref-tag border-ref-tag/45 bg-ref-tag/12',
};

interface RefBadgeProps {
  kind: RefKind;
  name: string;
  /** Marks the ref HEAD currently sits on. */
  current?: boolean;
}

export function RefBadge({ kind, name, current = false }: RefBadgeProps) {
  return (
    <span
      className={cx(
        'inline-flex max-w-56 items-center gap-1 rounded-sm border px-1.5 py-px',
        'font-mono text-2xs leading-4 whitespace-nowrap',
        REF_CLASSES[kind],
        current && 'font-semibold',
      )}
      title={name}
    >
      {/* A glyph per kind, and the branch has one too. Three kinds carried a
          glyph and the fourth carried none, so a bare word in a badge meant
          "branch" by elimination — a rule the reader had to work out. */}
      {kind === 'remote' && <RemoteIcon size={10} />}
      {kind === 'tag' && <TagIcon size={10} />}
      {(kind === 'branch' || kind === 'head') && <BranchIcon size={10} />}
      <span className="truncate">{name}</span>
    </span>
  );
}

/**
 * Status badge, for everything that is not a git ref: the state of an
 * operation, the result of a command, the kind of a file.
 */
export type BadgeTone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info';

const TONE_CLASSES: Record<BadgeTone, string> = {
  neutral: 'bg-hover text-ink-muted',
  accent: 'bg-accent-soft text-accent',
  success: 'bg-success-soft text-success',
  warning: 'bg-warning-soft text-warning',
  danger: 'bg-danger-soft text-danger',
  info: 'bg-info-soft text-info',
};

interface BadgeProps {
  tone?: BadgeTone;
  children: ReactNode;
  className?: string;

  /**
   * A longer explanation, shown on hover.
   *
   * Never the whole message: a badge whose meaning is only in a tooltip is a
   * badge a keyboard user cannot read. It carries the detail behind a label
   * that already says what is wrong and what to do — the reason a watch was
   * refused, for instance, behind "Not refreshing on its own".
   */
  title?: string;
}

export function Badge({ tone = 'neutral', children, className, title }: BadgeProps) {
  return (
    <span
      title={title}
      className={cx(
        'inline-flex items-center gap-1 rounded-sm px-1.5 py-px',
        'text-2xs leading-4 font-medium whitespace-nowrap tabular',
        TONE_CLASSES[tone],
        className,
      )}
    >
      {children}
    </span>
  );
}
