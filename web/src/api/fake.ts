// In-memory implementation of Api over the fixtures. It behaves like the server would:
// plan items matched across revisions, comments carried to newer revisions (`current`) or
// marked outdated, drafts visible only here (the owner's UI), places with per-machine
// locations, todos, crude stemmed search.
import { ago, fixtures, fxPlaces, fxTodos, type FxDoc, type FxTask } from './fixtures';
import { anchorOf, matchItems, relocate, taskLines, utf16ToCp } from '../lib/markdown';
import { absolute, paths } from '../router';
import { ConflictError } from './types';
import type {
  Api,
  Comment,
  DiffView,
  DocKind,
  DocumentSummary,
  DocumentView,
  By,
  HitKind,
  ItemNote,
  SearchHit,
  NewTodo,
  JournalEntry,
  NewTask,
  PendingRound,
  Place,
  PlaceSummary,
  PlanItem,
  Revision,
  ReviewRound,
  Task,
  TaskDetail,
  TaskListItem,
  TaskListQuery,
  TaskListResult,
  TaskPatch,
  TaskStatus,
  Todo,
  TodoListResult,
  TodoQuery,
  TodoState,
} from './types';

interface ItemRec {
  id: string;
  done: boolean;
  done_by?: By;
  done_at?: string;
  notes: ItemNote[];
  at: Map<number, { line: number; text: string }>;
}
interface DocRec {
  slug: string;
  kind: DocKind;
  title: string;
  revisions: Revision[];
  items: ItemRec[];
  rounds: ReviewRound[];
}
interface TaskRec {
  task: Task;
  docs: DocRec[];
  journal: JournalEntry[];
}

let seq = 100;
const nextId = () => ++seq;
const now = () => new Date().toISOString();

// ---------------------------------------------------------------- places

const places = new Map<string, Place>();
for (const [key, label, locs] of fxPlaces) {
  const path = key.includes(':/');
  places.set(key, {
    key,
    kind: path ? 'path' : 'git',
    label: label ?? key.split('/').pop()!,
    locations: locs.map(([machine, p, at]) => ({ machine, path: p, seen_at: ago(at) })),
  });
}
const placeRef = (key: string): Place => {
  const p = places.get(key);
  if (!p) throw new Error(`fixture place not found: ${key}`);
  return { key: p.key, kind: p.kind, label: p.label };
};

// ---------------------------------------------------------------- build

/** Carries an anchored comment from its revision to later ones (server re-anchoring). */
function carry(c: Comment, revisions: Revision[]) {
  if (!c.anchor) return;
  let a = c.anchor;
  for (let n = c.revision + 1; n <= revisions.length; n++) {
    const next = relocate(revisions[n - 1].body, a);
    if (!next) {
      c.outdated_in = n;
      return;
    }
    a = next;
    c.current = { revision: n, quote: a.quote, start: a.start, end: a.end };
  }
}

