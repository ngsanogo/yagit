import { useState, type ReactNode } from 'react';

import { ApiError } from '../api/client';
import type { ConflictSide, FileStatus, Operation, WorkFile } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { EmptyState } from '../components/EmptyState';
import { Kbd } from '../components/Kbd';
import { SegmentedControl } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { ConflictResolver } from './ConflictResolver';
import { conflictSideNotes, findConflicts, hasConflictMarkers } from './conflict';
import { useWorkFile } from './useWorkingDirectory';

/**
 * Editing a file, in the client, without leaving it.
 *
 * The pane exists for one moment: an operation has stopped, a file has markers
 * in it, and every other screen here can describe that file without being able
 * to fix it. Sending somebody to another editor to delete seven characters is
 * where a git client stops being one.
 *
 * It is a quick-fix pane and it says so — no syntax highlighting, no
 * completion, a two-megabyte cap. What it does have is the part that is
 * actually hard: the buffer it saves is the one that was read, checked by
 * fingerprint, so a checkout running in another window cannot be silently
 * overwritten by a save typed before it.
 *
 * One buffer, two ways to look at it. The guided view offers a choice per
 * conflict region; the text view is the same characters in a box. Switching
 * between them changes nothing — there is no second copy of the content, and
 * a choice taken in one is immediately what the other shows.
 *
 * Saving does not stage, and nothing here pretends otherwise. Disk and index
 * are different places on every other screen in this panel, and an editor that
 * quietly staged would be deciding which of the two the user meant.
 */

type EditorView = 'guided' | 'text';

interface FileEditorProps {
  repositoryId: string;
  file: FileStatus;
  busy: boolean;
  /**
   * What the repository is in the middle of. The notes beside Ours and Theirs
   * follow it — during a rebase git swaps the two, and a note that does not
   * would name the wrong side.
   */
  operation: Operation;
  /** Writes the buffer to disk. */
  onSave: (path: string, text: string, base: string, onSaved: () => void) => void;
  /** `git checkout --ours|--theirs`, then `git add`: the whole file, one side. */
  onKeepWholeFile: (path: string, side: ConflictSide) => void;
  /** `git add`, which is what marks a hand-resolved file resolved. */
  onStage: (path: string) => void;
}

export function FileEditor({
  repositoryId,
  file,
  busy,
  operation,
  onSave,
  onKeepWholeFile,
  onStage,
}: FileEditorProps) {
  const opened = useWorkFile(repositoryId, file.path);

  /**
   * What the user has typed, or nothing when they have not.
   *
   * `undefined` rather than a copy of the file's text, and the distinction is
   * load-bearing: it is what tells an untouched buffer from one edited back to
   * exactly what was on disk, and it is what lets a re-read replace the
   * content without having to guess whether anything would be lost.
   */
  const [draft, setDraft] = useState<string>();
  const [view, setView] = useState<EditorView>('guided');

  // The path can change under this component when another row is picked, and
  // a draft belongs to the file it was typed into. Keyed on the fingerprint as
  // well as the path, so a save or a reload also starts a fresh buffer.
  //
  // A NUL joins the two because it is the one byte a path cannot contain, so
  // no pair of files can share an identity. Written as the escape `\0` and
  // never as the byte itself: a raw NUL in the source makes git call this
  // whole module binary, and a 900-line component then arrives at review as
  // `Bin 14368 -> 15279 bytes` with `grep` skipping the file entirely.
  const identity = `${file.path}\0${opened.data?.fingerprint ?? ''}`;
  const [seen, setSeen] = useState(identity);
  if (seen !== identity) {
    setSeen(identity);
    setDraft(undefined);
  }

  if (opened.isPending) {
    return (
      <div className="grid h-full place-items-center">
        <Spinner label="Reading the file" />
      </div>
    );
  }

  if (opened.isError) {
    return <OpenFailure error={opened.error} conflicted={file.kind === 'unmerged'} />;
  }

  const content = opened.data;
  const text = draft ?? content.text;
  const dirty = draft !== undefined && draft !== content.text;
  const regions = findConflicts(text);
  // Not the same question as "are there regions". A half-deleted region has no
  // side to offer and is still a marker that would be committed, and it is the
  // one this button has to refuse.
  const markers = hasConflictMarkers(text);

  // The guided view is only a view of something. With no region left in the
  // buffer it has nothing to draw, so the text is what remains — and switching
  // for the user beats showing them an empty pane with a switch above it.
  const showing: EditorView = regions.length === 0 ? 'text' : view;

  function save() {
    if (!dirty || busy) {
      return;
    }
    onSave(file.path, text, content.fingerprint, () => setDraft(undefined));
  }

  return (
    <div
      className="flex h-full min-h-0 flex-col"
      onKeyDown={(event) => {
        // The shortcut every editor has, and the one the browser would
        // otherwise answer by offering to save the page — which is why the
        // default is prevented whether or not there is anything to write.
        if (event.key === 's' && (event.metaKey || event.ctrlKey)) {
          event.preventDefault();
          save();
        }
      }}
    >
      <Header
        content={content}
        dirty={dirty}
        regions={regions.length}
        conflicted={file.kind === 'unmerged'}
        markers={markers}
        busy={busy}
        view={showing}
        onView={setView}
        onSave={save}
        onRevert={() => setDraft(undefined)}
        onKeepWholeFile={(side) => onKeepWholeFile(file.path, side)}
        onStage={() => onStage(file.path)}
        operation={operation}
      />

      {content.mixed_eol && (
        <Notice tone="warning">
          This file uses both LF and CRLF line endings. Saving writes {content.eol.toUpperCase()}{' '}
          throughout, which changes lines you did not type in.
        </Notice>
      )}

      {showing === 'guided' ? (
        <ConflictResolver text={text} onChange={setDraft} busy={busy} operation={operation} />
      ) : (
        <TextView text={text} onChange={setDraft} />
      )}

      <Footer
        conflicted={file.kind === 'unmerged'}
        dirty={dirty}
        regions={regions.length}
        markers={markers}
      />
    </div>
  );
}

