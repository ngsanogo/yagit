import { useState, type ReactNode } from 'react';

import { ApiError } from '../api/client';
import type { ConflictSide, FileStatus, Operation, WorkFile } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { EmptyState } from '../components/EmptyState';
import { Kbd } from '../components/Kbd';
import { SegmentedControl } from '../components/SegmentedControl';
import { Spinner } from '../components/Spinner';
import { cx } from '../lib/cx';
import { errorDescription, refusalHeading } from '../lib/errorDisplay';
import { commandModifier } from '../lib/platform';
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
  /**
   * `git checkout --ours|--theirs`, then `git add`: the whole file, one side.
   *
   * Called once the user has been shown those commands and has said yes —
   * see KeepWholeFileDialog. Everything above this line is a question; this
   * is the only thing on the pane that overwrites a file through git.
   *
   * `onSettled` is the convention `onSave` above already uses, and it is what
   * closes the confirmation: the pane does not own the mutation, so without a
   * signal back it has to guess from a flag that covers every other operation
   * in the panel as well. Called whether git accepted or refused — a box left
   * open over a failure is a box with no way out of it.
   */
  onKeepWholeFile: (path: string, side: ConflictSide, onSettled: () => void) => void;
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

  /**
   * The side somebody has asked for the whole file from, and whether git is
   * already working on it.
   *
   * `undefined` is the ordinary state: no question on screen. See
   * KeepWholeFileDialog for why there is a question at all.
   *
   * The box closes when git has answered, not when the button was pressed.
   * The discard flow found this first and its comment in ChangesView records
   * what the shorter version costs: a confirmation that dismisses itself on
   * the press hands its own `busy` flag a moment that never arrives, so the
   * spinner it draws and the Cancel it refuses are unreachable by
   * construction, and a checkout that runs a clean filter over a large file
   * reads as a click that did nothing until the list comes back.
   *
   * `running` goes on with the press because the press IS the request —
   * onKeepWholeFile calls `mutate` synchronously — and it goes off in the
   * settled callback that mutation hands back. Watching the pane's `busy`
   * instead was the earlier shape, and it was a guess in both directions: the
   * flag is true for every save and every stage in the panel as well, and a
   * flag that never rose at all would have left the box modal with nothing
   * able to dismiss it.
   */
  const [keeping, setKeeping] = useState<{ side: ConflictSide; running: boolean }>();

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
    // The dialog is a sibling of the editor rather than a child of it, and
    // the keydown handler below is why: a <dialog> stays where it is rendered
    // in the tree, so a Ctrl+S pressed on the confirmation would bubble out of
    // it and save the file behind the question being asked.
    <>
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
          onKeepWholeFile={(side) => setKeeping({ side, running: false })}
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

      <KeepWholeFileDialog
        side={keeping?.side}
        file={file}
        dirty={dirty}
        // Only while git is actually working. `busy` greys the box's own
        // Cancel and refuses Escape, and a box that did that before the
        // request left would be refusing on a promise it had not made yet.
        busy={keeping?.running === true}
        onCancel={() => setKeeping(undefined)}
        onConfirm={(side) => {
          setKeeping({ side, running: true });
          onKeepWholeFile(file.path, side, () => setKeeping(undefined));
        }}
      />
    </>
  );
}

/**
 * The question in front of the one operation on this pane that leaves the
 * browser.
 *
 * Taking a side per region rewrites the buffer and can be undone by taking
 * the other one; taking the WHOLE file runs git. What that overwrites is the
 * file in the work tree — the markers, and every edit already saved into it —
 * and then `git add` collapses the three index stages, after which
 * `git checkout -m` cannot put the conflict back. A resolution somebody typed
 * out and saved is at that point in no stage, no commit and no reflog: it is
 * the same loss `git restore` is, reached from a button that until now asked
 * nothing.
 *
 * So it asks, in the shape every other destructive step in the product asks
 * in — the exact commands above the answer, and what disappears named — and
 * the two buttons that open it are `danger`, because that is what they are.
 *
 * Exported because the same mutation is reached from a second place: the pane
 * ChangesView draws for a conflict with no file on disk offers "Record the
 * deletion", and that button runs this `git rm`. A confirmation that appeared
 * on one of the two routes into a command and not the other is a rule with an
 * exception rather than a rule. It stays in this file rather than moving to a
 * shared one because the two functions that compose its command lines live
 * here, with the test that pins them.
 *
 * `busy` is on for as long as git is working rather than going out with the
 * press: see the state that owns the question in FileEditor for what a box
 * that dismisses itself on the answer costs.
 */