function buildDoc(fx: FxDoc): DocRec {
  const revisions: Revision[] = fx.revisions.map((r, i) => ({
    n: i + 1,
    body: r.body,
    author: r.author,
    message: r.message,
    created_at: ago(r.at),
  }));

  const items: ItemRec[] = [];
  let prev: ItemRec[] = [];
  let itemN = 0;
  for (const rev of revisions) {
    const lines = taskLines(rev.body);
    const match = matchItems(
      prev.map((p) => p.at.get(rev.n - 1)!.text),
      lines.map((l) => l.text),
    );
    prev = lines.map((l, i) => {
      const rec = match[i] >= 0 ? prev[match[i]] : { id: `i${++itemN}`, done: l.checked, notes: [], at: new Map() };
      if (match[i] < 0) items.push(rec);
      rec.at.set(rev.n, { line: l.line, text: l.text });
      return rec;
    });
  }
  const latest = revisions.length;
  for (const [key, st] of Object.entries(fx.items ?? {})) {
    const rec = items.find((it) => it.at.get(latest)?.text.startsWith(key));
    if (!rec) throw new Error(`fixture item not found: ${key}`);
    rec.done = st.done;
    if (st.done) rec.done_by = st.by ?? 'agent';
    rec.notes = (st.notes ?? []).map(([body, at, by]) => ({ body, author: by ?? 'agent', created_at: ago(at) }));
  }

  const rounds: ReviewRound[] = (fx.rounds ?? []).map((r, i) => {
    const id = nextId();
    const body = revisions[r.revision - 1].body;
    return {
      id,
      n: i + 1,
      revision: r.revision,
      state: r.state,
      summary: r.summary,
      created_at: ago(r.at),
      submitted_at: r.submittedAt !== undefined ? ago(r.submittedAt) : undefined,
      addressed_at: r.addressedAt !== undefined ? ago(r.addressedAt) : undefined,
      addressed_message: r.addressedMessage,
      addressed_in: r.addressedIn,
      comments: r.comments.map((c) => {
        const comment: Comment = {
          id: nextId(),
          round: id,
          revision: r.revision,
          anchor: c.quote ? anchorOf(body, c.quote, c.occurrence) : null,
          body: c.body,
          state: c.state,
          created_at: ago(c.at),
          replies: (c.replies ?? []).map((rp) => ({ id: nextId(), author: rp.author, body: rp.body, created_at: ago(rp.at) })),
        };
        carry(comment, revisions);
        return comment;
      }),
    };
  });
  return { slug: fx.slug, kind: fx.kind, title: fx.title, revisions, items, rounds };
}

function buildTask(fx: FxTask): TaskRec {
  return {
    task: {
      id: fx.id,
      title: fx.title,
      summary: fx.summary,
      status: fx.status,
      waiting_on: fx.waiting_on,
      closed_reason: fx.closed_reason,
      archived_at: fx.archivedAt !== undefined ? ago(fx.archivedAt) : undefined,
      tags: fx.tags,
      places: (fx.places ?? []).map(([key, role, note]) => ({ ...places.get(key)!, role, note })),
      created_at: ago(fx.createdAt),
      updated_at: ago(fx.updatedAt),
    },
    docs: (fx.docs ?? []).map(buildDoc),
    journal: (fx.journal ?? [])
      .map((j) => ({ id: nextId(), kind: j.kind, body: j.body, source: j.source, created_at: ago(j.at) }))
      .sort((a, b) => b.created_at.localeCompare(a.created_at)),
  };
}

const db: TaskRec[] = fixtures.map(buildTask);
let taskSeq = Math.max(...db.map((t) => Number(t.task.id.slice(2))));

const todos: Todo[] = fxTodos.map((t, i) => ({
  id: `D-${i + 1}`,
  text: t.text,
  note: t.note,
  state: t.state,
  task: t.task,
  origin_task: t.origin,
  places: t.places.map(placeRef),
  tags: t.tags ?? [],
  source: t.source,
  created_at: ago(t.at),
  updated_at: ago(t.closedAt ?? t.at),
  closed_at: t.closedAt !== undefined ? ago(t.closedAt) : undefined,
}));

// ---------------------------------------------------------------- helpers

const clone = <T>(v: T): T => structuredClone(v);
const reply = <T>(v: T, ms = 60): Promise<T> => new Promise((res) => setTimeout(() => res(clone(v)), ms));
const fail = (msg: string) => Promise.reject(new Error(msg));

function findTask(id: string): TaskRec | undefined {
  return db.find((t) => t.task.id.toLowerCase() === id.toLowerCase());
}
function findDoc(t: TaskRec, slug: string) {
  return t.docs.find((d) => d.slug === slug);
}
function allComments(): { t: TaskRec; d: DocRec; r: ReviewRound; c: Comment }[] {
  const out: { t: TaskRec; d: DocRec; r: ReviewRound; c: Comment }[] = [];
  for (const t of db) for (const d of t.docs) for (const r of d.rounds) for (const c of r.comments) out.push({ t, d, r, c });
  return out;
}
function findRound(id: number) {
  for (const t of db) for (const d of t.docs) for (const r of d.rounds) if (r.id === id) return { t, d, r };
  return undefined;
}
function touch(t: TaskRec) {
  t.task.updated_at = now();
}

