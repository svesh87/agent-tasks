// Routing with the contract's URL scheme: /t/<id>, /t/<id>/d/<slug>, /t/<id>/d/<slug>/r/<n>,
// /t/<id>/d/<slug>/diff/<a>..<b>, /todos, /todos/<id>.
//
// Served by the Go binary the app uses real paths (the server answers unknown paths with
// index.html). The single-file mockup has no server, so there the same paths live in the
// hash (#/t/T-12). The mode is fixed at build time (see vite.config.ts).
import { useSyncExternalStore } from 'react';

declare const __ROUTING__: 'hash' | 'history';
const HASH = __ROUTING__ === 'hash';

export type Route =
  | { name: 'list'; query: URLSearchParams }
  | { name: 'task'; id: string; query: URLSearchParams }
  | { name: 'doc'; id: string; slug: string; rev?: number; query: URLSearchParams }
  | { name: 'diff'; id: string; slug: string; from: number; to: number }
  | { name: 'todos'; query: URLSearchParams }
  | { name: 'todo'; id: string }
  | { name: 'notfound'; path: string };

/** Parses an app path such as `/t/T-1/d/plan?c=3`. */
export function parse(raw: string): Route {
  const [path, qs = ''] = (raw || '/').split('?');
  const query = new URLSearchParams(qs);
  const p = path.split('/').filter(Boolean).map(decodeURIComponent);
  if (p.length === 0) return { name: 'list', query };
  if (p[0] === 'todos' && p.length === 1) return { name: 'todos', query };
  if (p[0] === 'todos' && p.length === 2) return { name: 'todo', id: p[1] };
  if (p[0] === 't' && p[1]) {
    const id = p[1];
    if (p.length === 2) return { name: 'task', id, query };
    if (p[2] === 'd' && p[3]) {
      const slug = p[3];
      if (p.length === 4) return { name: 'doc', id, slug, query };
      if (p[4] === 'r' && /^\d+$/.test(p[5] ?? '') && p.length === 6) return { name: 'doc', id, slug, rev: Number(p[5]), query };
      const m = p[4] === 'diff' && p.length === 6 ? p[5].match(/^(\d+)\.\.(\d+)$/) : null;
      if (m) return { name: 'diff', id, slug, from: Number(m[1]), to: Number(m[2]) };
    }
  }
  return { name: 'notfound', path };
}

export const paths = {
  list: (query?: URLSearchParams) => (query && query.toString() ? `/?${query}` : '/'),
  todos: (query?: URLSearchParams) => (query && query.toString() ? `/todos?${query}` : '/todos'),
  todo: (id: string) => `/todos/${id}`,
  task: (id: string) => `/t/${id}`,
  doc: (id: string, slug: string, rev?: number) => (rev ? `/t/${id}/d/${slug}/r/${rev}` : `/t/${id}/d/${slug}`),
  diff: (id: string, slug: string, a: number, b: number) => `/t/${id}/d/${slug}/diff/${a}..${b}`,
};

/** The href for an app path. */
export const href = (path: string) => (HASH ? `#${path}` : path);

/** The current app path (`/t/T-1?x=y`). */
function current(): string {
  if (HASH) return location.hash.replace(/^#/, '') || '/';
  return location.pathname + location.search;
}

const EVENT = 'app:navigate';

export function navigate(path: string, replace = false) {
  if (replace) history.replaceState(null, '', href(path));
  else history.pushState(null, '', href(path));
  window.dispatchEvent(new Event(EVENT));
}

const subscribe = (cb: () => void) => {
  window.addEventListener(EVENT, cb);
  window.addEventListener('popstate', cb);
  window.addEventListener('hashchange', cb);
  return () => {
    window.removeEventListener(EVENT, cb);
    window.removeEventListener('popstate', cb);
    window.removeEventListener('hashchange', cb);
  };
};

export function useRoutePath(): string {
  return useSyncExternalStore(subscribe, current);
}

/** In history mode, same-origin <a> clicks navigate without a page load. */
export function interceptLinks() {
  if (HASH) return;
  document.addEventListener('click', (e) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    const a = (e.target as Element | null)?.closest?.('a');
    if (!a || a.target || a.hasAttribute('download')) return;
    const url = new URL(a.href, location.href);
    if (url.origin !== location.origin || url.pathname.startsWith('/api/')) return;
    e.preventDefault();
    navigate(url.pathname + url.search);
  });
}

/** Absolute URL of an app path, for "copy link". */
export const absolute = (path: string) => `${location.origin}${HASH ? location.pathname + '#' : ''}${path}`;
