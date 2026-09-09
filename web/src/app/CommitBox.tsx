import { useEffect, useId, useRef, useState } from 'react';

import type { MessageSource, WorkingDirectory } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { GitFailureDetail } from '../components/GitFailureDetail';
import { Kbd } from '../components/Kbd';
import { useToast } from '../components/ToastHost';
import { ApiError } from '../api/client';
import { cx } from '../lib/cx';
import { gitFailureLine } from '../lib/errorDisplay';
import { pluralize, shortenSha } from '../lib/format';
import { commandModifier } from '../lib/platform';
import { emptyCoAuthor, isCompleteCoAuthor, withCoAuthors, type CoAuthor } from './coAuthors';
import { suggestCommitMessage } from './commitMessage';
import { usePreparedMessage } from './useWorkingDirectory';

/**
 * Where a commit is written.
 *
 * One text area, not two. git's own format is a subject, a blank line and a
 * body — one string, which is what reaches `git commit --file=-` — and a form
 * with separate fields would have to invent the blank line and then explain
 * why the message it produced is not the one that was typed.
 *
 * The commit runs the user's hooks, and the message keeps its '#' lines: in a
 * text box a '#' is text, and `--cleanup=whitespace` is what makes that true.
 *
 * The box is rarely empty when it opens, and there are two different reasons
 * for that which are deliberately not the same mechanism:
 *
 *   git's message goes IN the box, as real, editable text. After a merge that
 *   stopped on a conflict git has already written "Merge branch 'side'", and
 *   under --amend the commit being replaced already has a message. Both are
 *   things the user said themselves, once; asking them to say it again from
 *   memory is the interface losing their work.
 *
 *   yagit's suggestion stays a PLACEHOLDER until somebody accepts it. It is
 *   read off the staged file list — "Add src/parser.ts" — and it is a guess.
 *   A guess nobody read is not a commit message, so no keystroke of the user's
 *   is needed to reject it, and taking it is the button under the box: Tab
 *   reaches it, Enter presses it. It was one key — Tab, caught in the box —
 *   until that turned out to mean a box a keyboard user could not pass
 *   without writing a message they had not chosen. See the hint below.
 *
 * That line — git's words go in, ours stay behind the glass — is the whole
 * design of this box, and it is why the two never share a code path.
 */

/**
 * Where the subject stops being a subject.
 *
 * git imposes no limit; every tool that reads a log assumes one, and 50 is the
 * number they assume. Past it the counter appears — a warning, never a
 * refusal. Somebody with a good reason for a long subject is not wrong, and
 * this is not the place to argue with them.
 */
const SUBJECT_LIMIT = 50;

interface CommitBoxProps {
  repositoryId: string;
  status: WorkingDirectory;
  /**
   * What the user has typed, or nothing when they have not touched the box.
   *
   * Held by the repository rather than by this box: switching to the history
   * unmounts everything below it, and a draft that lived here would be
   * destroyed by the one round trip the switch exists to invite — write half a
   * message, read what the last commit said, come back.
   *
   * `undefined` rather than `''`, and the difference is what makes the
   * prepared message work. An empty string is a user who cleared the box and
   * meant it; nothing at all is a box nobody has been in yet, and only that
   * one may be filled in on their behalf.
   */
  draft: string | undefined;
  onDraftChange: (draft: string | undefined) => void;
  /**
   * Records the commit, and calls back once git has accepted it.
   *
   * The callback is what empties the box. Emptying it at the moment the
   * request leaves would throw the message away whenever git refuses — a hook
   * that fails, a signing key that is locked, an identity that is not
   * configured — and a message typed over several lines has no undo.
   */
  onCommit: (message: string, amend: boolean, onRecorded: () => void) => void;
  busy: boolean;
  error: Error | null;
}

