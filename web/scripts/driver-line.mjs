// How the driver reads one line of its command language.
//
// The rule is one sentence: the first few words of a line are tokens, and
// whatever follows them is the rest of the line, verbatim. Selectors are
// tokens — they need quoting because they contain spaces. Values are not: a
// commit message, a JSON body and a snippet of JavaScript all carry quotes of
// their own, and a parser that ate them would corrupt the payload of every
// command that matters while looking like it worked.
//
// It lives apart from the driver so it can be tested without a browser, which
// is worth a file of its own: this is where a driver silently types the wrong
// thing into the right box.

/**
 * Takes `count` tokens off the front of a line and returns them followed by
 * the untouched remainder.
 *
 * The result always has `count + 1` entries. Tokens that the line does not
 * supply come back as empty strings, so a caller can destructure without
 * checking the length and test the value it actually wanted.
 *
 * A token wrapped in single or double quotes runs to its closing quote and
 * loses the quotes; any other token runs to the next space and is taken as it
 * stands. There is no escape character: a role selector carries double quotes
 * of its own — `role=button[name="Open"]` — so a scheme that made the caller
 * escape them would be unusable for the commonest selector in this interface.
 * Wrap that one in single quotes instead.
 *
 * @param {string} line
 * @param {number} count
 * @returns {string[]} `count` tokens, then the rest of the line
 */
export function take(line, count) {
  const taken = [];
  let at = 0;

  while (taken.length < count) {
    while (at < line.length && isSpace(line[at])) at += 1;
    if (at >= line.length) break;

    const quote = line[at] === "'" || line[at] === '"' ? line[at] : '';
    if (quote === '') {
      let end = at;
      while (end < line.length && !isSpace(line[end])) end += 1;
      taken.push(line.slice(at, end));
      at = end;
      continue;
    }

    const closing = line.indexOf(quote, at + 1);
    // Silently treating an unterminated quote as running to the end of the
    // line is how a typo becomes a selector that matches nothing and an error
    // about the wrong thing.
    if (closing === -1) {
      throw new Error(`unterminated ${quote} quote in: ${line.slice(at)}`);
    }
    taken.push(line.slice(at + 1, closing));
    at = closing + 1;
  }

  while (taken.length < count) taken.push('');
  taken.push(line.slice(at).trim());
  return taken;
}

// \r counts: a script written on Windows, or pasted from one, arrives with a
// carriage return on the end of every line, and a token that quietly carried
// one would fail to match any command in the table.
function isSpace(character) {
  return character === ' ' || character === '\t' || character === '\r';
}
