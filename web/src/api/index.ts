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
const listeners = new Set<() => void>();
const bump = () => {
  version++;
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

/** Runs `load` on mount, when `key` changes and after any mutation. Keeps stale data while reloading. */
export function useQuery<T>(key: string, load: () => Promise<T>): Query<T> {
  const v = useSyncExternalStore(subscribe, () => version);
  const [state, setState] = useState<Query<T> & { key?: string }>({ data: undefined, error: undefined, loading: true });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(load, [key, v]);
  useEffect(() => {
    let live = true;
    setState((s) => ({ ...s, loading: true, ...(s.key !== key ? { data: undefined, error: undefined } : {}) }));
    run().then(
      (data) => live && setState({ data, error: undefined, loading: false, key }),
      (error: Error) => live && setState({ data: undefined, error, loading: false, key }),
    );
    return () => {
      live = false;
    };
  }, [run, key]);
  return state;
}

export * from './types';