export function CommitBox({
  repositoryId,
  status,
  draft,
  onDraftChange,
  onCommit,
  busy,
  error,
}: CommitBoxProps) {
  const [amend, setAmend] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const toast = useToast();

  // Ties the sentence that says why the button is off to the button itself.
  // Only one of the two is ever drawn, so one id covers both.
  const refusalId = useId();

  // Co-authors are kept beside the message rather than typed into it, because
  // the trailer's shape is exact and its position in the message decides
  // whether a forge reads it at all. See coAuthors.ts.
  //
  // Local to the box, unlike the draft: a name half-typed is not work worth
  // carrying across a view switch, and the message it would be appended to
  // has gone with the switch anyway.
  const [coAuthors, setCoAuthors] = useState<CoAuthor[]>([]);

  const staged = status.files.filter((file) => file.staged);
  const conflicted = status.files.filter((file) => file.kind === 'unmerged');

  // What git would open an editor on. Asked again when the answer changes —
  // the amend box is ticked, an operation starts or finishes — and never on a
  // timer: a box that reloaded under somebody mid-sentence would be worse than
  // one that never offered anything.
  const prepared = usePreparedMessage(repositoryId, amend, status.state);
  const preparedText = prepared.data?.text ?? '';

  const message = draft ?? preparedText;
  const empty = message.trim() === '';

  // Read off the staged files, never off their contents. Shown as the
  // placeholder and taken from the button under the box; see the note at the
  // top.
  const suggestion = suggestCommitMessage(staged);

  const subject = message.split('\n', 1)[0] ?? '';

  // Amending with nothing staged is a reword, which is a perfectly good reason
  // to be here. A first commit needs something in it.
  const nothingToRecord = staged.length === 0 && !amend;

  // Named once, because the button and the shortcut are the same action and a
  // shortcut that sends what the button refuses is a request the user was told
  // was impossible — answered by git's own "no changes added to commit".
  const canCommit = !empty && !nothingToRecord && conflicted.length === 0;

  // Of the three reasons the button is off, two have a sentence under it that
  // the button can point at. The third is an empty box, which says itself.
  const refused = conflicted.length > 0 || nothingToRecord;

  const modifier = commandModifier();

  // The trailers are added on the way out, never into the box. Writing them
  // into the draft would put text under somebody's cursor that they did not
  // type and cannot easily undo — and unticking a co-author afterwards would
  // then have to find its own line again and remove it.
  const recorded = () => withCoAuthors(message, coAuthors);

  const messageBox = useRef<HTMLTextAreaElement>(null);

  /**
   * Whoever held the keyboard when the commit landed, until the question of
   * where it should go can actually be answered. Null when there is nothing
   * outstanding.
   */
  const pressedOnCommit = useRef<Element | null>(null);

  /**
   * What a commit git accepted leaves behind — the empty box, and the focus.
   *
   * Called from the callback rather than from the click, because until git has
   * answered the message in the box is the only copy of it there is.
   *
   * The focus half is the part with no visible symptom. The button that was
   * pressed disables itself once the commit lands — nothing is staged any
   * more, which is the third of the three reasons it goes off — and a browser
   * blurs an element it disables, so focus falls to <body> and the next Tab
   * starts again at the top of the document. A control that vanishes or
   * refuses itself on success cannot report anything on itself; what it can do
   * is hand the keyboard somewhere deliberate, and the box the next message
   * gets typed into is where somebody who just committed is going.
   *
   * Which is why the answer is not read HERE. This runs inside the mutation's
   * callback, before React has re-rendered anything: the button is still
   * enabled and still holds the caret, so a check for <body> at this instant
   * reports "somebody else has it" on every commit and the keyboard is never
   * handed anywhere. What is recorded is who had it; the effect below reads
   * where it ended up, after the render that refuses the button.
   */
  function afterCommit() {
    onDraftChange(undefined);
    setCoAuthors([]);
    pressedOnCommit.current = document.activeElement;
  }

  /*
   * Where the keyboard goes once the commit has taken it.
   *
   * After every render rather than off a dependency list: the button goes off
   * when the draft empties, or when the status comes back with nothing staged,
   * or not at all on an amend that left something there — and a list of those
   * causes is a list that will one day be missing the fourth. The ref makes
   * every other render free.
   *
   * Still on the control that was pressed, while git is still answering, means
   * the render that refuses it has not happened yet: there is nothing to
   * reclaim and nothing to give up on either. Anywhere else means somebody has
   * moved: on <body> the browser put it there by disabling the button, and on
   * anything else a pointer user has chosen it — pulling them back into a text
   * area a second later is the trap every autofocus falls into.
   *
   * `busy` is what ends the wait, and without it the wait had no end. Clicking
   * a button does not focus it on every platform: where it does not, the
   * keyboard never left the message box, the first test above was true on
   * every render afterwards, and the question was never answered or dropped.
   * A press outstanding for the rest of the session is a press that answers
   * the next time focus happens to fall to <body> — a dialog closing an hour
   * later, and the caret jumps into a commit message nobody was writing.
   */
  useEffect(() => {
    const pressed = pressedOnCommit.current;
    if (pressed === null) {
      return;
    }

    const active = document.activeElement;
    if (busy && active === pressed && active !== document.body) {
      return;
    }

    pressedOnCommit.current = null;
    if (active === null || active === document.body) {
      messageBox.current?.focus();
    }
  });

  /*
   * A refusal is said as well as drawn.
   *
   * The failure git answers with — a hook that exited non-zero, a signing key
   * nobody unlocked, an identity that is not configured — is drawn under the
   * box, where it belongs: the message is still in the box and the reason has
   * to stay on screen while it is fixed. Drawn is not the same as noticed. The
   * button that was pressed refuses itself the moment the request leaves, the
   * paragraph appears below the fold of a 245-pixel panel, and nothing tells
   * a reader who cannot see it that anything happened at all.
   *
   * Said through the host's live region rather than by giving the paragraph
   * role="alert", and ToastHost's own comment is the reason: a region that
   * arrives with its text already inside it is the case screen readers
   * disagree about, and that region has existed since the page loaded. What
   * is spoken is one line — git's own, through the same helper the log rows
   * use — because the stderr block under it is a screenful and an
   * announcement is a sentence.
   */
  useEffect(() => {
    if (error === null) {
      return;
    }
    // Two sentences, because two different things refuse a commit and only one
    // of them is git. A hook that exited non-zero comes back with git's command
    // and git's stderr; a daemon that has stopped, or a body over the cap,
    // never reached git at all — and "git refused the commit" said over one of
    // those sends the reader to look for a hook that never ran. Which of the
    // two it was is the same question the paragraph below answers by drawing
    // either the failure block or the bare message.
    const failure = gitFailureLine(error);
    if (failure === undefined) {
      toast.announce(`Could not commit. ${error.message}`);
      return;
    }
    toast.announce(`git refused the commit. ${failure}`);
  }, [error, toast]);

  function submit() {
    if (!canCommit || busy) {
      return;
    }
    // Amending rewrites a commit that already exists. It is recoverable
    // through the reflog and it is still not something to do by pressing
    // Enter, so it asks first and shows the command it will run.
    if (amend) {
      setConfirming(true);
      return;
    }
    onCommit(recorded(), false, afterCommit);
  }

  return (
    <div className="flex shrink-0 flex-col gap-2 border-t border-line p-3">
      <label className="sr-only" htmlFor="commit-message">
        Commit message
      </label>
      <textarea
        id="commit-message"
        ref={messageBox}
        rows={3}
        value={message}
        onChange={(event) => onDraftChange(event.target.value)}
        onKeyDown={(event) => {
          // The shortcut every message box has. Enter alone stays a newline:
          // a commit body is several lines, and a form that commits on Enter
          // makes the second one impossible to type.
          if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            submit();
          }
          // Tab is not caught here, and it used to be: while the box was
          // empty it took the suggestion instead of moving on. That is the
          // one case where catching it is worst — somebody tabbing THROUGH
          // the changes rather than composing in them cannot pass the box
          // without writing a message they did not choose, and clearing it
          // again costs a select-all. The button below is the keyboard path,
          // and it is one stop away.
        }}
        placeholder={placeholderFor(amend, suggestion)}
        // resize-y, so the box can be dragged taller. Three rows is where a
        // message starts, not where every message fits: a subject, a blank
        // line and a paragraph saying why is ten lines, and the panel is a
        // flex column whose file list is the flexible child — so the height
        // taken here comes out of the list above and nothing else moves. The
        // grip is the browser's rather than this design system's, which is
        // the price of the one control on the screen that costs no code.
        className={cx(
          'w-full resize-y rounded-md border border-line-strong bg-sunken px-2.5 py-2',
          'font-mono text-xs text-ink placeholder:text-ink-subtle',
          'transition-colors transition-instant outline-none',
          'focus-visible:focus-ring hover:border-ink-subtle',
        )}
      />

      {/* Only while it is being offered. A control that fills in a box which
          is no longer empty would overwrite what somebody has written.

          The two keys are a description of the page rather than a shortcut of
          this box's own: the button is the next thing after the text area, so
          Tab reaches it and Enter presses it. Saying so is what the box owes
          somebody who was told there was one key and now finds two. */}
      {empty && suggestion !== '' && (
        <p className="flex items-center gap-1.5 text-2xs text-ink-subtle">
          <Kbd>Tab</Kbd>
          <span>then</span>
          <Kbd label="Enter">↵</Kbd>
          <span>to</span>
          <Button
            size="sm"
            variant="ghost"
            className="h-5 px-1.5"
            onClick={() => onDraftChange(suggestion)}
          >
            use the suggested summary
          </Button>
        </p>
      )}

      {/* Whose words are in the box, when they are not the user's. An
          interface that silently fills a field is one the user has to test to
          understand. */}
      {draft === undefined && preparedText !== '' && prepared.data !== undefined && (
        <p className="text-2xs text-ink-subtle">{sourceNote(prepared.data.source)}</p>
      )}

      {/* Said before the button rather than after it. Signing is the step that
          fails once everything else has succeeded — a locked key, an agent
          that is not running, a smartcard nobody touched — and a box that
          never mentioned it turns that into "the button did not work".
          yagit reads `commit.gpgsign` and never writes it: whose key a commit
          carries is not this application's decision. */}
      {prepared.data?.signing === true && (
        <p className="text-2xs text-ink-subtle">
          This commit will be signed. Your key or agent may ask for something.
        </p>
      )}

      {/* And when the question itself failed. Said rather than swallowed: the
          commit may still be signed, so a box that quietly showed nothing
          would be promising the opposite of what it knows. The detail is
          git's own — the command, its exit code, its stderr — because the fix
          is in a config file and nothing else on screen names it. */}
      {prepared.data?.signing_unreadable !== undefined && (
        <p className="text-2xs text-warning">
          yagit could not tell whether this commit will be signed:{' '}
          {prepared.data.signing_unreadable}
        </p>
      )}

      <div className="flex items-center gap-2">
        <label className="flex cursor-pointer items-center gap-1.5 text-2xs text-ink-muted">
          <input
            type="checkbox"
            checked={amend}
            disabled={status.unborn}
            onChange={(event) => setAmend(event.target.checked)}
            className="accent-accent"
          />
          Amend the last commit
        </label>

        {subject.length > SUBJECT_LIMIT && (
          <Badge tone="warning">{subject.length} characters in the subject</Badge>
        )}

        <span className="ml-auto flex items-center gap-2">
          {/* The key the reader actually has. The handler takes either
              modifier on every platform; only the hint was ever wrong, and it
              was wrong for everybody not on an Apple keyboard. */}
          <span className="flex items-center gap-1 text-2xs text-ink-subtle">
            <Kbd label={modifier.name}>{modifier.label}</Kbd>
            <Kbd label="Enter">↵</Kbd>
          </span>
          <Button
            variant="primary"
            size="sm"
            loading={busy}
            disabled={!canCommit}
            // Both modifiers, because both fire it. aria-keyshortcuts is how
            // the shortcut reaches a reader who cannot see the two keys drawn
            // beside the button.
            aria-keyshortcuts="Meta+Enter Control+Enter"
            // The sentence saying why, tied to the control it is about. A
            // refused button whose explanation is a loose paragraph is a dead
            // end at the control, which is the trap Menu already refuses to
            // ship.
            aria-describedby={refused ? refusalId : undefined}
            onClick={submit}
          >
            {commitLabel(status, staged.length, amend)}
          </Button>
        </span>
      </div>

      {/* Why the button is off, said out loud. A disabled control with no
          explanation is a dead end, and these three are the reasons it is
          ever disabled. The id is what carries either sentence to the button
          as its description; an empty box is the third reason and needs no
          sentence, because the empty box is on screen saying it. */}
      {conflicted.length > 0 && (
        <p id={refusalId} className="text-2xs text-conflicted">
          {conflictRefusal(conflicted.length)}
        </p>
      )}
      {conflicted.length === 0 && nothingToRecord && (
        <p id={refusalId} className="text-2xs text-ink-subtle">
          Nothing is staged. Stage a change, or amend to reword the last commit.
        </p>
      )}

      <CoAuthors authors={coAuthors} onChange={setCoAuthors} />

      {error !== null && <CommitError error={error} />}

      <ConfirmDialog
        open={confirming}
        onCancel={() => setConfirming(false)}
        onConfirm={() => {
          setConfirming(false);
          onCommit(recorded(), true, afterCommit);
        }}
        title="Replace the last commit?"
        command="git commit --file=- --cleanup=whitespace --amend"
        confirmLabel="Amend"
        busy={busy}
      />
    </div>
  );
}

