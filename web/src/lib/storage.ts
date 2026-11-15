// Per-browser preferences (language, theme, view toggles). Storage may be unavailable
// (private mode, sandboxed previews); everything works without it.
const PREFIX = 'agent-tasks:';

export function load(key: string): string | null {
  try {
    return localStorage.getItem(PREFIX + key);
  } catch {
    return null;
  }
}

export function save(key: string, value: string): void {
  try {
    localStorage.setItem(PREFIX + key, value);
  } catch {
    /* ignore */
  }
}
