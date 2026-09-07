import { describe, expect, it } from 'vitest';

import { coAuthorTrailer, isCompleteCoAuthor, withCoAuthors } from './coAuthors';

describe('a co-author becoming a trailer', () => {
  it('is the exact line a forge reads', () => {
    expect(coAuthorTrailer({ name: 'Ada Lovelace', email: 'ada@example.test' })).toBe(
      'Co-authored-by: Ada Lovelace <ada@example.test>',
    );
  });

  it('is trimmed, because a pasted address arrives with a space on it', () => {
    expect(coAuthorTrailer({ name: '  Ada  ', email: ' ada@example.test ' })).toBe(
      'Co-authored-by: Ada <ada@example.test>',
    );
  });

  it('needs both halves: credit without an address is credit nobody receives', () => {
    expect(isCompleteCoAuthor({ name: 'Ada', email: '' })).toBe(false);
    expect(isCompleteCoAuthor({ name: '', email: 'ada@example.test' })).toBe(false);
    expect(isCompleteCoAuthor({ name: ' ', email: ' ' })).toBe(false);
    expect(isCompleteCoAuthor({ name: 'Ada', email: 'ada@example.test' })).toBe(true);
  });
});

describe('putting the trailers in the message', () => {
  const ada = { name: 'Ada', email: 'ada@example.test' };
  const grace = { name: 'Grace', email: 'grace@example.test' };

  it('separates the block from prose with a blank line', () => {
    expect(withCoAuthors('Fix the cache\n\nIt was stale.', [ada])).toBe(
      'Fix the cache\n\nIt was stale.\n\nCo-authored-by: Ada <ada@example.test>',
    );
  });

  it('joins a block that is already there, with no blank line inside it', () => {
    // A blank line would end the block, and git reads nothing after it as a
    // trailer — the difference between credit a forge shows and prose.
    expect(withCoAuthors('Fix the cache\n\nSigned-off-by: Ada <ada@example.test>', [grace])).toBe(
      'Fix the cache\n\nSigned-off-by: Ada <ada@example.test>\n' +
        'Co-authored-by: Grace <grace@example.test>',
    );
  });

  it('drops the empty row the box leaves behind for the next name', () => {
    expect(withCoAuthors('Subject', [ada, { name: '', email: '' }])).toBe(
      'Subject\n\nCo-authored-by: Ada <ada@example.test>',
    );
  });

  it('does not stack a trailer up when a refused commit is sent again', () => {
    const once = withCoAuthors('Subject', [ada]);
    expect(withCoAuthors(once, [ada])).toBe(once);
  });

  it('adds the one that is missing without repeating the one that is not', () => {
    const once = withCoAuthors('Subject', [ada]);
    expect(withCoAuthors(once, [ada, grace])).toBe(
      `${once}\nCo-authored-by: Grace <grace@example.test>`,
    );
  });

  it('names the same person once however many rows say so', () => {
    expect(withCoAuthors('Subject', [ada, { name: ' Ada ', email: 'ada@example.test' }])).toBe(
      'Subject\n\nCo-authored-by: Ada <ada@example.test>',
    );
  });

  it('leaves a message with no complete co-author exactly as it was', () => {
    expect(withCoAuthors('Subject\n', [{ name: 'Ada', email: '' }])).toBe('Subject\n');
  });

  it('leaves an empty message empty: trailers alone are not a commit message', () => {
    expect(withCoAuthors('', [ada])).toBe('');
    expect(withCoAuthors('   \n', [ada])).toBe('   \n');
  });
});
