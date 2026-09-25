import type { SVGProps } from 'react';

/**
 * The interface's glyphs, drawn once each.
 *
 * Sixteen units across, a stroke and a half wide, round at every end: the
 * same pen for the branch in the sidebar, the arrow on Push and the plus on
 * every row. A set drawn by one hand is what lets a glyph carry meaning
 * instead of decoration — the reader learns "this shape is a branch" once and
 * meets it everywhere a branch is named.
 *
 * Every one is `aria-hidden`. A glyph never stands alone here: it sits beside
 * a word, or inside a control that names itself, and a screen reader that
 * heard "git branch icon, main" would be hearing the same fact twice, badly.
 * The control that puts one on its own — an icon-only button — owes it an
 * `aria-label`, and Button's IconButton is what enforces that.
 *
 * `currentColor` for the stroke, so a glyph takes the ink of whatever it is
 * in: muted on a quiet label, accent on a HEAD marker, danger on a discard.
 * No colour is ever decided here.
 */

export type IconProps = Omit<SVGProps<SVGSVGElement>, 'children'> & {
  /** Side length in pixels. Fourteen sits beside the small type step. */
  size?: number;
};

function Icon({ size = 14, className, ...rest }: IconProps & { children: React.ReactNode }) {
  const { children, ...attributes } = rest;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
      {...attributes}
    >
      {children}
    </svg>
  );
}

/* --------------------------------------------------------------------------
 * Git's own nouns
 * ------------------------------------------------------------------------ */

export function BranchIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="4.5" cy="3.25" r="1.75" />
      <circle cx="4.5" cy="12.75" r="1.75" />
      <circle cx="11.5" cy="5" r="1.75" />
      <path d="M4.5 5v6M11.5 6.75c0 2.25-1.75 3.25-3.75 3.5-1.25.15-2.4.4-3.25 1.1" />
    </Icon>
  );
}

export function TagIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.25 2.25h4.9c.3 0 .55.1.75.3l5.6 5.6a1 1 0 0 1 0 1.4l-3.85 3.85a1 1 0 0 1-1.4 0l-5.6-5.6a1 1 0 0 1-.3-.7V2.25z" />
      <circle cx="5.5" cy="5.5" r="1" fill="currentColor" stroke="none" />
    </Icon>
  );
}

export function RemoteIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M5 12.75h6.75a2.75 2.75 0 0 0 .35-5.48A4 4 0 0 0 4.35 7.6 2.6 2.6 0 0 0 5 12.75z" />
    </Icon>
  );
}

export function CommitIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="8" cy="8" r="2.75" />
      <path d="M1.75 8h3.5M10.75 8h3.5" />
    </Icon>
  );
}

export function MergeIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="4.5" cy="3.25" r="1.75" />
      <circle cx="4.5" cy="12.75" r="1.75" />
      <circle cx="11.5" cy="9.5" r="1.75" />
      <path d="M4.5 5v6M4.5 5.25c.25 2.75 2.5 4.25 5.25 4.25" />
    </Icon>
  );
}

export function StashIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.25 3.25h11.5v3H2.25zM3 6.25v6.5c0 .4.35.75.75.75h8.5c.4 0 .75-.35.75-.75v-6.5M6.5 9.25h3" />
    </Icon>
  );
}

export function WorktreeIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4.25 12.75H3.25A1.25 1.25 0 0 1 2 11.5V5.75A1.25 1.25 0 0 1 3.25 4.5h1.9l1.25 1.25h2.35" />
      <path d="M6.5 7h1.9l1.25 1.25h3.1A1.25 1.25 0 0 1 14 9.5v3.25a1.25 1.25 0 0 1-1.25 1.25H6.5a1.25 1.25 0 0 1-1.25-1.25V8.25A1.25 1.25 0 0 1 6.5 7z" />
    </Icon>
  );
}

export function SubmoduleIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 1.75 13.5 4.9v6.2L8 14.25 2.5 11.1V4.9z" />
      <path d="M2.75 5 8 8l5.25-3M8 8v6" />
    </Icon>
  );
}

export function LargeFileIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <ellipse cx="8" cy="4" rx="5.5" ry="2.25" />
      <path d="M2.5 4v8c0 1.25 2.45 2.25 5.5 2.25s5.5-1 5.5-2.25V4M2.5 8c0 1.25 2.45 2.25 5.5 2.25S13.5 9.25 13.5 8" />
    </Icon>
  );
}

export function RepositoryIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2 4.5A1.5 1.5 0 0 1 3.5 3h2.6l1.4 1.5h5A1.5 1.5 0 0 1 14 6v5.5A1.5 1.5 0 0 1 12.5 13h-9A1.5 1.5 0 0 1 2 11.5z" />
    </Icon>
  );
}

export function FileIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 1.75h5.25L13 5.5v8.75H4z" />
      <path d="M9.25 1.75V5.5H13" />
    </Icon>
  );
}

export function ChangesIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 1.75h5.25L13 5.5v8.75H4z" />
      <path d="M9.25 1.75V5.5H13M6.75 9.75h3.5M8.5 8v3.5" />
    </Icon>
  );
}