export function KeepWholeFileDialog({
  side,
  file,
  dirty = false,
  busy,
  onCancel,
  onConfirm,
}: {
  side: ConflictSide | undefined;
  file: FileStatus;
  /**
   * Whether there are unsaved edits to name among the losses.
   *
   * Defaulted, for the caller that has no editor at all: the pane for a
   * conflict with nothing on disk has no buffer to be dirty.
   */
  dirty?: boolean;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (side: ConflictSide) => void;
}) {
  if (side === undefined) {
    return null;
  }

  // The side being taken may be the side with no file on it, and then this is
  // not a checkout at all — it is `git rm`, and a dialog that said "git writes
  // the theirs version over this file" would be describing an operation that
  // is not the one about to run.
  const hasFile = sideHasContent(file.conflict, side);

  /*
   * Neither side has one, which is "both deleted" and no other pair.
   *
   * `git rm` there takes nothing out of the work tree, because there is
   * nothing in it: both branches deleted the file and git left none of it
   * behind. So this question is not the destructive one — a loss list headed
   * "This will permanently discard" would be inventing a loss to justify its
   * own colour, and the reader who checked would find the promise empty. What
   * is left is the reason the dialog exists at all: the exact command, before
   * it runs.
   */
  if (!hasFile && !sideHasContent(file.conflict, side === 'ours' ? 'theirs' : 'ours')) {
    return (
      <ConfirmDialog
        open
        onCancel={onCancel}
        onConfirm={() => onConfirm(side)}
        title={`Record the deletion of ${file.path}?`}
        description={`git calls this conflict "${file.conflict ?? 'unmerged'}": both sides deleted the file, so neither has a version to check out and there is nothing of it in the work tree. Recording the deletion collapses the three index stages, which is what ends the conflict.`}
        command={keepWholeFileCommands(file.path, side, false)}
        confirmLabel="Record the deletion"
        busy={busy}
      />
    );
  }

  const losing: [string, ...string[]] = [
    hasFile
      ? `${file.path} as it stands in the work tree — the markers, and every edit already saved into it`
      : `${file.path} itself: the ${side} side of this conflict is the file not existing`,
    ...(dirty ? ['the edits in this editor that have not been saved yet'] : []),
  ];

  return (
    <ConfirmDialog
      open
      destructive
      onCancel={onCancel}
      onConfirm={() => onConfirm(side)}
      title={hasFile ? `Take the whole file from ${side}?` : `Take ${side}, which has no file?`}
      description={
        hasFile
          ? `git writes the ${side} version over ${file.path} and stages it. Nothing here can bring back what is in the file now: once the three index stages are collapsed there is no version of the merge left to restore it from.`
          : `git calls this conflict "${file.conflict ?? 'unmerged'}", so the ${side} side has nothing to check out. Recording the deletion is what taking that side means, and it stages the removal as well.`
      }
      command={keepWholeFileCommands(file.path, side, hasFile)}
      losing={losing}
      confirmLabel={hasFile ? `Take ${side}` : 'Record the deletion'}
      busy={busy}
    />
  );
}

/**
 * The commands taking one side whole will run, spelled the way the log panel
 * will spell them.
 *
 * Composed here, which this project otherwise refuses to do: a command line
 * holding a path holds user input, and the rule the discard flow follows is
 * that the daemon answers with what it will run — `discard/plan` exists for
 * exactly this reason. There is no `resolve/plan` to ask, and a confirmation
 * with no command is not a confirmation this product ships, so the two
 * argument lists are mirrored from git.KeepSideArgs and
 * git.RemoveConflictedArgs and rendered by the rule git.CommandLine renders
 * by. It is a second definition of one thing and it should not outlive the
 * route that removes it.
 *
 * Which of the two runs is not a choice: it is what git's own status codes
 * say, and the daemon reads them the same way before it runs anything — see
 * sideHasContent, which the caller has already asked.
 *
 * Exported for the test beside this file, which is the only thing that holds
 * the copy to the original: it pins both shapes and the quoting, so that a
 * change made here has to be a change somebody meant to make. It cannot see
 * the Go, and no test can while the composition lives on this side.
 */