/**
 * Who else wrote this commit.
 *
 * Rows of a name and an address rather than a free-text field, because the
 * trailer's shape is exact: `Co-authored-by: Name <email>` is what a forge
 * reads, and a box people type the whole line into is a box people mistype the
 * whole line into. What they become, and where in the message, is coAuthors.ts.
 *
 * Nothing is drawn until somebody asks for it. Pair-programming credit is not
 * what most commits need, and two empty fields under every message box would
 * be two empty fields under every message box.
 */
function CoAuthors({
  authors,
  onChange,
}: {
  authors: CoAuthor[];
  onChange: (authors: CoAuthor[]) => void;
}) {
  const update = (at: number, changed: Partial<CoAuthor>) =>
    onChange(authors.map((author, index) => (index === at ? { ...author, ...changed } : author)));

  if (authors.length === 0) {
    return (
      <p>
        <Button
          size="sm"
          variant="ghost"
          className="h-5 px-1.5 text-2xs"
          onClick={() => onChange([emptyCoAuthor()])}
        >
          Add a co-author
        </Button>
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      {authors.map((author, index) => (
        // Keyed by position, which is the one case where that is right: a row
        // IS its position here — there is no identity to key on until both
        // fields are filled, and keying on the half-typed contents would
        // rebuild the input under the cursor on every keystroke.
        <div key={index} className="flex items-center gap-1.5">
          <input
            aria-label={`Co-author ${index + 1} name`}
            value={author.name}
            onChange={(event) => update(index, { name: event.target.value })}
            placeholder="Name"
            autoComplete="off"
            spellCheck={false}
            className={coAuthorFieldClass}
          />
          <input
            aria-label={`Co-author ${index + 1} email`}
            value={author.email}
            onChange={(event) => update(index, { email: event.target.value })}
            placeholder="email@example.com"
            autoComplete="off"
            spellCheck={false}
            className={coAuthorFieldClass}
          />
          <Button
            size="sm"
            variant="ghost"
            className="h-6 shrink-0 px-1.5 text-2xs"
            aria-label={`Remove co-author ${index + 1}`}
            onClick={() => onChange(authors.filter((_, at) => at !== index))}
          >
            Remove
          </Button>
        </div>
      ))}

      <p className="flex items-center gap-2 text-2xs text-ink-subtle">
        <Button
          size="sm"
          variant="ghost"
          className="h-5 px-1.5 text-2xs"
          onClick={() => onChange([...authors, emptyCoAuthor()])}
        >
          Add another
        </Button>
        {/* Said rather than enforced. A row that is not finished is dropped,
            and a person who leaves one behind while they look up an address
            should not have their commit refused for it. */}
        {authors.some((author) => !isCompleteCoAuthor(author)) && (
          <span>A row needs both a name and an address to become a trailer.</span>
        )}
      </p>
    </div>
  );
}

const coAuthorFieldClass = cx(
  'min-w-0 flex-1 rounded-sm border border-line-strong bg-sunken px-1.5 py-1',
  'text-2xs text-ink placeholder:text-ink-subtle',
  'transition-colors transition-instant outline-none',
  'focus-visible:focus-ring hover:border-ink-subtle',
);

/**
 * Why the button is off while something is still conflicted.
 *
 * The pronoun is branched, and a counting helper cannot do it: `pluralize`
 * agrees the noun it is given and knows nothing about the sentence after it,
 * so "1 file still conflicted. Resolve them" is what it produces on its own.
 * OperationBanner's refusal for `continue` branches the same word the same
 * way, and on a stopped merge the two sentences are read within a screen of
 * each other.
 */
function conflictRefusal(conflicted: number): string {
  const those = conflicted === 1 ? 'it' : 'them';
  return `${pluralize(conflicted, 'file')} still conflicted. Resolve ${those} and stage the result before committing.`;
}

/**
 * What an empty box says.
 *
 * The suggestion itself, when there is one, because a placeholder that shows
 * the actual words is a proposal somebody can judge — where "a summary of your
 * change" is a form label. The guidance is what stands in when there is
 * nothing staged to describe.
 */
function placeholderFor(amend: boolean, suggestion: string): string {
  if (suggestion !== '') {
    return suggestion;
  }
  return amend ? 'Reword the last commit…' : 'Summary, then a blank line, then why.';
}

function sourceNote(source: MessageSource): string {
  switch (source) {
    case 'head':
      return 'The message of the commit you are about to replace. Edit it or leave it.';
    case 'merge':
      return 'git wrote this message for the operation in progress. Edit it or leave it.';
    case 'squash':
      return 'git collected these messages from the commits being squashed.';
    case 'template':
      return 'Your commit.template. Edit it or leave it.';
    case '':
      return '';
  }
}

function commitLabel(status: WorkingDirectory, staged: number, amend: boolean): string {
  if (amend) {
    return 'Amend';
  }
  if (status.detached) {
    // Committing on a detached HEAD makes a commit no branch points at. The
    // button says where it is going rather than pretending there is a branch.
    return `Commit onto ${shortenSha(status.head_sha)}`;
  }
  if (status.branch === '') {
    return 'Commit';
  }
  return staged === 0 ? `Commit to ${status.branch}` : `Commit ${staged} to ${status.branch}`;
}

/**
 * A commit that git refused.
 *
 * Shown here rather than as a toast: the message is still in the box, the user
 * is about to change something and try again, and the reason has to stay on
 * screen while they do.
 */
function CommitError({ error }: { error: Error }) {
  if (error instanceof ApiError && error.git !== undefined) {
    return <GitFailureDetail failure={error.git} />;
  }
  return <p className="text-2xs text-danger">{error.message}</p>;
}