function Header({
  content,
  dirty,
  regions,
  conflicted,
  markers,
  busy,
  view,
  onView,
  onSave,
  onRevert,
  onKeepWholeFile,
  onStage,
  operation,
}: {
  content: WorkFile;
  dirty: boolean;
  regions: number;
  conflicted: boolean;
  markers: boolean;
  busy: boolean;
  view: EditorView;
  onView: (view: EditorView) => void;
  onSave: () => void;
  onRevert: () => void;
  onKeepWholeFile: (side: ConflictSide) => void;
  onStage: () => void;
  operation: Operation;
}) {
  const notes = conflictSideNotes(operation);
  return (
    <div className="flex shrink-0 flex-col gap-2 border-b border-line px-3 py-2">
      <div className="flex items-center gap-3">
        <span className="truncate font-mono text-xs text-ink" title={content.path}>
          {content.path}
        </span>
        {dirty && <Badge tone="warning">unsaved</Badge>}

        <span className="ml-auto flex shrink-0 items-center gap-2">
          {/* Only while there is something to choose between. A switch to a
              view with nothing in it is a control that does nothing. */}
          {regions > 0 && (
            <SegmentedControl
              label="How to show this file"
              value={view}
              onChange={onView}
              segments={[
                {
                  value: 'guided',
                  label: 'Conflicts',
                  badge: <Badge tone="danger">{regions}</Badge>,
                },
                { value: 'text', label: 'Text' },
              ]}
            />
          )}

          <Button size="sm" variant="ghost" disabled={!dirty || busy} onClick={onRevert}>
            Revert
          </Button>
          <span className="text-2xs text-ink-subtle">
            <Kbd>⌘</Kbd>
            <Kbd>S</Kbd>
          </span>
          <Button size="sm" variant="secondary" disabled={!dirty || busy} onClick={onSave}>
            Save
          </Button>
        </span>
      </div>

      {conflicted && (
        <div className="flex flex-wrap items-start gap-2">
          {/* The whole file, one side, through git — as against the per-region
              buttons inside the view, which only rewrite the buffer. Both are
              here because they answer different questions: a file where every
              region goes the same way is one command, and one where they do
              not is the view below. */}
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-2xs text-ink-muted">Take the whole file from</span>
              <Button
                size="sm"
                variant="secondary"
                disabled={busy}
                onClick={() => onKeepWholeFile('ours')}
              >
                Ours
              </Button>
              <Button
                size="sm"
                variant="secondary"
                disabled={busy}
                onClick={() => onKeepWholeFile('theirs')}
              >
                Theirs
              </Button>
            </div>
            {/* Drawn, not hovered. A tooltip is hidden until then, and during
                a rebase the sentence is the whole of what Ours means. */}
            <p className="text-2xs text-pretty text-ink-subtle">
              {notes.ours}; {notes.theirs}
            </p>
          </div>

          <span className="ml-auto flex items-center gap-2">
            {/* What actually ends a conflict: `git add` collapses the three
                index stages into one. Saving the file does not, which is why
                this is a separate button and not a step inside Save.

                Off while a marker is still in the buffer, and that is the one
                thing this pane exists to prevent: `git add` on a file full of
                `<<<<<<<` succeeds, and git will commit it without a word. The
                footer says which of the two reasons it is off for. */}
            <Button
              size="sm"
              variant="secondary"
              disabled={busy || dirty || markers}
              onClick={onStage}
            >
              Mark resolved
            </Button>
          </span>
        </div>
      )}
    </div>
  );
}

