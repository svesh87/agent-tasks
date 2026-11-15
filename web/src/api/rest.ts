// The real data source: the Go server's JSON API under /api (docs/backend-design.md,
// "JSON API"). Reshapes its answers into the UI's model (types.ts).
//
// Offsets: the API counts anchor offsets in Unicode code points, the UI in UTF-16 indices;
// conversion happens here against the revision body the offsets belong to.
import { cpToUtf16, utf16ToCp } from '../lib/markdown';
import { paths } from '../router';
import { ConflictError } from './types';
import type {
  HitKind,
  SearchHit,
  Anchor,
  Api,
  By,
  Comment,
  DiffView,
  DocumentSummary,
  DocumentView,
  JournalEntry,
  PendingRound,
  Place,
  PlaceSummary,
  PlanItem,
  ReviewRound,
  Revision,
  RevisionMeta,
  Task,
  TaskDetail,
  TaskListItem,
  TaskListResult,
  TaskStatus,
  Todo,
  TodoListResult,
  TodoState,
} from './types';

// ---------------------------------------------------------------- transport

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let data: unknown = undefined;
  try {
    data = text ? JSON.parse(text) : undefined;
  } catch {
    /* not JSON */
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | undefined)?.error ?? `${res.status} ${res.statusText}`;
    throw Object.assign(new Error(msg), { status: res.status });
  }
  return data as T;
}

const get = <T>(path: string) => call<T>('GET', path);
const enc = encodeURIComponent;

function qs(params: Record<string, string | number | string[] | undefined>): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === '') continue;
    if (Array.isArray(v)) v.forEach((x) => q.append(k, x));
    else q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : '';
}

// ---------------------------------------------------------------- API shapes (subset)

interface ApiPlace extends Place {
  role?: 'primary' | 'touched';
  note?: string;
}
interface ApiTask {
  id: string;
  title: string;
  summary?: string;
  status: TaskStatus;
  waiting_on?: string;
  closed_reason?: string;
  archived_at?: string;
  tags: string[] | null;
  places: ApiPlace[] | null;
  created_at: string;
  updated_at: string;
  url?: string;
  workspace?: string;
}
interface ApiHit {
  source?: string;
  /** v0.6: task | document | comment | round | journal | todo */
  kind?: string;
  /** v0.6: deep link of the matched object. */
  url?: string;
  label?: string;
  text: string;
  highlights: [number, number][] | null;
}
interface ApiDocSummary {
  slug: string;
  kind: DocumentSummary['kind'];
  title: string;
  latest_revision: number;
  updated_at: string;
  open_comments: number;
  items_done: number;
  items_total: number;
}
interface ApiRoundBrief {
  round: number;
  slug: string;
  n: number;
  revision: number;
  state: PendingRound['state'];
  comments: number;
  submitted_at?: string;
}
interface ApiTaskOverview extends ApiTask {
  documents: ApiDocSummary[] | null;
  plan_progress: { done: number; total: number };
  journal: JournalEntry[] | null;
  pending_rounds: ApiRoundBrief[] | null;
  draft_rounds: ApiRoundBrief[] | null;
  todos: Todo[] | null;
}
interface ApiNote {
  author?: string;
  by?: string;
  body: string;
  created_at: string;
}
interface ApiItem {
  id: string;
  text: string;
  done: boolean;
  done_at?: string;
  done_by?: string;
  by?: string;
  line: number;
  notes: ApiNote[] | null;
}
interface ApiAnchor {
  quote: string;
  prefix?: string;
  suffix?: string;
  start: number;
  end: number;
}
interface ApiComment {
  id: number;
  round: number;
  revision: number;
  anchor?: ApiAnchor | null;
  current?: { revision: number; quote: string; start: number; end: number };
  outdated_in?: number;
  body: string;
  state: Comment['state'];
  replies: { author: 'agent' | 'owner'; body: string; created_at: string }[] | null;
  created_at: string;
}
interface ApiRound {
  round: number;
  n: number;
  revision: number;
  state: ReviewRound['state'];
  summary?: string;
  message?: string;
  created_at: string;
  submitted_at?: string;
  addressed_at?: string;
  addressed_revision?: number;
}
interface ApiDocView {
  document: { task: string; slug: string; kind: DocumentView['kind']; title: string; latest_revision: number };
  revision: Revision;
  items: ApiItem[] | null;
  comments: ApiComment[] | null;
  revisions: Revision[] | null;
  rounds: ApiRound[] | null;
}