function itemsFor(d: DocRec, n: number): PlanItem[] {
  return d.items
    .filter((it) => it.at.has(n))
    .map((it) => ({ id: it.id, text: it.at.get(n)!.text, done: it.done, done_by: it.done_by, done_at: it.done_at, notes: it.notes, line: it.at.get(n)!.line }))
    .sort((a, b) => a.line! - b.line!);
}
function progress(d: DocRec) {
  const items = itemsFor(d, d.revisions.length);
  return items.length ? { done: items.filter((i) => i.done).length, total: items.length } : undefined;
}
function planDoc(t: TaskRec) {
  return t.docs.find((d) => d.kind === 'plan' && progress(d)) ?? t.docs.find((d) => progress(d));
}

function summarize(d: DocRec): DocumentSummary {
  const latest = d.revisions[d.revisions.length - 1];
  const draft = d.rounds.find((r) => r.state === 'draft');
  const submitted = d.rounds.filter((r) => r.state === 'submitted').pop();
  return {
    slug: d.slug,
    kind: d.kind,
    title: d.title,
    latest: latest.n,
    latest_author: latest.author,
    updated_at: latest.created_at,
    plan: progress(d),
    open_comments: d.rounds.filter((r) => r.state !== 'draft').flatMap((r) => r.comments).filter((c) => c.state === 'open').length,
    draft_round: draft && { n: draft.n, comments: draft.comments.length },
    submitted_round: submitted && { n: submitted.n, comments: submitted.comments.length },
  };
}

/** Task with place locations narrowed to one machine (as `GET /api/tasks/{id}?machine=`). */
function forMachine(task: Task, machine?: string): Task {
  if (!machine) return task;
  return { ...task, places: task.places.map((p) => ({ ...p, locations: (p.locations ?? []).filter((l) => l.machine === machine) })) };
}

// ---------------------------------------------------------------- search

const WORD = /[\p{L}\p{N}_-]+/gu;
/** Crude stand-in for Snowball: compare the first 4–6 letters of each word. */
const stem = (w: string) => {
  const s = w.toLowerCase().replace(/ё/g, 'е');
  return s.length <= 4 ? s : s.slice(0, Math.max(4, Math.min(6, s.length - 2)));
};
const terms = (q: string) => (q.match(WORD) ?? []).map(stem);
const hit = (term: string, s: string) => s.startsWith(term) || term.startsWith(s);

interface Field {
  kind: HitKind;
  label?: string;
  text: string;
  /** App path; `hl` (the matched word) is added for documents. */
  path: string;
  title?: boolean;
}

