// The UI talks to `api` only. The data source is fixed at build time (vite.config.ts):
// the JSON API of the Go server (`rest`), or in-memory fixtures (`fake`) for the
// single-file mockup and for `npm run dev` without a server.
import { useCallback, useEffect, useState, useSyncExternalStore } from 'react';
import { fakeApi } from './fake';
import { restApi } from './rest';
import type { Api } from './types';

declare const __DATA__: 'fake' | 'rest';
export const FAKE = __DATA__ === 'fake';

const inner: Api = FAKE ? fakeApi : restApi;

// Every mutation bumps a version; queries refetch when it changes.
let version = 0;
// Versions up to this one came from the server's event stream (changes made elsewhere):
// queries refetch them quietly — no "loading" dimming, and a failed refetch keeps the data.
let quietUpTo = 0;
const listeners = new Set<() => void>();
const bump = (quiet = false) => {
  version++;
  if (quiet) quietUpTo = version;
  listeners.forEach((l) => l());
};
const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

const MUTATIONS: (keyof Api)[] = [
  'createTask',
  'updateTask',
  'updateTodo',
  'createTodo',
  'bulkUpdateTasks',
  'bulkUpdateTodos',
  'writeDocument',
  'revertDocument',
  'approveDocument',
  'revokeApproval',
  'setItem',
  'addDraftComment',
  'updateDraftComment',
  'deleteDraftComment',
  'setRoundSummary',
  'submitRound',
  'discardRound',
  'reply',
  'setCommentState',
];

export const api: Api = new Proxy(inner, {
  get(target, prop: keyof Api) {
    const fn = target[prop] as (...a: unknown[]) => Promise<unknown>;
    if (!MUTATIONS.includes(prop)) return fn;
    return async (...args: unknown[]) => {
      const res = await fn(...args);
      bump();
      return res;
    };
  },
});

export interface Query<T> {
  data: T | undefined;
  error: Error | undefined;
  loading: boolean;
}

/**
 * Runs `load` on mount, when `key` changes, after any mutation and after a change made
 * elsewhere (live updates). Keeps stale data while reloading.
 */
export function useQuery<T>(key: string, load: () => Promise<T>): Query<T> {
  const v = useSyncExternalStore(subscribe, () => version);
  const [state, setState] = useState<Query<T> & { key?: string }>({ data: undefined, error: undefined, loading: true });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(load, [key, v]);
  useEffect(() => {
    let live = true;
    const quiet = v <= quietUpTo;
    setState((s) => {
      if (s.key !== key) return { data: undefined, error: undefined, loading: true };
      return quiet && s.data !== undefined ? s : { ...s, loading: true };
    });
    run().then(
      (data) => live && setState({ data, error: undefined, loading: false, key }),
      (error: Error) =>
        live && setState((s) => (quiet && s.key === key && s.data !== undefined ? s : { data: undefined, error, loading: false, key })),
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [run, key]);
  return state;
}

// ---------------------------------------------------------------- live updates

/** State of the server's event stream: `live`, reconnecting, or given up (`offline`). */
export type LiveState = 'live' | 'connecting' | 'offline';

let liveState: LiveState = 'connecting';
const liveListeners = new Set<() => void>();
const setLive = (s: LiveState) => {
  if (s === liveState) return;
  liveState = s;
  liveListeners.forEach((l) => l());
};

/** The event stream's state, for the "no connection" mark in the header. Always `live` in the mockup. */
export function useLive(): LiveState {
  return useSyncExternalStore(
    (l) => {
      liveListeners.add(l);
      return () => liveListeners.delete(l);
    },
    () => (FAKE ? 'live' : liveState),
  );
}

// Changes made elsewhere (an agent through MCP, another tab or device) arrive as `change`
// events on /api/events; a burst of them becomes one quiet refetch. The browser reconnects
// by itself after a network drop. A failed (re)connection — the server restarting, or an
// expired Cloudflare Access session answering with a login redirect — closes the stream for
// good, so it is retried here with a growing delay, and again whenever the tab comes back.
const COALESCE_MS = 300;
const RETRY_MS = [2000, 5000, 10000, 30000, 60000];
const MAX_RETRIES = 10;

function startLive() {
  let es: EventSource | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let pending: ReturnType<typeof setTimeout> | undefined;
  let failures = 0;
  let opened = false;

  const refresh = () => {
    if (pending) return;
    pending = setTimeout(() => {
      pending = undefined;
      bump(true);
    }, COALESCE_MS);
  };

  const connect = () => {
    clearTimeout(timer);
    timer = undefined;
    es?.close();
    setLive('connecting');
    const src = new EventSource('/api/events');
    es = src;
    src.onopen = () => {
      // Whatever changed while the stream was down is picked up by one refetch.
      if (opened) refresh();
      opened = true;
      failures = 0;
      setLive('live');
    };
    src.addEventListener('change', refresh);
    src.onerror = () => {
      if (src.readyState === EventSource.CONNECTING) {
        setLive('connecting'); // the browser retries by itself
        return;
      }
      src.close();
      failures++;
      if (failures > MAX_RETRIES) {
        setLive('offline');
        return;
      }
      setLive(failures > 2 ? 'offline' : 'connecting');
      timer = setTimeout(connect, RETRY_MS[Math.min(failures - 1, RETRY_MS.length - 1)]);
    };
  };

  const onVisible = () => {
    if (document.visibilityState !== 'visible') return;
    if (!es || es.readyState === EventSource.CLOSED) {
      failures = 0;
      connect();
    } else {
      refresh(); // a phone may have slept through events
    }
  };
  document.addEventListener('visibilitychange', onVisible);
  window.addEventListener('online', onVisible);
  connect();
}

if (!FAKE && typeof EventSource !== 'undefined') startLive();

export * from './types';