export function keepWholeFileCommands(
  path: string,
  side: ConflictSide,
  hasFile: boolean,
): [string, ...string[]] {
  const spec = quotedPathspec(path);
  if (!hasFile) {
    return [`git rm -- ${spec}`];
  }
  return [`git checkout --${side} -- ${spec}`, `git add -- ${spec}`];
}

/**
 * Whether the side being taken has a file at all.
 *
 * Not every conflict is two versions of one file. "Deleted by them" is our
 * side with content and their side without, and `git checkout --theirs` on it
 * fails with "does not have their version" — so taking theirs there means
 * removing the file, and the command that does it is `git rm`.
 *
 * Read off the words the daemon already sends rather than off the two status
 * codes beside them, so that the seven pairs are decoded once, in the place
 * that decodes them for everything else.
 *
 * Exported for the test beside this file, which walks the same seven pairs the
 * daemon's own test for HasSide walks.
 */
export function sideHasContent(conflict: string | undefined, side: ConflictSide): boolean {
  switch (conflict) {
    case 'both deleted':
      return false;
    case 'added by us':
    case 'deleted by them':
      return side === 'ours';
    case 'added by them':
    case 'deleted by us':
      return side === 'theirs';
    default:
      // Both modified, both added, and any pair a later git invents: two
      // sides that both have content, which is a checkout. The daemon's
      // answer for the unknown pair is the same one, and it is the answer
      // that fails loudly rather than running `git rm` on a file neither side
      // understood.
      return true;
  }
}

/**
 * A path as git will be given it, quoted as the log panel quotes it.
 *
 * `--` ends option parsing; it does not make what follows a name. Everything
 * after it is a pathspec, matched with wildmatch, so a file called `app/[id].tsx`
 * is a PATTERN by the time git reads it — `:(literal)` is git's own way to say
 * otherwise, and the daemon puts it on every path it sends.
 *
 * That prefix carries brackets, so the pathspec always holds a character a
 * shell would read and the daemon's unquoted branch is unreachable from here:
 * double quotes when the name holds an apostrophe and none of the four
 * characters a shell still reads inside them, single quotes otherwise, with
 * each apostrophe broken out. Two branches, and they are the whole of it.
 */
function quotedPathspec(path: string): string {
  const literal = `:(literal)${path}`;
  if (literal.includes("'") && !/["$`\\]/.test(literal)) {
    return `"${literal}"`;
  }
  return `'${literal.replaceAll("'", "'\\''")}'`;
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
  const modifier = commandModifier();
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
          {/* The key this reader has. The handler takes either modifier
              everywhere; only the hint was ever platform-specific, and it
              named a key most keyboards do not have. */}
          <span className="flex items-center gap-1 text-2xs text-ink-subtle">
            <Kbd label={modifier.name}>{modifier.label}</Kbd>
            <Kbd>S</Kbd>
          </span>
          <Button
            size="sm"
            variant="secondary"
            disabled={!dirty || busy}
            // Both modifiers, because the handler answers both. It is how the
            // shortcut reaches a reader who cannot see the keys drawn beside
            // the button.
            aria-keyshortcuts="Meta+S Control+S"
            onClick={onSave}
          >
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
              not is the view below.

              `danger`, and they ask before they run. The per-region buttons
              are reversible until the save; these overwrite the file and
              collapse the index stages, which is where a resolution somebody
              typed stops existing anywhere. See KeepWholeFileDialog. */}
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-2xs text-ink-muted">Take the whole file from</span>
              <Button
                size="sm"
                variant="danger"
                disabled={busy}
                onClick={() => onKeepWholeFile('ours')}
              >
                Ours
              </Button>
              <Button
                size="sm"
                variant="danger"
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

  return <EmptyState title="Could not open the file" detail={detail} />;
}
