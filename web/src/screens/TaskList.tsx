import { useEffect, useMemo, useRef, useState } from 'react';
import { api, useQuery, type ClosedReason, type PlaceSummary, type SearchHit, type SortKey, type TaskListItem, type TaskStatus } from '../api';
import { Icon } from '../components/Icon';
import { PlaceFacet } from '../components/PlaceFacet';
import { Empty, ErrorView, PlaceChip, Progress, StatusBadge, Tag, Time, tagParts } from '../components/ui';
import { useI18n } from '../i18n';
import { href, navigate, paths } from '../router';

const STATUSES: TaskStatus[] = ['active', 'waiting', 'later', 'archived'];
const DEFAULT_STATUS: TaskStatus[] = ['active', 'waiting', 'later'];
const SORTS: SortKey[] = ['updated', 'created', 'relevance'];
const NS_ORDER = ['repo', 'topic', 'area', ''];

interface Filters {
  q: string;
  status: TaskStatus[];
  tags: string[];
  place?: string;
  sort: SortKey;
}

function readQuery(q: URLSearchParams): Filters {
  const st = q.get('status');
  const status = st === null ? DEFAULT_STATUS : (st.split(',').filter((s) => STATUSES.includes(s as TaskStatus)) as TaskStatus[]);
  const sort = SORTS.includes(q.get('sort') as SortKey) ? (q.get('sort') as SortKey) : 'updated';
  return { q: q.get('q') ?? '', status, tags: q.getAll('tag'), place: q.get('place') ?? undefined, sort };
}

function writeQuery(f: Filters): URLSearchParams {
  const q = new URLSearchParams();
  if (f.q) q.set('q', f.q);
  if (f.status.join(',') !== DEFAULT_STATUS.join(',')) q.set('status', f.status.join(','));
  if (f.place) q.set('place', f.place);
  f.tags.forEach((t) => q.append('tag', t));
  if (f.sort !== 'updated') q.set('sort', f.sort);
  return q;
}

function Snippet({ text }: { text: string }) {
  const parts = text.split(/\[\[|\]\]/);
  return (
    <>
      {parts.map((p, i) => (i % 2 ? <mark key={i}>{p}</mark> : <span key={i}>{p}</span>))}
    </>
  );
}

/** What a search matched, each line a link straight to the matched object. */
export function Hits({ hits, title }: { hits: SearchHit[]; title?: string }) {
  const { t } = useI18n();
  const shown = hits.filter((h) => !(h.kind === 'task' && h.text.replace(/\[\[|\]\]/g, '') === title)).slice(0, 3);
  if (!shown.length) return null;
  return (
    <ul className="hits">
      {shown.map((h, i) => (
        <li key={i}>
          <a className="hit" href={href(h.path)}>
            <span className={`hit-kind hit-${h.kind}`}>{t(`hit.${h.kind}`)}</span>
            {h.label && <span className="hit-label">{h.label}</span>}
            <span className="hit-text">
              <Snippet text={h.text} />
            </span>
          </a>
        </li>
      ))}
    </ul>
  );
}

