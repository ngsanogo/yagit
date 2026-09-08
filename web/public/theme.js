/*
 * The theme, stamped on <html> before the page is painted for the first time.
 *
 * Not part of the bundle, and not inline in index.html, for one reason each.
 * The bundle is a module: it is deferred by definition, so it cannot run
 * before a paint however early it is written, and React's own first paint is
 * already correct without it. An inline script would run early enough and is
 * refused by the daemon's `script-src 'self'` in the released binary while
 * working perfectly under `./do dev` — the silent divergence between the two
 * ways of serving this page that once cost a font. A classic script on a file
 * of its own is 'self', is render-blocking, and behaves the same both ways.
 *
 * This duplicates three facts from web/src/app/sessionStore.ts: the storage
 * key, that an absent value means "follow the machine", and that the query
 * asked is for light. That is a real second copy of a value the project
 * otherwise keeps in one place, and it is the price of running before any
 * module can. It is bounded — three constants, tested in sessionStore, and a
 * disagreement shows up as a flash of the WRONG theme rather than as nothing —
 * and the comment on readStoredTheme points here so the pair is edited
 * together.
 */
(function () {
  var LIGHT = 'light';
  var DARK = 'dark';

  var chosen = null;
  try {
    chosen = localStorage.getItem('yagit.theme');
  } catch {
    // A private context can refuse storage entirely. Following the machine is
    // the answer that needs nothing stored, which is what the absent case
    // already does — so there is nothing to report and nothing to recover.
  }

  if (chosen !== LIGHT && chosen !== DARK) {
    // Asked for light rather than for dark, so that a browser with no opinion
    // and one too old to answer both land on dark — the theme that has had the
    // proportional pass. sessionStore.ts makes the same choice for the same
    // reason.
    chosen =
      typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: light)').matches
        ? LIGHT
        : DARK;
  }

  document.documentElement.dataset.theme = chosen;
})();