/**
 * The buffer as characters.
 *
 * A textarea, deliberately. It is the one editing surface every browser
 * already gets right — undo, selection, an input method, a screen reader, a
 * spell checker off — and a hand-rolled one would have to win all of those
 * back before it was even level. What it costs is syntax highlighting, in a
 * pane whose whole purpose is a change small enough not to need it.
 */
function TextView({ text, onChange }: { text: string; onChange: (text: string) => void }) {
  return (
    <div className="min-h-0 flex-1 p-2">
      <label className="sr-only" htmlFor="file-text">
        File content
      </label>
      <textarea
        id="file-text"
        value={text}
        onChange={(event) => onChange(event.target.value)}
        spellCheck={false}
        // Tab moves between controls, and it has to keep doing so: this is one
        // element on a page full of them, and trapping Tab inside a text box
        // is how a keyboard user gets stuck in it. A quick fix does not need
        // to indent.
        className={cx(
          'h-full w-full resize-none rounded-md border border-line-strong bg-sunken px-2.5 py-2',
          'font-mono text-xs whitespace-pre text-ink',
          'transition-colors transition-instant outline-none',
          'focus-visible:focus-ring hover:border-ink-subtle',
        )}
      />
    </div>
  );
}

/**
 * What is left to do, said out loud.
 *
 * A conflicted file goes through three states and only one of them looks
 * finished from the outside: markers gone, saved, staged. Stopping after the
 * second leaves a work tree that reads as resolved and a repository that still
 * refuses to commit, so the pane says which one is still missing.
 */
function Footer({
  conflicted,
  dirty,
  regions,
  markers,
}: {
  conflicted: boolean;
  dirty: boolean;
  regions: number;
  markers: boolean;
}) {
  if (!conflicted) {
    return null;
  }

  if (regions > 0) {
    return (
      <Notice tone="muted">
        {regions === 1 ? '1 conflict left' : `${regions} conflicts left`}. Choose a side for each,
        or edit the text yourself.
      </Notice>
    );
  }

  if (markers) {
    // findConflicts offers no button for half a region, and staging it would
    // commit the marker. Saying so is the whole of what can honestly be done.
    return (
      <Notice tone="warning">
        A conflict marker is still in this file, but not a complete one. Fix it in the text view
        before staging — staging now would commit the marker.
      </Notice>
    );
  }

  if (dirty) {
    return (
      <Notice tone="muted">
        Every conflict is resolved. Save the file, then mark it resolved.
      </Notice>
    );
  }

  return (
    <Notice tone="muted">
      Every conflict is resolved and saved. Mark it resolved to stage it.
    </Notice>
  );
}

const NOTICE_CLASSES = {
  warning: 'border-warning/40 bg-warning-soft text-ink',
  muted: 'border-line text-ink-muted',
} as const;

function Notice({ tone, children }: { tone: keyof typeof NOTICE_CLASSES; children: ReactNode }) {
  return (
    <p className={cx('shrink-0 border-t px-3 py-1.5 text-2xs', NOTICE_CLASSES[tone])}>{children}</p>
  );
}

/**
 * Why the file could not be opened.
 *
 * The daemon tells the cases apart and each one has a different thing to say.
 * A file over the cap is not broken and neither is a binary one — they are
 * files this pane is the wrong tool for, and the message says which tool is
 * the right one rather than apologising.
 */
function OpenFailure({ error, conflicted }: { error: Error; conflicted: boolean }) {
  const detail = errorDescription(error);
  const heading = refusalHeading(error);

  if (heading !== undefined) {
    return (
      <EmptyState
        title={heading}
        description="This pane holds a file small enough to fix by hand. Open it in your own editor, save, and yagit will see the change within a couple of seconds."
        detail={detail}
      />
    );
  }

  if (error instanceof ApiError && error.status === 409) {
    return (
      <EmptyState
        title="Not a text file"
        description={
          conflicted
            ? 'git could not merge it and this pane cannot show it. Take one side whole from the list, or resolve it in another tool.'
            : 'There is nothing here to edit as text.'
        }
        detail={detail}
      />
    );
  }

  return <EmptyState title="Could not open the file" description="" detail={detail} />;
}