// ---------------------------------------------------------------- mapping

const asBy = (s: string | undefined): By => (s === 'owner' ? 'owner' : 'agent');

function mapTask(t: ApiTask): Task {
  return {
    id: t.id,
    title: t.title,
    summary: t.summary || undefined,
    status: t.status,
    waiting_on: t.waiting_on || undefined,
    closed_reason: t.closed_reason === 'done' || t.closed_reason === 'cancelled' ? t.closed_reason : undefined,
    archived_at: t.archived_at || undefined,
    tags: t.tags ?? [],
    places: (t.places ?? []).map((p) => ({ ...p, role: p.role ?? 'touched', locations: p.locations ?? [] })),
    created_at: t.created_at,
    updated_at: t.updated_at,
  };
}

function mapItem(it: ApiItem): PlanItem {
  return {
    id: it.id,
    text: it.text,
    done: it.done,
    done_by: it.done ? ((it.done_by ?? it.by) as By | undefined) : undefined,
    done_at: it.done_at || undefined,
    line: it.line,
    notes: (it.notes ?? []).map((n) => ({ body: n.body, author: asBy(n.by ?? n.author), created_at: n.created_at })),
  };
}

/** Converts an anchor's code-point offsets to UTF-16 against `body` (the revision it is on). */
function toUtf16<T extends { start: number; end: number }>(a: T, body: string | undefined): T {
  if (body === undefined) return a;
  return { ...a, start: cpToUtf16(body, a.start), end: cpToUtf16(body, a.end) };
}

function mapComment(c: ApiComment, bodies: Map<number, string>): Comment {
  const anchor: Anchor | null = c.anchor
    ? toUtf16({ quote: c.anchor.quote, prefix: c.anchor.prefix ?? '', suffix: c.anchor.suffix ?? '', start: c.anchor.start, end: c.anchor.end }, bodies.get(c.revision))
    : null;
  return {
    id: c.id,
    round: c.round,
    revision: c.revision,
    anchor,
    current: c.current ? toUtf16(c.current, bodies.get(c.current.revision)) : undefined,
    outdated_in: c.outdated_in || undefined,
    body: c.body,
    state: c.state,
    replies: (c.replies ?? []).map((r, i) => ({ id: c.id * 1000 + i, author: r.author, body: r.body, created_at: r.created_at })),
    created_at: c.created_at,
  };
}

function mapRounds(rounds: ApiRound[], comments: Comment[]): ReviewRound[] {
  return rounds
    .map((r) => ({
      id: r.round,
      n: r.n,
      revision: r.revision,
      state: r.state,
      summary: r.summary || undefined,
      created_at: r.created_at,
      submitted_at: r.submitted_at || undefined,
      addressed_at: r.addressed_at || undefined,
      addressed_message: r.message || undefined,
      addressed_in: r.addressed_revision || undefined,
      comments: comments.filter((c) => c.round === r.round),
    }))
    .sort((a, b) => b.n - a.n);
}

const meta = ({ body: _b, ...m }: Revision): RevisionMeta => m;

/** Absolute or relative server URL → app path (same origin). */
function appPath(url: string): string {
  try {
    const u = new URL(url, location.origin);
    return u.pathname + u.search;
  } catch {
    return url;
  }
}

const KINDS: HitKind[] = ['task', 'document', 'comment', 'round', 'journal', 'todo'];