export function HistoryIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.75 8a5.25 5.25 0 1 0 1.55-3.75" />
      <path d="M2.75 2.75v2.75h2.75M8 5.25V8.25l2.25 1.25" />
    </Icon>
  );
}

export function KeyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="5.5" cy="10.5" r="2.75" />
      <path d="M7.5 8.5 13.25 2.75M11 5l2 2M9.5 6.5l1.5 1.5" />
    </Icon>
  );
}

/* --------------------------------------------------------------------------
 * The network
 * ------------------------------------------------------------------------ */

export function FetchIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 2.25v7.25M5 6.5 8 9.5l3-3M2.5 11v1.5c0 .7.55 1.25 1.25 1.25h8.5c.7 0 1.25-.55 1.25-1.25V11" />
    </Icon>
  );
}

export function PullIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 2.25v11.5M3.75 9.5 8 13.75 12.25 9.5" />
    </Icon>
  );
}

export function PushIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 13.75V2.25M3.75 6.5 8 2.25 12.25 6.5" />
    </Icon>
  );
}

export function UndoIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.75 3.25v3.5h3.5" />
      <path d="M3.1 9.75a5.25 5.25 0 1 0 .4-4.4L2.75 6.75" />
    </Icon>
  );
}

/* --------------------------------------------------------------------------
 * Actions and states
 * ------------------------------------------------------------------------ */

export function PlusIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 3v10M3 8h10" />
    </Icon>
  );
}

export function MinusIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 8h10" />
    </Icon>
  );
}

export function CheckIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M3 8.5 6.25 11.75 13 5" />
    </Icon>
  );
}

export function CloseIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M4 4l8 8M12 4l-8 8" />
    </Icon>
  );
}

export function TrashIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.5 4h11M5.75 4V2.75h4.5V4M4.25 4l.6 8.75c0 .4.35.75.75.75h4.8c.4 0 .75-.35.75-.75L11.75 4M6.75 7v4M9.25 7v4" />
    </Icon>
  );
}

export function CopyIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
      <path d="M10.5 3.75v-.5A1.25 1.25 0 0 0 9.25 2h-6A1.25 1.25 0 0 0 2 3.25v6a1.25 1.25 0 0 0 1.25 1.25h.5" />
    </Icon>
  );
}

export function SearchIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="7" cy="7" r="4.25" />
      <path d="M10.25 10.25 13.5 13.5" />
    </Icon>
  );
}

export function FilterIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M2.25 3.75h11.5M4.5 8h7M6.75 12.25h2.5" />
    </Icon>
  );
}

export function PencilIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="m11.25 2.5 2.25 2.25L5.25 13H3v-2.25z" />
      <path d="m9.5 4.25 2.25 2.25" />
    </Icon>
  );
}

export function TerminalIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="m2.75 4.25 3.5 3.5-3.5 3.5M8.25 11.75h5" />
    </Icon>
  );
}

export function ChevronDownIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="m4 6.25 4 4 4-4" />
    </Icon>
  );
}

export function ChevronRightIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="m6.25 4 4 4-4 4" />
    </Icon>
  );
}

export function ArrowUpIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 13V3M4 7l4-4 4 4" />
    </Icon>
  );
}

export function ArrowDownIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M8 3v10M4 9l4 4 4-4" />
    </Icon>
  );
}

export function InfoIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <circle cx="8" cy="8" r="5.75" />
      <path d="M8 7.25v3.75M8 5.1v.05" />
    </Icon>
  );
}

export function WarningIcon(props: IconProps) {
  return (
    <Icon {...props}>
      <path d="M7.1 2.75 1.9 12a1 1 0 0 0 .9 1.5h10.4a1 1 0 0 0 .9-1.5L8.9 2.75a1 1 0 0 0-1.8 0z" />
      <path d="M8 6.25v3M8 11.25v.05" />
    </Icon>
  );
}

export function EllipsisIcon(props: IconProps) {
  return (
    <Icon {...props} fill="currentColor" stroke="none">
      <circle cx="3" cy="8" r="1.25" />
      <circle cx="8" cy="8" r="1.25" />
      <circle cx="13" cy="8" r="1.25" />
    </Icon>
  );
}

/**
 * Every glyph, by name, for the design system page.
 *
 * A registry rather than an index of the module, because the page has to draw
 * them under their names and a module has no names to read at runtime.
 */
export const ICONS = {
  BranchIcon,
  TagIcon,
  RemoteIcon,
  CommitIcon,
  MergeIcon,
  StashIcon,
  WorktreeIcon,
  SubmoduleIcon,
  LargeFileIcon,
  RepositoryIcon,
  FileIcon,
  ChangesIcon,
  HistoryIcon,
  KeyIcon,
  FetchIcon,
  PullIcon,
  PushIcon,
  UndoIcon,
  PlusIcon,
  MinusIcon,
  CheckIcon,
  CloseIcon,
  TrashIcon,
  CopyIcon,
  SearchIcon,
  FilterIcon,
  PencilIcon,
  TerminalIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ArrowUpIcon,
  ArrowDownIcon,
  InfoIcon,
  WarningIcon,
  EllipsisIcon,
} as const;