/** Hits of all fields, or null when some query term matches nowhere. */
function hitsOf(fields: Field[], ts: string[]): SearchHit[] | null {
  const words = new Set(fields.flatMap((f) => (f.text.match(WORD) ?? []).map(stem)));
  if (!ts.every((term) => [...words].some((w) => hit(term, w)))) return null;
  const out: SearchHit[] = [];
  for (const f of fields) {
    const re = /[\p{L}\p{N}_-]+/gu;
    let m: RegExpExecArray | null;
    while ((m = re.exec(f.text))) {
      const s = stem(m[0]);
      if (!ts.some((term) => hit(term, s))) continue;
      const from = Math.max(0, m.index - 60);
      const to = Math.min(f.text.length, m.index + m[0].length + 80);
      const text = (from > 0 ? '…' : '') + f.text.slice(from, m.index) + '[[' + m[0] + ']]' + f.text.slice(m.index + m[0].length, to) + (to < f.text.length ? '…' : '');
      // Documents: the position of the match, in code points of the body (as the server does).
      const at = `${utf16ToCp(f.text, m.index)}-${utf16ToCp(f.text, m.index + m[0].length)}`;
      const path = f.kind === 'document' ? `${f.path}${f.path.includes('?') ? '&' : '?'}at=${at}` : f.path;
      out.push({ kind: f.kind, label: f.label, text: f.title ? f.text : text.replace(/\s+/g, ' ').replace(/[#*`>]/g, ''), path });
      break;
    }
  }
  return out;
}

function taskFields(t: TaskRec): Field[] {
  const id = t.task.id;
  const out: Field[] = [{ kind: 'task', text: t.task.title, path: paths.task(id), title: true }];
  if (t.task.summary) out.push({ kind: 'task', text: t.task.summary, path: paths.task(id) });
  for (const d of t.docs) {
    out.push({ kind: 'document', label: d.title, text: d.revisions[d.revisions.length - 1].body, path: paths.doc(id, d.slug) });
    for (const r of d.rounds) {
      if (r.state === 'draft') continue;
      if (r.summary)
        out.push({ kind: 'round', label: d.title, text: r.summary, path: `${paths.doc(id, d.slug, r.revision === d.revisions.length ? undefined : r.revision)}?round=${r.id}` });
      for (const c of r.comments) {
        const rev = c.current?.revision ?? c.revision;
        out.push({
          kind: 'comment',
          label: d.title,
          text: [c.body, ...c.replies.map((x) => x.body)].join(' '),
          path: `${paths.doc(id, d.slug, rev === d.revisions.length ? undefined : rev)}?c=${c.id}`,
        });
      }
    }
  }
  for (const j of t.journal) out.push({ kind: 'journal', text: j.body, path: `${paths.task(id)}?j=${j.id}` });
  return out;
}

const placeMatch = (keys: string[], place?: string) => !place || (place === 'none' ? keys.length === 0 : keys.includes(place));

// ---------------------------------------------------------------- the API

export const fakeApi: Api = {
  listTasks(q: TaskListQuery): Promise<TaskListResult> {
    const status_counts: Record<TaskStatus, number> = { active: 0, waiting: 0, later: 0, archived: 0 };
    const tagCounts = new Map<string, number>();
    const items: TaskListItem[] = [];
    const ts = q.q?.trim() ? terms(q.q) : [];
    for (const t of db) {
      let hits: SearchHit[] | null | undefined;
      if (ts.length) {
        hits = hitsOf(taskFields(t), ts);
        if (hits === null) continue;
      }
      if (!q.tags.every((tag) => t.task.tags.includes(tag))) continue;
      if (!placeMatch(t.task.places.map((p) => p.key), q.place)) continue;
      status_counts[t.task.status]++;
      if (q.status.length && !q.status.includes(t.task.status)) continue;
      for (const tag of t.task.tags) tagCounts.set(tag, (tagCounts.get(tag) ?? 0) + 1);
      const p = planDoc(t);
      items.push({
        task: t.task,
        plan: p && progress(p),
        documents: t.docs.length,
        submitted_rounds: t.docs.flatMap((d) => d.rounds).filter((r) => r.state === 'submitted').length,
        draft_rounds: t.docs.flatMap((d) => d.rounds).filter((r) => r.state === 'draft').length,
        hits: hits ?? undefined,
      });
    }
    // Relevance stand-in: title matches first, then the rest by update time.
    const rel = (x: TaskListItem) => (x.hits?.[0]?.kind === 'task' ? 1 : 0);
    items.sort((a, b) =>
      q.sort === 'relevance' && rel(a) !== rel(b)
        ? rel(b) - rel(a)
        : q.sort === 'created'
          ? b.task.created_at.localeCompare(a.task.created_at)
          : b.task.updated_at.localeCompare(a.task.updated_at),
    );
    const tag_counts = [...tagCounts].map(([tag, count]) => ({ tag, count })).sort((a, b) => b.count - a.count || a.tag.localeCompare(b.tag));
    return reply({ items, status_counts, tag_counts });
  },

  getTask(id: string, machine?: string): Promise<TaskDetail> {
    const t = findTask(id);
    if (!t) return fail(`task ${id} not found`);
    const p = planDoc(t);
    const rounds: PendingRound[] = [];
    for (const d of t.docs)
      for (const r of d.rounds)
        if (r.state !== 'addressed')
          rounds.push({ id: r.id, slug: d.slug, doc_title: d.title, n: r.n, revision: r.revision, state: r.state, comments: r.comments.length });
    return reply({
      task: forMachine(t.task, machine),
      url: absolute(paths.task(t.task.id)),
      workspace: `~/agent-work/${t.task.id}`,
      documents: t.docs.map(summarize),
      plan: p && { slug: p.slug, ...progress(p)!, items: itemsFor(p, p.revisions.length) },
      journal: t.journal,
      rounds,
      todos: todos.filter((x) => x.task === t.task.id),
    });
  },

  createTask(n: NewTask): Promise<Task> {
    if (!n.title.trim()) return fail('title is required');
    const used = (n.from_todos ?? []).map((id) => todos.find((x) => x.id === id));
    if (used.some((x) => !x || x.state !== 'open')) return fail('from_todos takes only open todos');
    const id = `T-${++taskSeq}`;
    const placeKeys = [...new Set(used.flatMap((x) => x!.places.map((p) => p.key)))];
    const rec: TaskRec = {
      task: {
        id,
        title: n.title.trim(),
        summary: n.summary?.trim() || undefined,
        status: n.status ?? 'active',
        tags: n.tags ?? [],
        places: placeKeys.map((k) => ({ ...places.get(k)!, role: 'touched' as const })),
        created_at: now(),
        updated_at: now(),
      },
      docs: [],
      journal: [],
    };
    for (const x of used) {
      x!.state = 'in_task';
      x!.task = id;
      x!.updated_at = now();
    }
    if (used.length)
      rec.journal.push({ id: nextId(), kind: 'log', source: 'owner', created_at: now(), body: `Created from todos: ${used.map((x) => `${x!.id} ${x!.text}`).join('; ')}` });
    db.push(rec);
    return reply(rec.task, 200);
  },

  updateTask(id: string, patch: TaskPatch): Promise<Task> {
    const t = findTask(id);
    if (!t) return fail(`task ${id} not found`);
    const task = t.task;
    if (patch.status === 'archived' && !patch.closed_reason) return fail('archiving needs closed_reason');
    if (patch.title !== undefined) {
      if (!patch.title.trim()) return fail('title cannot be empty');
      task.title = patch.title.trim();
    }
    if (patch.summary !== undefined) task.summary = patch.summary.trim() || undefined;
    if (patch.status) {
      task.status = patch.status;
      task.waiting_on = patch.status === 'waiting' ? patch.waiting_on || task.waiting_on : undefined;
      task.closed_reason = patch.status === 'archived' ? patch.closed_reason : undefined;
      task.archived_at = patch.status === 'archived' ? now() : undefined;
    }
    if (patch.tags_add) task.tags = [...new Set([...task.tags, ...patch.tags_add])];
    if (patch.tags_remove) task.tags = task.tags.filter((x) => !patch.tags_remove!.includes(x));
    if (patch.places_remove) task.places = task.places.filter((p) => !patch.places_remove!.includes(p.key));
    for (const pa of patch.places_add ?? []) {
      if (!places.has(pa.place)) places.set(pa.place, { key: pa.place, kind: pa.place.includes(':/') ? 'path' : 'git', label: pa.place.split('/').pop()!, locations: [] });
      task.places = task.places.filter((p) => p.key !== pa.place);
      task.places.push({ ...places.get(pa.place)!, role: pa.role ?? 'touched', note: pa.note });
    }
    touch(t);
    return reply(task);
  },

  listPlaces(): Promise<PlaceSummary[]> {
    const out = [...places.values()].map((p) => ({
      ...p,
      tasks: db.filter((t) => t.task.status !== 'archived' && t.task.places.some((x) => x.key === p.key)).length,
      open_todos: todos.filter((x) => x.state === 'open' && x.places.some((y) => y.key === p.key)).length,
    }));
    return reply(out.sort((a, b) => b.tasks + b.open_todos - (a.tasks + a.open_todos) || a.label.localeCompare(b.label)));
  },

  listTodos(q: TodoQuery): Promise<TodoListResult> {
    const state_counts: Record<TodoState, number> = { open: 0, in_task: 0, done: 0, dropped: 0 };
    const tagCounts = new Map<string, number>();
    const ts = q.q?.trim() ? terms(q.q) : [];
    const out: Todo[] = [];
    for (const x of todos) {
      if (ts.length && hitsOf([{ kind: 'todo', text: `${x.text} ${x.note ?? ''}`, path: paths.todo(x.id) }], ts) === null) continue;
      if (!placeMatch(x.places.map((p) => p.key), q.place)) continue;
      if (!q.tags.every((tag) => x.tags.includes(tag))) continue;
      state_counts[x.state]++;
      if (q.state.length && !q.state.includes(x.state)) continue;
      x.tags.forEach((tag) => tagCounts.set(tag, (tagCounts.get(tag) ?? 0) + 1));
      out.push(x);
    }
    out.sort((a, b) => b.created_at.localeCompare(a.created_at));
    const tag_counts = [...tagCounts].map(([tag, count]) => ({ tag, count })).sort((a, b) => b.count - a.count || a.tag.localeCompare(b.tag));
    return reply({ total: out.length, todos: out, state_counts, tag_counts });
  },

  async bulkUpdateTasks(ids, patch) {
    if (patch.status === 'archived' && !patch.closed_reason) return fail('archiving needs closed_reason');
    for (const id of ids) await fakeApi.updateTask(id, patch);
  },

  async bulkUpdateTodos(ids, state) {
    for (const id of ids) {
      const x = todos.find((t) => t.id === id);
      if (x && x.state !== 'in_task') await fakeApi.updateTodo(id, { state });
    }
  },

  writeDocument(taskId, slug, body, base, message) {
    const t = findTask(taskId);
    const d = t && findDoc(t, slug);
    if (!t || !d) return fail(`document ${taskId}/${slug} not found`);
    const prevN = d.revisions.length;
    if (base !== prevN) return Promise.reject(new ConflictError(`revision ${prevN} appeared while you were editing r${base}`, prevN));
    if (body === d.revisions[prevN - 1].body) return reply({ n: prevN });
    const n = prevN + 1;
    d.revisions.push({ n, body, author: 'owner', message: message?.trim() || undefined, created_at: now() });
    // Plan items: match the new task lines to the previous revision's items.
    const prev = d.items.filter((it) => it.at.has(prevN)).sort((a, b) => a.at.get(prevN)!.line - b.at.get(prevN)!.line);
    const lines = taskLines(body);
    const match = matchItems(prev.map((p) => p.at.get(prevN)!.text), lines.map((l) => l.text));
    lines.forEach((l, i) => {
      let rec = match[i] >= 0 ? prev[match[i]] : undefined;
      if (!rec) {
        rec = { id: `i${d.items.length + 1}`, done: l.checked, done_by: l.checked ? 'owner' : undefined, notes: [], at: new Map() };
        d.items.push(rec);
      }
      rec.at.set(n, { line: l.line, text: l.text });
    });
    // Comments: carry open and resolved ones to the new revision.
    for (const r of d.rounds)
      for (const c of r.comments) {
        if (!c.anchor || c.outdated_in || r.state === 'draft') continue;
        if ((c.current?.revision ?? c.revision) !== prevN) continue;
        const from = c.current ? { ...c.anchor, ...c.current } : c.anchor;
        const a = relocate(body, from);
        if (a) c.current = { revision: n, quote: a.quote, start: a.start, end: a.end };
        else {
          c.outdated_in = n;
          if (c.state === 'open') c.state = 'outdated';
        }
      }
    touch(t);
    return reply({ n });
  },

  revertDocument(taskId, slug, to, base) {
    const t = findTask(taskId);
    const d = t && findDoc(t, slug);
    if (!t || !d) return fail(`document ${taskId}/${slug} not found`);
    const r = d.revisions[to - 1];
    if (!r) return fail(`revision ${to} does not exist`);
    return fakeApi.writeDocument(taskId, slug, r.body, base, `revert to r${to}`);
  },

  listTags() {
    const m = new Map<string, number>();
    for (const t of db) t.task.tags.forEach((x) => m.set(x, (m.get(x) ?? 0) + 1));
    for (const x of todos) x.tags.forEach((g) => m.set(g, (m.get(g) ?? 0) + 1));
    return reply([...m].map(([tag, count]) => ({ tag, count })).sort((a, b) => b.count - a.count || a.tag.localeCompare(b.tag)));
  },

  createTodo(n: NewTodo): Promise<Todo> {
    if (!n.text.trim()) return fail('text is required');
    for (const k of n.places ?? []) {
      if (!places.has(k)) places.set(k, { key: k, kind: k.includes(':/') ? 'path' : 'git', label: k.split('/').pop()!, locations: [] });
    }
    const x: Todo = {
      id: `D-${todos.length + 1}`,
      text: n.text.trim(),
      note: n.note?.trim() || undefined,
      state: 'open',
      places: (n.places ?? []).map(placeRef),
      tags: n.tags ?? [],
      source: 'owner',
      created_at: now(),
      updated_at: now(),
    };
    todos.push(x);
    return reply(x);
  },

  setItem(taskId, slug, itemId, change) {
    const t = findTask(taskId);
    const d = t && findDoc(t, slug);
    const it = d?.items.find((x) => x.id === itemId);
    if (!t || !it) return fail(`item ${itemId} of ${taskId}/${slug} not found`);
    if (change.done !== undefined && change.done !== it.done) {
      it.done = change.done;
      it.done_by = change.done ? 'owner' : undefined;
      it.done_at = change.done ? now() : undefined;
    }
    if (change.note?.trim()) it.notes = [...it.notes, { body: change.note.trim(), author: 'owner', created_at: now() }];
    touch(t);
    return reply(undefined);
  },

  getTodo(id: string): Promise<Todo> {
    const x = todos.find((t) => t.id.toLowerCase() === id.toLowerCase());
    return x ? reply(x) : fail(`todo ${id} not found`);
  },

  updateTodo(id, patch): Promise<Todo> {
    const x = todos.find((t) => t.id === id);
    if (!x) return fail(`todo ${id} not found`);
    if (patch.text !== undefined) x.text = patch.text;
    if (patch.note !== undefined) x.note = patch.note || undefined;
    if (patch.places_remove) x.places = x.places.filter((p) => !patch.places_remove!.includes(p.key));
    if (patch.tags_add) x.tags = [...new Set([...x.tags, ...patch.tags_add])];
    if (patch.tags_remove) x.tags = x.tags.filter((g) => !patch.tags_remove!.includes(g));
    for (const k of patch.places_add ?? []) {
      if (!places.has(k)) places.set(k, { key: k, kind: k.includes(':/') ? 'path' : 'git', label: k.split('/').pop()!, locations: [] });
      if (!x.places.some((p) => p.key === k)) x.places.push(placeRef(k));
    }
    if (patch.state) {
      x.state = patch.state;
      x.closed_at = patch.state === 'open' ? undefined : now();
      if (patch.state === 'open') x.task = undefined;
    }
    x.updated_at = now();
    return reply(x);
  },

  getDocument(taskId: string, slug: string, revision?: number): Promise<DocumentView> {
    const t = findTask(taskId);
    const d = t && findDoc(t, slug);
    if (!t || !d) return fail(`document ${taskId}/${slug} not found`);
    const latest = d.revisions.length;
    const n = revision ?? latest;
    if (n < 1 || n > latest) return fail(`revision ${n} of ${taskId}/${slug} does not exist (latest is ${latest})`);
    return reply({
      task: t.task,
      slug: d.slug,
      kind: d.kind,
      title: d.title,
      latest,
      revision: d.revisions[n - 1],
      revisions: d.revisions.map(({ body: _b, ...m }) => m).reverse(),
      items: itemsFor(d, n),
      rounds: [...d.rounds].reverse(),
    });
  },

  getDiff(taskId: string, slug: string, from: number, to: number): Promise<DiffView> {
    const t = findTask(taskId);
    const d = t && findDoc(t, slug);
    if (!t || !d) return fail(`document ${taskId}/${slug} not found`);
    const latest = d.revisions.length;
    if (from < 1 || to > latest || from >= to) return fail(`cannot diff r${from}..r${to} (latest is ${latest})`);
    return reply({
      task: t.task,
      slug: d.slug,
      title: d.title,
      latest,
      revisions: d.revisions.map(({ body: _b, ...m }) => m).reverse(),
      from: d.revisions[from - 1],
      to: d.revisions[to - 1],
      rounds: [...d.rounds].reverse().filter((r) => r.revision >= from && r.revision < to && r.state !== 'draft'),
    });
  },

  addDraftComment(taskId, slug, c): Promise<Comment> {
    const t = findTask(taskId);
    const d = t && findDoc(t, slug);
    if (!t || !d) return fail(`document ${taskId}/${slug} not found`);
    let r = d.rounds.find((x) => x.state === 'draft');
    if (!r) {
      r = { id: nextId(), n: d.rounds.length + 1, revision: d.revisions.length, state: 'draft', created_at: now(), comments: [] };
      d.rounds.push(r);
    }
    const comment: Comment = { id: nextId(), round: r.id, revision: r.revision, anchor: c.anchor, body: c.body, state: 'open', replies: [], created_at: now() };
    r.comments.push(comment);
    return reply(comment);
  },

  updateDraftComment(id, body) {
    const h = allComments().find((x) => x.c.id === id);
    if (!h || h.r.state !== 'draft') return fail('only draft comments can be edited');
    h.c.body = body;
    return reply(undefined);
  },

  deleteDraftComment(id) {
    const h = allComments().find((x) => x.c.id === id);
    if (!h || h.r.state !== 'draft') return fail('only draft comments can be deleted');
    h.r.comments = h.r.comments.filter((c) => c.id !== id);
    if (!h.r.comments.length && !h.r.summary) h.d.rounds = h.d.rounds.filter((r) => r !== h.r);
    return reply(undefined);
  },

  setRoundSummary(roundId, summary) {
    const f = findRound(roundId);
    if (!f || f.r.state !== 'draft') return fail('only a draft round takes a summary');
    f.r.summary = summary || undefined;
    return reply(undefined);
  },

  submitRound(roundId) {
    const f = findRound(roundId);
    if (!f || f.r.state !== 'draft') return fail('draft round not found');
    if (!f.r.comments.length && !f.r.summary) return fail('nothing to submit');
    f.r.state = 'submitted';
    f.r.submitted_at = now();
    // A draft made on an older revision is carried to the latest one on submit.
    for (const c of f.r.comments) carry(c, f.d.revisions);
    touch(f.t);
    return reply(undefined, 250);
  },

  discardRound(roundId) {
    const f = findRound(roundId);
    if (!f || f.r.state !== 'draft') return fail('draft round not found');
    f.d.rounds = f.d.rounds.filter((x) => x !== f.r);
    return reply(undefined);
  },

  reply(commentId, body) {
    const h = allComments().find((x) => x.c.id === commentId);
    if (!h) return fail('comment not found');
    h.c.replies.push({ id: nextId(), author: 'owner', body, created_at: now() });
    touch(h.t);
    return reply(undefined);
  },

  setCommentState(commentId, state) {
    const h = allComments().find((x) => x.c.id === commentId);
    if (!h) return fail('comment not found');
    h.c.state = state;
    touch(h.t);
    return reply(undefined);
  },
};
