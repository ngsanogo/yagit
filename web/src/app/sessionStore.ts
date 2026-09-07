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

export type Theme = 'dark' | 'light';

export function readStoredTheme(): Theme {
  try {
    return localStorage.getItem(THEME_KEY) === 'light' ? 'light' : 'dark';
  } catch {
    // localStorage can throw in a private context; dark is the finished default.
    return 'dark';
  }
}

export function writeStoredTheme(theme: Theme): void {
  try {
    localStorage.setItem(THEME_KEY, theme);
  } catch {
    // Preference is lost on reload; the toggle still works for this session.
  }
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
