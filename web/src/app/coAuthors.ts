/**
 * Co-authors, as the trailers git and every forge read.
 *
 * `Co-authored-by: Name <email>` is a convention rather than a git feature:
 * git stores the message verbatim and `git interpret-trailers` is what knows
 * where a trailer block begins. So the whole of this is where the lines go in
 * the message — which is exactly the part that is easy to get almost right,
 * and almost right means a forge silently attributing the commit to one
 * person.
 *
 * Kept out of the box that draws it so it can be asserted without a browser.
 */

/** One co-author, as typed. Neither field is trusted to be filled. */
export interface CoAuthor {
  name: string;
  email: string;
}

/** An empty row, which is what the box adds when somebody asks for one. */
export function emptyCoAuthor(): CoAuthor {
  return { name: '', email: '' };
}

/**
 * Whether a row says enough to become a trailer.
 *
 * Both fields, because a `Co-authored-by:` without an address is a line no
 * forge attributes to anybody — it reads as credit given and is not.
 */
export function isCompleteCoAuthor(author: CoAuthor): boolean {
  return author.name.trim() !== '' && author.email.trim() !== '';
}

/** The one line a co-author becomes. */
export function coAuthorTrailer(author: CoAuthor): string {
  return `Co-authored-by: ${author.name.trim()} <${author.email.trim()}>`;
}

/**
 * Whether a line is already a trailer — `Key: value`, git's own shape.
 *
 * It decides whether a co-author line joins the block at the end of the
 * message or starts one after a blank line. `git interpret-trailers` will not
 * read a trailer separated from the block by a blank line, so the difference
 * is between a co-author a forge sees and one it treats as prose.
 */
function isTrailerLine(line: string): boolean {
  return /^[A-Za-z][A-Za-z-]*:\s/.test(line);
}

/**
 * The message with a trailer for each complete co-author.
 *
 * Three things it has to get right:
 *
 *   Incomplete rows are dropped rather than half-written. The box leaves an
 *   empty row sitting there for the next name, and committing must not turn
 *   it into `Co-authored-by:  <>`.
 *
 *   A co-author already in the message is not added twice. The message
 *   survives a refused commit — a locked signing key, a hook that failed —
 *   and pressing the button again must not stack the trailers up.
 *
 *   A message that already ends in a trailer block gets the lines appended to
 *   it, with no blank line between. A blank line there would end the block,
 *   and everything after it is prose as far as git is concerned.
 */
export function withCoAuthors(message: string, authors: readonly CoAuthor[]): string {
  const trailers = authors
    .filter(isCompleteCoAuthor)
    .map(coAuthorTrailer)
    .filter((trailer, index, all) => all.indexOf(trailer) === index);

  if (trailers.length === 0) {
    return message;
  }

  const body = message.replace(/\s+$/, '');
  const existing = body.split('\n');
  const missing = trailers.filter((trailer) => !existing.includes(trailer));
  if (missing.length === 0) {
    return message;
  }

  // An empty message keeps its emptiness: trailers alone are not a commit
  // message, and the button that would send them is disabled anyway. Writing
  // them here would turn "nothing typed" into a subject line of credit.
  if (body === '') {
    return message;
  }

  const lastLine = existing[existing.length - 1] ?? '';
  const separator = isTrailerLine(lastLine) ? '\n' : '\n\n';
  return `${body}${separator}${missing.join('\n')}`;
}
