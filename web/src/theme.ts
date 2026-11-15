import { useEffect, useState } from 'react';
import { load, save } from './lib/storage';

export type ThemeMode = 'system' | 'light' | 'dark';
const order: ThemeMode[] = ['system', 'light', 'dark'];

function apply(mode: ThemeMode) {
  const root = document.documentElement;
  if (mode === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', mode);
}

export function useTheme(): [ThemeMode, () => void] {
  const [mode, setMode] = useState<ThemeMode>(() => {
    const s = load('theme');
    return s === 'light' || s === 'dark' ? s : 'system';
  });
  useEffect(() => {
    apply(mode);
    save('theme', mode);
  }, [mode]);
  return [mode, () => setMode((m) => order[(order.indexOf(m) + 1) % order.length])];
}
