/**
 * Session preferences that outlive a reload.
 *
 * The daemon holds open repositories in memory only, so a restart empties the
 * list. Paths here are what the workbench re-opens on load — the same POST
 * /api/repos a person would make by hand. Theme and history scope are pure
 * UI and need no daemon help.
 *
 * Paths rather than opaque ids: an id dies with the daemon process that minted
 * it. A path under YAGIT_ROOT is what Open already takes.
 */

const PATHS_KEY = 'yagit.open-paths';
const ACTIVE_KEY = 'yagit.active-path';
const THEME_KEY = 'yagit.theme';
const SCOPE_KEY = 'yagit.history-scope';

/** What actually gets painted. `data-theme` is always one of these two. */
export type Theme = 'dark' | 'light';

/**
 * What the reader chose, which is not the same thing.
 *
 * The third state is the honest one and it was missing: a two-state toggle
 * defaulting to dark serves dark to somebody whose desktop has said "light"
 * for years, in every fresh profile and every private window, and offers no
 * way to say "whichever the machine is using" — only "dark, always" or
 * "light, always".
 *
 * `system` is stored as the ABSENCE of the key. A reader who never touched
 * the control follows their desktop, an older `dark` or `light` still means
 * what it meant, and there is no third string to keep two parsers agreeing on.
 */
export type ThemeChoice = Theme | 'system';

/**
 * Asked for light rather than for dark, so that everything else lands on dark.
 *
 * A browser with no opinion, and one too old to answer, both report `false`
 * here — and dark is the theme that has had the proportional pass, so it is
 * the right place for "I could not tell".
 */
const LIGHT_QUERY = '(prefers-color-scheme: light)';

/**
 * The colour preference the machine already holds.
 *
 * Guarded rather than assumed: this module is imported by tests that run under
 * node, where there is no `window` at all, and a store that throws on import
 * takes the suite with it.
 */
function systemTheme(): Theme {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return 'dark';
  }
  return window.matchMedia(LIGHT_QUERY).matches ? 'light' : 'dark';
}

/**
 * What the reader chose, or `system` where they have not.
 *
 * `web/public/theme.js` reads the same key with the same three-state meaning,
 * and it has to: it runs before any module can, which is what stamps the right
 * theme on the frame this bundle has not painted yet. The two are a pair —
 * change the key or the meaning here and change it there, or the first frame
 * is the wrong theme rather than merely the default one.
 */
export function readStoredTheme(): ThemeChoice {
  try {
    const stored = localStorage.getItem(THEME_KEY);
    return stored === 'light' || stored === 'dark' ? stored : 'system';
  } catch {
    // localStorage can throw in a private context. Following the system is
    // the default there too — it is the answer that needs nothing stored.
    return 'system';
  }
}

export function writeStoredTheme(choice: ThemeChoice): void {
  try {
    if (choice === 'system') {
      localStorage.removeItem(THEME_KEY);
    } else {
      localStorage.setItem(THEME_KEY, choice);
    }
  } catch {
    // Preference is lost on reload; the toggle still works for this session.
  }
}

/** The choice turned into the one of two themes that can be painted. */
export function resolveTheme(choice: ThemeChoice): Theme {
  return choice === 'system' ? systemTheme() : choice;
}

/**
 * Calls back when the machine's colour preference changes.
 *
 * A desktop that switches on a schedule switches while yagit is open, and a
 * preference read once at load is a preference that is wrong for the rest of
 * the evening. Returns the unsubscribe, and returns one that does nothing
 * where there is no `matchMedia` to unsubscribe from, so no caller has to ask
 * which it got.
 */
export function watchSystemTheme(onChange: () => void): () => void {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return () => undefined;
  }
  const query = window.matchMedia(LIGHT_QUERY);
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}

export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
}

export function readStoredPaths(): string[] {
  try {
    const raw = localStorage.getItem(PATHS_KEY);
    if (raw === null) {
      return [];
    }
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter((item): item is string => typeof item === 'string' && item !== '');
  } catch {
    return [];
  }
}

export function writeStoredPaths(paths: string[]): void {
  try {
    localStorage.setItem(PATHS_KEY, JSON.stringify(paths));
  } catch {
    // Same as theme: the session still works; only the next reload forgets.
  }
}

export function readStoredActivePath(): string | undefined {
  try {
    return localStorage.getItem(ACTIVE_KEY) ?? undefined;
  } catch {
    return undefined;
  }
}

export function writeStoredActivePath(path: string | undefined): void {
  try {
    if (path === undefined) {
      localStorage.removeItem(ACTIVE_KEY);
    } else {
      localStorage.setItem(ACTIVE_KEY, path);
    }
  } catch {
    // Preference lost on reload.
  }
}

/** Per-path history scope: head | all | refs. Selected refs themselves stay in memory. */
export function readStoredScope(path: string): 'head' | 'all' | 'refs' | undefined {
  try {
    const raw = localStorage.getItem(SCOPE_KEY);
    if (raw === null) {
      return undefined;
    }
    const parsed: unknown = JSON.parse(raw);
    if (parsed === null || typeof parsed !== 'object') {
      return undefined;
    }
    const value = (parsed as Record<string, unknown>)[path];
    if (value === 'head' || value === 'all' || value === 'refs') {
      return value;
    }
    return undefined;
  } catch {
    return undefined;
  }
}

export function writeStoredScope(path: string, scope: 'head' | 'all' | 'refs'): void {
  try {
    const raw = localStorage.getItem(SCOPE_KEY);
    const parsed: Record<string, string> =
      raw === null ? {} : (JSON.parse(raw) as Record<string, string>);
    parsed[path] = scope;
    localStorage.setItem(SCOPE_KEY, JSON.stringify(parsed));
  } catch {
    // Preference lost on reload.
  }
}
