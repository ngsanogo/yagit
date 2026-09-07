import { useState } from 'react';

import type { MessageSource, WorkingDirectory } from '../api/types';
import { Badge } from '../components/Badge';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { GitFailureDetail } from '../components/GitFailureDetail';
import { Kbd } from '../components/Kbd';
import { ApiError } from '../api/client';
import { cx } from '../lib/cx';
import { pluralize, shortenSha } from '../lib/format';
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
 *   is needed to reject it and exactly one is needed to take it.
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
  // placeholder and accepted with one key; see the note at the top.
  const suggestion = suggestCommitMessage(staged);

  const subject = message.split('\n', 1)[0] ?? '';

  // Amending with nothing staged is a reword, which is a perfectly good reason
  // to be here. A first commit needs something in it.
  const nothingToRecord = staged.length === 0 && !amend;

  // Named once, because the button and the shortcut are the same action and a
  // shortcut that sends what the button refuses is a request the user was told
  // was impossible — answered by git's own "no changes added to commit".
  const canCommit = !empty && !nothingToRecord && conflicted.length === 0;

  // The trailers are added on the way out, never into the box. Writing them
  // into the draft would put text under somebody's cursor that they did not
  // type and cannot easily undo — and unticking a co-author afterwards would
  // then have to find its own line again and remove it.
  const recorded = () => withCoAuthors(message, coAuthors);

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
    onCommit(recorded(), false, () => {
      onDraftChange(undefined);
      setCoAuthors([]);
    });
  }

  return (
    <div className="flex shrink-0 flex-col gap-2 border-t border-line p-3">
      <label className="sr-only" htmlFor="commit-message">
        Commit message
      </label>
      <textarea
        id="commit-message"
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
            return;
          }
          // Tab takes the suggestion, and only while the box is empty. Past
          // the first character Tab has to go on moving to the next control:
          // a text box that swallows Tab is one a keyboard user cannot leave.
          if (event.key === 'Tab' && !event.shiftKey && empty && suggestion !== '') {
            event.preventDefault();
            onDraftChange(suggestion);
          }
        }}
        placeholder={placeholderFor(amend, suggestion)}
        className={cx(
          'w-full resize-none rounded-md border border-line-strong bg-sunken px-2.5 py-2',
          'font-mono text-xs text-ink placeholder:text-ink-subtle',
          'transition-colors transition-instant outline-none',
          'focus-visible:focus-ring hover:border-ink-subtle',
        )}
      />

      {/* Only while it is being offered. A control that fills in a box which
          is no longer empty would overwrite what somebody has written. */}
      {empty && suggestion !== '' && (
        <p className="flex items-center gap-1.5 text-2xs text-ink-subtle">
          <Kbd>Tab</Kbd>
          <span>or</span>
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
          <span className="text-2xs text-ink-subtle">
            <Kbd>⌘</Kbd>
            <Kbd>↵</Kbd>
          </span>
          <Button variant="primary" size="sm" loading={busy} disabled={!canCommit} onClick={submit}>
            {commitLabel(status, staged.length, amend)}
          </Button>
        </span>
      </div>

      {/* Why the button is off, said out loud. A disabled control with no
          explanation is a dead end, and these three are the reasons it is
          ever disabled. */}
      {conflicted.length > 0 && (
        <p className="text-2xs text-conflicted">
          {pluralize(conflicted.length, 'file')} still conflicted. Resolve them and stage the result
          before committing.
        </p>
      )}
      {conflicted.length === 0 && nothingToRecord && (
        <p className="text-2xs text-ink-subtle">
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
          onCommit(recorded(), true, () => {
            onDraftChange(undefined);
            setCoAuthors([]);
          });
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
