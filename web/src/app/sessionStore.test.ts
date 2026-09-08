import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  applyTheme,
  readStoredActivePath,
  readStoredPaths,
  readStoredScope,
  readStoredTheme,
  resolveTheme,
  watchSystemTheme,
  writeStoredActivePath,
  writeStoredPaths,
  writeStoredScope,
  writeStoredTheme,
} from './sessionStore';

/**
 * Vitest runs in node here (see vite.config.ts). sessionStore talks to
 * localStorage and document — stub both so the round-trip is what is under
 * test, not a DOM environment the suite otherwise does not need.
 */
const memory = new Map<string, string>();

const localStorageStub = {
  getItem: (key: string) => memory.get(key) ?? null,
  setItem: (key: string, value: string) => {
    memory.set(key, value);
  },
  removeItem: (key: string) => {
    memory.delete(key);
  },
  clear: () => {
    memory.clear();
  },
};

const documentElement = {
  dataset: {} as Record<string, string | undefined>,
  removeAttribute(name: string) {
    if (name === 'data-theme') {
      delete this.dataset.theme;
    }
  },
};

beforeEach(() => {
  memory.clear();
  documentElement.dataset = {};
  vi.stubGlobal('localStorage', localStorageStub);
  vi.stubGlobal('document', { documentElement });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('sessionStore', () => {
  it('follows the system until the reader chooses, and stores that as no key at all', () => {
    expect(readStoredTheme()).toBe('system');

    writeStoredTheme('light');
    expect(readStoredTheme()).toBe('light');
    applyTheme(resolveTheme('light'));
    expect(documentElement.dataset.theme).toBe('light');

    // Going back to "system" removes the key rather than writing a third
    // string: absence is what a reader who never touched the control has, and
    // the two have to mean the same thing or a reset would not be one.
    writeStoredTheme('system');
    expect(localStorage.getItem('yagit.theme')).toBeNull();
    expect(readStoredTheme()).toBe('system');
  });

  it('resolves "system" through the colour preference, and dark where there is none', () => {
    // Vitest runs this file under node, where there is no window to ask — the
    // same shape as a browser too old to answer, and dark is the theme that
    // has had the proportional pass.
    expect(resolveTheme('system')).toBe('dark');

    const listeners: (() => void)[] = [];
    vi.stubGlobal('window', {
      matchMedia: (query: string) => ({
        matches: query === '(prefers-color-scheme: light)',
        addEventListener: (_event: string, listener: () => void) => {
          listeners.push(listener);
        },
        removeEventListener: () => {
          listeners.pop();
        },
      }),
    });

    expect(resolveTheme('system')).toBe('light');
    // A choice is a choice: a light desktop does not get to reinterpret it.
    expect(resolveTheme('dark')).toBe('dark');

    const stop = watchSystemTheme(() => undefined);
    expect(listeners).toHaveLength(1);
    stop();
    expect(listeners).toHaveLength(0);
  });

  it('round-trips open paths and the active one', () => {
    expect(readStoredPaths()).toEqual([]);
    writeStoredPaths(['/repos/a', '/repos/b']);
    expect(readStoredPaths()).toEqual(['/repos/a', '/repos/b']);

    writeStoredActivePath('/repos/b');
    expect(readStoredActivePath()).toBe('/repos/b');
    writeStoredActivePath(undefined);
    expect(readStoredActivePath()).toBeUndefined();
  });

  it('ignores malformed path JSON', () => {
    localStorage.setItem('yagit.open-paths', '{not-json');
    expect(readStoredPaths()).toEqual([]);
    localStorage.setItem('yagit.open-paths', '["ok", 1, ""]');
    expect(readStoredPaths()).toEqual(['ok']);
  });

  it('stores history scope per path and ignores unknown values', () => {
    writeStoredScope('/repos/a', 'refs');
    expect(readStoredScope('/repos/a')).toBe('refs');
    expect(readStoredScope('/repos/missing')).toBeUndefined();

    localStorage.setItem('yagit.history-scope', JSON.stringify({ '/repos/a': 'nope' }));
    expect(readStoredScope('/repos/a')).toBeUndefined();
  });
});