function mapHits(t: ApiTask, hits: ApiHit[]): SearchHit[] {
  return hits.map((h) => {
    let text = '';
    let at = 0;
    let first = '';
    for (const [s, e] of [...(h.highlights ?? [])].sort((a, b) => a[0] - b[0])) {
      const a = cpToUtf16(h.text, s);
      const b = cpToUtf16(h.text, e);
      first ||= h.text.slice(a, b);
      text += h.text.slice(at, a) + '[[' + h.text.slice(a, b) + ']]';
      at = b;
    }
    text += h.text.slice(at);
    const src = h.source ?? '';
    const kind: HitKind = KINDS.includes(h.kind as HitKind)
      ? (h.kind as HitKind)
      : src.startsWith('doc:')
        ? 'document'
        : src.startsWith('comment:')
          ? 'comment'
          : src.startsWith('round:')
            ? 'round'
            : src.startsWith('journal:')
              ? 'journal'
              : 'task';
    // The server's deep link when it sends one; else the best path `source` allows.
    let path = h.url ? appPath(h.url) : paths.task(t.id);
    if (!h.url && kind === 'document') path = paths.doc(t.id, src.slice(4));
    if (!h.url && kind === 'journal') path = `${paths.task(t.id)}?j=${src.slice(8)}`;
    // Older servers send no position: fall back to highlighting the matched word.
    if (kind === 'document' && first && !/[?&](at|hl)=/.test(path)) path += `${path.includes('?') ? '&' : '?'}hl=${encodeURIComponent(first)}`;
    return { kind, label: h.label || (kind === 'document' && src.startsWith('doc:') ? src.slice(4) : undefined), text, path };
  });
}

// Bodies of revisions seen, for converting offsets of new comments (key task/slug/n).
const bodies = new Map<string, string>();
const latestRev = new Map<string, number>();

async function docView(task: string, slug: string, rev?: number): Promise<ApiDocView> {
  const v = await get<ApiDocView>(`/api/tasks/${enc(task)}/docs/${enc(slug)}${qs({ rev })}`);
  bodies.set(`${task}/${slug}/${v.revision.n}`, v.revision.body);
  latestRev.set(`${task}/${slug}`, v.document.latest_revision);
  return v;
}

function mapDocView(v: ApiDocView, task: Task, extraBodies: Revision[] = []): DocumentView {
  const known = new Map<number, string>([[v.revision.n, v.revision.body], ...extraBodies.map((r) => [r.n, r.body] as [number, string])]);
  const comments = (v.comments ?? []).map((c) => mapComment(c, known));
  return {
    task,
    slug: v.document.slug,
    kind: v.document.kind,
    title: v.document.title,
    latest: v.document.latest_revision,
    revision: v.revision,
    revisions: (v.revisions ?? []).map(meta).sort((a, b) => b.n - a.n),
    items: (v.items ?? []).map(mapItem),
    rounds: mapRounds(v.rounds ?? [], comments),
  };
}

/** Runs an owner write; a 409 (the document moved on) becomes ConflictError with the newest n. */
async function withConflict(task: string, slug: string, base: number, write: () => Promise<{ revision: number }>): Promise<{ n: number }> {
  try {
    const res = await write();
    return { n: res.revision };
  } catch (e) {
    if ((e as Error & { status?: number }).status !== 409) throw e;
    const cur = await get<{ revisions: Revision[] | null }>(`/api/tasks/${enc(task)}/docs/${enc(slug)}/revisions`);
    const latest = Math.max(base + 1, ...(cur.revisions ?? []).map((r) => r.n));
    throw new ConflictError((e as Error).message, latest);
  }
}

const STATUSES: TaskStatus[] = ['active', 'waiting', 'later', 'archived'];
const TODO_STATES: TodoState[] = ['open', 'in_task', 'done', 'dropped'];

// ---------------------------------------------------------------- the API

