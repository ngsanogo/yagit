import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  applyTheme,
  readStoredActivePath,
  readStoredPaths,
  readStoredScope,
  readStoredTheme,
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
  it('defaults theme to dark and round-trips light', () => {
    expect(readStoredTheme()).toBe('dark');
    writeStoredTheme('light');
    expect(readStoredTheme()).toBe('light');
    applyTheme('light');
    expect(documentElement.dataset.theme).toBe('light');
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
