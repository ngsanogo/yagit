import { describe, expect, it } from 'vitest';

import { take } from './driver-line.mjs';

describe('take', () => {
  it('splits a bare line into tokens and a remainder', () => {
    expect(take('click button', 1)).toEqual(['click', 'button']);
    expect(take('api GET /api/repos', 2)).toEqual(['api', 'GET', '/api/repos']);
  });

  it('always returns count + 1 entries, padding what the line omits', () => {
    expect(take('', 1)).toEqual(['', '']);
    expect(take('press', 2)).toEqual(['press', '', '']);
  });

  it('keeps a quoted token whole and drops its quotes', () => {
    expect(take('click \'role=button[name="Open repository"]\'', 2)).toEqual([
      'click',
      'role=button[name="Open repository"]',
      '',
    ]);
  });

  // The bug this file exists for. A JSON body and a snippet of JavaScript both
  // carry quotes that mean something; a parser that tokenized them would hand
  // JSON.parse and page.evaluate a mangled string.
  it('leaves the remainder exactly as it was written', () => {
    // As the driver reads it: the command name first, then the two tokens the
    // api command wants, then the body it must not touch.
    const [name, rest] = take('api POST /api/repos {"path": "/home/ada/work"}', 1);
    expect(name).toBe('api');
    expect(take(rest, 2)).toEqual(['POST', '/api/repos', '{"path": "/home/ada/work"}']);

    expect(take('eval document.title + " (built)"', 1)).toEqual([
      'eval',
      'document.title + " (built)"',
    ]);
  });

  // The value of a fill is the rest of the line, so a commit message keeps its
  // spaces, its colon and any quotes inside it.
  it('takes a selector as a token and the value as the remainder', () => {
    expect(take('\'role=textbox[name="Commit message"]\' feat: a line from the driver', 1)).toEqual(
      ['role=textbox[name="Commit message"]', 'feat: a line from the driver'],
    );
  });

  // A repository path is a value, not a token: it may hold spaces, and
  // truncating it at the first one would blame a directory nobody typed.
  it('keeps a path with spaces in the remainder', () => {
    expect(take('repo open /home/ada/My Repo', 2)).toEqual(['repo', 'open', '/home/ada/My Repo']);
  });

  it('trims the whitespace between the last token and the remainder', () => {
    expect(take('shot   the name', 1)).toEqual(['shot', 'the name']);
  });

  // A script written on Windows arrives with a carriage return on every line.
  it('does not carry a carriage return into a token', () => {
    expect(take('open\r', 1)).toEqual(['open', '']);
    expect(take('click button\r', 1)).toEqual(['click', 'button']);
  });

  it('refuses a quote that never closes', () => {
    expect(() => take("click 'role=button", 2)).toThrow(/unterminated ' quote/);
  });
});
