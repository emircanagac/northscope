import { useCallback, useEffect, useState } from 'react';

export type ThemeMode = 'light' | 'dark';

const THEME_STORAGE_KEY = 'northscope-theme';

export function initialThemeMode(): ThemeMode {
  if (typeof window === 'undefined') {
    return 'light';
  }
  try {
    const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (stored === 'light' || stored === 'dark') {
      return stored;
    }
  } catch {
    return 'light';
  }
  return typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

// useThemeMode follows the system theme until the user picks one, then keeps
// that choice across reloads.
export function useThemeMode(): { mode: ThemeMode; isDark: boolean; toggle: () => void } {
  const [mode, setMode] = useState<ThemeMode>(initialThemeMode);
  const isDark = mode === 'dark';

  useEffect(() => {
    document.documentElement.classList.toggle('dark', isDark);
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, mode);
    } catch {
      // Ignore storage errors so private browsing policies do not break the UI.
    }
  }, [isDark, mode]);

  const toggle = useCallback(() => setMode((current) => (current === 'dark' ? 'light' : 'dark')), []);
  return { mode, isDark, toggle };
}