function Row({
  item,
  selectedTags,
  onTag,
  checked,
  onCheck,
}: {
  item: TaskListItem;
  selectedTags: string[];
  onTag: (t: string) => void;
  checked: boolean;
  onCheck: () => void;
}) {
  const { t, count } = useI18n();
  const task = item.task;
  return (
    <li className={`row-item${checked ? ' selected' : ''}`}>
      <label className="row-check" title={t('todo.select', { x: task.id })}>
        <input type="checkbox" checked={checked} onChange={onCheck} aria-label={t('todo.select', { x: task.id })} />
        <span className={`check${checked ? ' on' : ''}`} aria-hidden="true">
          {checked && <Icon name="check" size={11} />}
        </span>
      </label>
    <div className={`row is-${task.status}`}>
      <a className="row-link" href={href(paths.task(task.id))}>
        <span className="row-id mono">{task.id}</span>
        <span className="row-title">{task.title}</span>
      </a>
      <div className="row-status">
        <StatusBadge task={task} />
        {task.status === 'waiting' && task.waiting_on && <span className="row-waiting">{t('waiting.on', { x: task.waiting_on })}</span>}
      </div>
      <div className="row-time">
        <Time iso={task.updated_at} />
      </div>
      {item.hits?.length ? (
        <div className="row-snippet">
          <Hits hits={item.hits} title={task.title} />
        </div>
      ) : task.summary ? (
        <p className="row-summary">{task.summary.replace(/[`*_]/g, '')}</p>
      ) : null}
      <div className="row-meta">
        {(task.tags.length > 0 || task.places.length > 0) && (
          <span className="tags">
            {task.places.map((p) => (
              <PlaceChip key={p.key} place={p} link="tasks" />
            ))}
            {task.tags.map((tag) => (
              <Tag key={tag} tag={tag} active={selectedTags.includes(tag)} onClick={() => onTag(tag)} />
            ))}
          </span>
        )}
        <span className="row-facts">
          {item.plan && <Progress done={item.plan.done} total={item.plan.total} />}
          {item.submitted_rounds > 0 && (
            <span className="fact fact-submitted">
              <Icon name="comment" size={13} />
              {t('list.reviewSubmitted')}
            </span>
          )}
          {item.draft_rounds > 0 && (
            <span className="fact fact-draft">
              <Icon name="edit" size={13} />
              {t('list.reviewDraft')}
            </span>
          )}
          {item.documents > 0 && (
            <span className="fact">
              <Icon name="doc" size={13} />
              {count(item.documents, 'doc')}
            </span>
          )}
        </span>
      </div>
    </div>
    </li>
  );
}

/** Status change for the selected tasks. */
function BulkTaskBar({ ids, onDone }: { ids: string[]; onDone: () => void }) {
  const { t } = useI18n();
  const [mode, setMode] = useState<'waiting' | 'archived' | null>(null);
  const [waitingOn, setWaitingOn] = useState('');
  const [error, setError] = useState('');
  const run = async (patch: { status: TaskStatus; waiting_on?: string; closed_reason?: ClosedReason }) => {
    setError('');
    try {
      await api.bulkUpdateTasks(ids, patch);
      setMode(null);
      onDone();
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <div className="selbar selbar-tasks" role="toolbar" aria-label={t('bulk.title')}>
      <span className="selbar-n">{t('todo.selected', { n: ids.length })}</span>
      <span className="selbar-actions">
        <span className="muted small">{t('bulk.setStatus')}</span>
        <button type="button" className="btn btn-sm" onClick={() => run({ status: 'active' })}>
          <StatusBadge status="active" />
        </button>
        <button type="button" className={`btn btn-sm${mode === 'waiting' ? ' on' : ''}`} onClick={() => setMode(mode === 'waiting' ? null : 'waiting')}>
          <StatusBadge status="waiting" />
        </button>
        <button type="button" className="btn btn-sm" onClick={() => run({ status: 'later' })}>
          <StatusBadge status="later" />
        </button>
        <button type="button" className={`btn btn-sm${mode === 'archived' ? ' on' : ''}`} onClick={() => setMode(mode === 'archived' ? null : 'archived')}>
          <StatusBadge status="archived" />
        </button>
        <button type="button" className="link-btn" onClick={onDone}>
          {t('todo.clearSel')}
        </button>
      </span>
      {mode === 'waiting' && (
        <form
          className="selbar-form"
          onSubmit={(e) => {
            e.preventDefault();
            run({ status: 'waiting', waiting_on: waitingOn.trim() || undefined });
          }}
        >
          <input autoFocus value={waitingOn} onChange={(e) => setWaitingOn(e.target.value)} placeholder={t('task.waitingOnPh')} aria-label={t('task.waitingOn')} />
          <button type="submit" className="btn btn-sm btn-primary">
            {t('task.apply')}
          </button>
        </form>
      )}
      {mode === 'archived' && (
        <div className="selbar-form">
          <span className="muted small">{t('task.closedReason')}</span>
          <button type="button" className="btn btn-sm btn-primary" onClick={() => run({ status: 'archived', closed_reason: 'done' })}>
            {t('closed.done')}
          </button>
          <button type="button" className="btn btn-sm" onClick={() => run({ status: 'archived', closed_reason: 'cancelled' })}>
            {t('closed.cancelled')}
          </button>
        </div>
      )}
      {error && <p className="notice notice-warn">{error}</p>}
    </div>
  );
}

/** Todos matching the search, under the tasks. */
function TodoHits({ q, place }: { q: string; place?: string }) {
  const { t, count } = useI18n();
  const res = useQuery(`todo-hits:${q}:${place ?? ''}`, () => api.listTodos({ q, place, state: ['open', 'in_task', 'done', 'dropped'], tags: [] }));
  const todos = res.data?.todos ?? [];
  if (!todos.length) return null;
  return (
    <section className="todo-hits">
      <h2 className="side-title">
        {t('nav.todos')} · {count(todos.length, 'todo')}
      </h2>
      <ul className="rows">
        {todos.slice(0, 10).map((x) => (
          <li key={x.id} className="row-item">
            <a className="hit hit-todo-row" href={href(paths.todo(x.id))}>
              <span className="hit-kind hit-todo">{t('hit.todo')}</span>
              <span className="mono todo-id">{x.id}</span>
              <span className="hit-text">{x.text}</span>
              {x.state !== 'open' && <span className={`tstate tstate-${x.state}`}>{t(`todo.is.${x.state}`)}</span>}
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}

function FilterPanel({
  f,
  places,
  counts,
  tagCounts,
  set,
}: {
  f: Filters;
  counts: Record<TaskStatus, number> | undefined;
  tagCounts: { tag: string; count: number }[];
  places: PlaceSummary[];
  set: (patch: Partial<Filters>) => void;
}) {
  const { t } = useI18n();
  const [find, setFind] = useState('');
  const groups = useMemo(() => {
    const all = new Map(tagCounts.map((x) => [x.tag, x.count]));
    f.tags.forEach((tag) => all.has(tag) || all.set(tag, 0));
    const g = new Map<string, { tag: string; count: number }[]>();
    for (const [tag, count] of all) {
      if (find && !tag.toLowerCase().includes(find.toLowerCase())) continue;
      const ns = NS_ORDER.includes(tagParts(tag).ns) ? tagParts(tag).ns : '';
      if (!g.has(ns)) g.set(ns, []);
      g.get(ns)!.push({ tag, count });
    }
    return NS_ORDER.filter((ns) => g.has(ns)).map((ns) => [ns, g.get(ns)!] as const);
  }, [tagCounts, f.tags, find]);
  const toggleStatus = (s: TaskStatus) =>
    set({ status: f.status.includes(s) ? f.status.filter((x) => x !== s) : STATUSES.filter((x) => x === s || f.status.includes(x)) });

  return (
    <div className="filters">
      <div className="filter-group">
        <div className="filter-head">{t('new.status')}</div>
        <div className="status-list" role="group">
          {STATUSES.map((s) => (
            <button
              key={s}
              type="button"
              className={`status-filter${f.status.includes(s) ? ' on' : ''}`}
              aria-pressed={f.status.includes(s)}
              onClick={() => toggleStatus(s)}
            >
              <span className={`check${f.status.includes(s) ? ' on' : ''}`} aria-hidden="true">
                {f.status.includes(s) && <Icon name="check" size={11} />}
              </span>
              <StatusBadge status={s} />
              <span className="count">{counts?.[s] ?? ''}</span>
            </button>
          ))}
        </div>
      </div>
      <PlaceFacet places={places} value={f.place} onChange={(place) => set({ place })} count={(p) => p.tasks} />
      <div className="filter-group">
        <div className="filter-head">{t('list.tags')}</div>
        <label className="tag-find">
          <Icon name="search" size={13} />
          <input value={find} onChange={(e) => setFind(e.target.value)} placeholder={t('list.tagsFind')} aria-label={t('list.tagsFind')} />
        </label>
        {groups.map(([ns, tags]) => (
          <div key={ns} className="tag-group">
            <div className="tag-group-name">{ns ? `${ns}:` : t('tags.other')}</div>
            <ul>
              {tags.map(({ tag, count }) => {
                const on = f.tags.includes(tag);
                return (
                  <li key={tag}>
                    <button
                      type="button"
                      className={`tag-facet${on ? ' on' : ''}`}
                      aria-pressed={on}
                      title={tag}
                      onClick={() => set({ tags: on ? f.tags.filter((x) => x !== tag) : [...f.tags, tag] })}
                    >
                      <span className={`check${on ? ' on' : ''}`} aria-hidden="true">
                        {on && <Icon name="check" size={11} />}
                      </span>
                      <span className="tag-facet-name">{tagParts(tag).short}</span>
                      <span className="count">{count}</span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        ))}
      </div>
    </div>
  );
}

export function TaskList({ query }: { query: URLSearchParams }) {
  const { t, count } = useI18n();
  const f = readQuery(query);
  const [text, setText] = useState(f.q);
  const [showFilters, setShowFilters] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);

  const set = (patch: Partial<Filters>) => navigate(paths.list(writeQuery({ ...f, ...patch })), true);

  // Debounced search into the URL.
  useEffect(() => {
    if (text === f.q) return;
    const id = setTimeout(() => set({ q: text }), 200);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text]);

  // "/" focuses search.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === '/' && !(e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement)) {
        e.preventDefault();
        inputRef.current?.focus();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const key = JSON.stringify(f);
  const res = useQuery(key, () => api.listTasks(f));
  const placesQ = useQuery('places', () => api.listPlaces());
  if (res.error) return <ErrorView error={res.error} />;
  const data = res.data;
  const visibleIds = new Set((data?.items ?? []).map((x) => x.task.id));
  const sel = selected.filter((id) => visibleIds.has(id));
  const filtered = f.tags.length > 0 || f.status.join() !== DEFAULT_STATUS.join() || !!f.q || !!f.place;
  const toggleTag = (tag: string) => set({ tags: f.tags.includes(tag) ? f.tags.filter((x) => x !== tag) : [...f.tags, tag] });

  return (
    <div className="page list-page">
      <aside className={`list-side${showFilters ? ' open' : ''}`} aria-label={t('list.tagsFilter')}>
        <FilterPanel f={f} counts={data?.status_counts} tagCounts={data?.tag_counts ?? []} places={placesQ.data ?? []} set={set} />
      </aside>
      <section className="list-main">
        <h1 className="sr-only">{t('nav.tasks')}</h1>
        <div className="list-toolbar">
          <label className="search">
            <Icon name="search" size={16} />
            <input
              ref={inputRef}
              type="search"
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder={t('list.search')}
              aria-label={t('list.search')}
            />
            <kbd className="search-kbd">/</kbd>
          </label>
          <div className="list-controls">
            <button
              type="button"
              className={`btn btn-sm filters-toggle${showFilters ? ' on' : ''}`}
              onClick={() => setShowFilters((s) => !s)}
              aria-expanded={showFilters}
            >
              <Icon name="filter" size={14} />
              {t('list.tagsFilter')}
              {f.tags.length + (f.place ? 1 : 0) > 0 && <span className="pill">{f.tags.length + (f.place ? 1 : 0)}</span>}
            </button>
            <label className="sort">
              <span className="sort-label">{t('list.sort')}</span>
              <select value={f.sort} onChange={(e) => set({ sort: e.target.value as SortKey })}>
                {SORTS.map((s) => (
                  <option key={s} value={s}>
                    {t(`sort.${s}`)}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
        <div className="list-summary">
          <span className="muted">{data ? count(data.items.length, 'task') : ' '}</span>
          {f.place && (
            <Tag
              tag={f.place === 'none' ? t('place.none') : (placesQ.data?.find((p) => p.key === f.place)?.label ?? f.place)}
              active
              onRemove={() => set({ place: undefined })}
            />
          )}
          {f.tags.map((tag) => (
            <Tag key={tag} tag={tag} active onRemove={() => toggleTag(tag)} />
          ))}
          {filtered && (
            <button
              type="button"
              className="link-btn"
              onClick={() => {
                setText('');
                navigate(paths.list(), true);
              }}
            >
              {t('list.clear')}
            </button>
          )}
        </div>
        {sel.length > 0 && <BulkTaskBar ids={sel} onDone={() => setSelected([])} />}
        {data && data.items.length === 0 ? (
          <Empty title={t('list.empty')} hint={t('list.emptyHint')} />
        ) : (
          <ul className={`rows${res.loading ? ' stale' : ''}`}>
            {data?.items.map((it) => (
              <Row
                key={it.task.id}
                item={it}
                selectedTags={f.tags}
                onTag={toggleTag}
                checked={sel.includes(it.task.id)}
                onCheck={() => setSelected((s) => (s.includes(it.task.id) ? s.filter((x) => x !== it.task.id) : [...s, it.task.id]))}
              />
            ))}
          </ul>
        )}
        {f.q.trim() && <TodoHits q={f.q.trim()} place={f.place} />}
      </section>
    </div>
  );
}