export const restApi: Api = {
  async listTasks(q) {
    const sort = q.sort === 'relevance' && !q.q?.trim() ? 'updated' : q.sort;
    const base = { q: q.q?.trim(), tag: q.tags, place: q.place };
    const res = await get<{
      total: number;
      tasks: (ApiTask & {
        hits: ApiHit[] | null;
        plan_progress: { done: number; total: number } | null;
        doc_count: number;
        submitted_rounds: number;
        draft_rounds: number;
      })[];
      status_counts: TaskListResult['status_counts'];
      tag_counts: { tag: string; count: number }[] | null;
    }>(`/api/tasks${qs({ ...base, status: q.status.length ? q.status.join(',') : STATUSES.join(','), sort, limit: 100 })}`);
    const items: TaskListItem[] = res.tasks.map((t) => ({
      task: mapTask(t),
      plan: t.plan_progress && t.plan_progress.total ? t.plan_progress : undefined,
      documents: t.doc_count ?? 0,
      submitted_rounds: t.submitted_rounds ?? 0,
      draft_rounds: t.draft_rounds ?? 0,
      hits: q.q?.trim() ? mapHits(t, t.hits ?? []) : undefined,
    }));
    const sc: Partial<TaskListResult['status_counts']> = res.status_counts ?? {};
    const status_counts = { active: sc.active ?? 0, waiting: sc.waiting ?? 0, later: sc.later ?? 0, archived: sc.archived ?? 0 };
    return { items, status_counts, tag_counts: res.tag_counts ?? [] };
  },

  async getTask(id, machine) {
    const o = await get<ApiTaskOverview>(`/api/tasks/${enc(id)}${qs({ machine })}`);
    const task = mapTask(o);
    const docs = o.documents ?? [];
    const rounds = [...(o.draft_rounds ?? []), ...(o.pending_rounds ?? [])];
    const documents: DocumentSummary[] = docs.map((d) => {
      const draft = (o.draft_rounds ?? []).find((r) => r.slug === d.slug);
      const sub = (o.pending_rounds ?? []).find((r) => r.slug === d.slug && r.state === 'submitted');
      return {
        slug: d.slug,
        kind: d.kind,
        title: d.title,
        latest: d.latest_revision,
        updated_at: d.updated_at,
        plan: d.items_total ? { done: d.items_done, total: d.items_total } : undefined,
        open_comments: d.open_comments,
        draft_round: draft && { n: draft.n, comments: draft.comments },
        submitted_round: sub && { n: sub.n, comments: sub.comments },
      };
    });
    const planDoc = docs.find((d) => d.kind === 'plan' && d.items_total) ?? docs.find((d) => d.items_total);
    let plan: TaskDetail['plan'];
    if (planDoc) {
      const v = await docView(id, planDoc.slug);
      plan = { slug: planDoc.slug, done: planDoc.items_done, total: planDoc.items_total, items: (v.items ?? []).map(mapItem) };
    }
    const seen = new Set<number>();
    return {
      task,
      url: o.url ?? '',
      workspace: o.workspace ?? '',
      documents,
      plan,
      journal: [...(o.journal ?? [])].sort((a, b) => b.created_at.localeCompare(a.created_at)),
      rounds: rounds
        .filter((r) => !seen.has(r.round) && seen.add(r.round))
        .map((r) => ({
          id: r.round,
          slug: r.slug,
          doc_title: docs.find((d) => d.slug === r.slug)?.title ?? r.slug,
          n: r.n,
          revision: r.revision,
          state: r.state,
          comments: r.comments,
        })),
      todos: o.todos ?? [],
    };
  },

  async createTask(n) {
    return mapTask(await call<ApiTask>('POST', '/api/tasks', n));
  },

  async updateTask(id, patch) {
    return mapTask(await call<ApiTask>('PATCH', `/api/tasks/${enc(id)}`, patch));
  },

  async listPlaces() {
    const r = await get<{ places: PlaceSummary[] | null }>('/api/places');
    return r.places ?? [];
  },

  async listTags() {
    const r = await get<{ tags: { tag: string; count: number }[] | null }>('/api/tags?limit=500');
    return [...(r.tags ?? [])].sort((a, b) => b.count - a.count || a.tag.localeCompare(b.tag));
  },

  async listTodos(q) {
    const base = { q: q.q?.trim(), tag: q.tags, place: q.place };
    const res = await get<{
      total: number;
      todos: Todo[] | null;
      state_counts: TodoListResult['state_counts'];
      tag_counts: { tag: string; count: number }[] | null;
    }>(`/api/todos${qs({ ...base, state: (q.state.length ? q.state : TODO_STATES).join(','), limit: 200 })}`);
    return {
      total: res.total,
      todos: (res.todos ?? []).map((x) => ({ ...x, places: x.places ?? [], tags: x.tags ?? [] })),
      state_counts: Object.fromEntries(TODO_STATES.map((s) => [s, res.state_counts?.[s] ?? 0])) as TodoListResult['state_counts'],
      tag_counts: res.tag_counts ?? [],
    };
  },

  async getTodo(id) {
    const x = await get<Todo>(`/api/todos/${enc(id)}`);
    return { ...x, places: x.places ?? [], tags: x.tags ?? [] };
  },

  async updateTodo(id, patch) {
    return call<Todo>('PATCH', `/api/todos/${enc(id)}`, patch);
  },

  async createTodo(n) {
    return call<Todo>('POST', '/api/todos', n);
  },

  async bulkUpdateTasks(ids, patch) {
    // All or nothing on the server; its error names the task that failed.
    await call('POST', '/api/tasks/bulk', { ids, ...patch });
  },

  async bulkUpdateTodos(ids, state) {
    await call('POST', '/api/todos/bulk', { ids, state });
  },

  async writeDocument(task, slug, body, base, message) {
    return withConflict(task, slug, base, () =>
      call<{ revision: number }>('PUT', `/api/tasks/${enc(task)}/docs/${enc(slug)}`, {
        body,
        base_revision: base,
        ...(message?.trim() ? { message: message.trim() } : {}),
      }),
    );
  },

  async revertDocument(task, slug, to, base) {
    return withConflict(task, slug, base, () =>
      call<{ revision: number }>('POST', `/api/tasks/${enc(task)}/docs/${enc(slug)}/revert`, { to, base_revision: base }),
    );
  },

  async setItem(task, slug, item, change) {
    await call('POST', `/api/tasks/${enc(task)}/docs/${enc(slug)}/items/${enc(item)}`, change);
  },

  async getDocument(task, slug, revision) {
    const [v, o] = await Promise.all([docView(task, slug, revision), get<ApiTask>(`/api/tasks/${enc(task)}`)]);
    return mapDocView(v, mapTask(o));
  },

  async getDiff(task, slug, from, to): Promise<DiffView> {
    const [a, b, o] = await Promise.all([docView(task, slug, from), docView(task, slug, to), get<ApiTask>(`/api/tasks/${enc(task)}`)]);
    const view = mapDocView(a, mapTask(o), [b.revision]);
    return {
      task: view.task,
      slug: view.slug,
      title: view.title,
      latest: view.latest,
      revisions: view.revisions,
      from: a.revision,
      to: b.revision,
      rounds: view.rounds.filter((r) => r.revision >= from && r.revision < to && r.state !== 'draft'),
    };
  },

  async addDraftComment(task, slug, c) {
    const round = await call<ApiRound>('POST', `/api/tasks/${enc(task)}/docs/${enc(slug)}/rounds/draft`, {});
    const body = bodies.get(`${task}/${slug}/${round.revision}`) ?? bodies.get(`${task}/${slug}/${latestRev.get(`${task}/${slug}`)}`);
    const anchor = c.anchor && body !== undefined
      ? { quote: c.anchor.quote, start: utf16ToCp(body, c.anchor.start), end: utf16ToCp(body, c.anchor.end) }
      : c.anchor
        ? { quote: c.anchor.quote }
        : undefined;
    const out = await call<ApiComment>('POST', `/api/rounds/${round.round}/comments`, { body: c.body, anchor });
    return mapComment(out, new Map(body !== undefined ? [[round.revision, body]] : []));
  },

  async updateDraftComment(id, body) {
    await call('PATCH', `/api/comments/${id}`, { body });
  },
  async deleteDraftComment(id) {
    await call('DELETE', `/api/comments/${id}`);
  },
  async setRoundSummary(id, summary) {
    await call('PATCH', `/api/rounds/${id}`, { summary });
  },
  async submitRound(id) {
    await call('POST', `/api/rounds/${id}/submit`);
  },
  async discardRound(id) {
    await call('DELETE', `/api/rounds/${id}`);
  },
  async reply(id, body) {
    await call('POST', `/api/comments/${id}/replies`, { body });
  },
  async setCommentState(id, state) {
    await call('PATCH', `/api/comments/${id}`, { state });
  },
};
