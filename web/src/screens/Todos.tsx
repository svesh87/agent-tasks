import { useEffect, useRef, useState } from 'react';
import { api, useQuery, type PlaceSummary, type Todo, type TodoState } from '../api';
import { Icon } from '../components/Icon';
import { InlineMarkdown, SmallMarkdown } from '../components/Markdown';
import { PlaceFacet } from '../components/PlaceFacet';
import { PlacePicker } from '../components/PlacePicker';
import { TagInput } from '../components/TagInput';
import { Empty, ErrorView, Loading, PlaceChip, Tag, TaskRef, Time } from '../components/ui';
import { useI18n } from '../i18n';
import { href, navigate, paths } from '../router';

const STATES: TodoState[] = ['open', 'in_task', 'done', 'dropped'];

interface Filters {
  q: string;
  state: TodoState[];
  tags: string[];
  place?: string;
}

function readQuery(q: URLSearchParams): Filters {
  const st = q.get('state');
  return {
    q: q.get('q') ?? '',
    state: st === null ? ['open'] : (st.split(',').filter((s) => STATES.includes(s as TodoState)) as TodoState[]),
    tags: q.getAll('tag'),
    place: q.get('place') ?? undefined,
  };
}
function writeQuery(f: Filters): URLSearchParams {
  const q = new URLSearchParams();
  if (f.q) q.set('q', f.q);
  if (f.state.join(',') !== 'open') q.set('state', f.state.join(','));
  if (f.place) q.set('place', f.place);
  f.tags.forEach((t) => q.append('tag', t));
  return q;
}

export function TodoStateBadge({ state, plural }: { state: TodoState; plural?: boolean }) {
  const { t } = useI18n();
  return <span className={`tstate tstate-${state}`}>{t(plural ? `todo.state.${state}` : `todo.is.${state}`)}</span>;
}

function TodoRow({ x, selected, onToggle }: { x: Todo; selected: boolean; onToggle: () => void }) {
  const { t } = useI18n();
  const selectable = x.state !== 'in_task';
  return (
    <li className={`todo-row${selected ? ' selected' : ''} is-${x.state}`}>
      <label className={`todo-check${selectable ? '' : ' disabled'}`}>
        <input type="checkbox" checked={selected} disabled={!selectable} onChange={onToggle} aria-label={t('todo.select', { x: x.id })} />
        <span className={`check${selected ? ' on' : ''}`} aria-hidden="true">
          {selected && <Icon name="check" size={11} />}
        </span>
      </label>
      <div className="todo-main">
        <a className="todo-link" href={href(paths.todo(x.id))}>
          <span className="todo-id mono">{x.id}</span>
          <span className="todo-text">
            <InlineMarkdown text={x.text} />
          </span>
        </a>
        <div className="todo-meta">
          {x.state !== 'open' && <TodoStateBadge state={x.state} />}
          {x.task && (
            <span className="todo-link-task">
              {t('todo.inTask')} <TaskRef id={x.task} />
            </span>
          )}
          {x.places.map((p) => (
            <PlaceChip key={p.key} place={p} link="todos" />
          ))}
          {x.tags.map((tag) => (
            <Tag key={tag} tag={tag} link={false} />
          ))}
          {x.origin_task && (
            <span className="origin">
              {t('todo.from')} <TaskRef id={x.origin_task} />
            </span>
          )}
          <span className="todo-time">
            <Time iso={x.created_at} />
          </span>
        </div>
      </div>
    </li>
  );
}

function NewTaskDialog({ todos, onClose }: { todos: Todo[]; onClose: () => void }) {
  const { t } = useI18n();
  const [title, setTitle] = useState(todos.length === 1 ? todos[0].text : '');
  const [summary, setSummary] = useState(todos.length > 1 ? todos.map((x) => `- ${x.text}`).join('\n') : (todos[0]?.note ?? ''));
  const [error, setError] = useState('');
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  const placeSet = new Map(todos.flatMap((x) => x.places).map((p) => [p.key, p]));
  return (
    <dialog ref={dialog} className="dialog" onClose={onClose} onCancel={onClose} aria-labelledby="new-task-title">
      <form
        method="dialog"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            const task = await api.createTask({ title, summary, from_todos: todos.map((x) => x.id) });
            onClose();
            navigate(paths.task(task.id));
          } catch (err) {
            setError((err as Error).message);
          }
        }}
      >
        <h2 id="new-task-title">{t('new.title')}</h2>
        <label className="field">
          <span>{t('new.taskTitle')}</span>
          <input value={title} onChange={(e) => setTitle(e.target.value)} required autoFocus />
        </label>
        <label className="field">
          <span>{t('new.summary')}</span>
          <textarea rows={4} value={summary} onChange={(e) => setSummary(e.target.value)} />
        </label>
        <div className="field">
          <span>{t('new.from')}</span>
          <ul className="mini-todos">
            {todos.map((x) => (
              <li key={x.id}>
                <span className="mono todo-id">{x.id}</span>
                <span>
                  <InlineMarkdown text={x.text} />
                </span>
              </li>
            ))}
          </ul>
        </div>
        {placeSet.size > 0 && (
          <div className="field">
            <span className="tags">
              {[...placeSet.values()].map((p) => (
                <PlaceChip key={p.key} place={p} />
              ))}
            </span>
            <span className="small">{t('new.placesNote')}</span>
          </div>
        )}
        {error && <p className="notice notice-warn">{error}</p>}
        <div className="form-actions">
          <button type="button" className="btn btn-ghost" onClick={onClose}>
            {t('task.cancel')}
          </button>
          <button type="submit" className="btn btn-primary" disabled={!title.trim()}>
            {t('new.create')}
          </button>
        </div>
      </form>
    </dialog>
  );
}

export function TodoList({ query }: { query: URLSearchParams }) {
  const { t, count } = useI18n();
  const f = readQuery(query);
  const [text, setText] = useState(f.q);
  const [selected, setSelected] = useState<string[]>([]);
  const [dialog, setDialog] = useState(false);
  const [newTodo, setNewTodo] = useState<string | null>(null);
  const [showFilters, setShowFilters] = useState(false);
  const set = (patch: Partial<Filters>) => navigate(paths.todos(writeQuery({ ...f, ...patch })), true);
  useEffect(() => {
    if (text === f.q) return;
    const id = setTimeout(() => set({ q: text }), 200);
    return () => clearTimeout(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text]);
  const res = useQuery(`todos:${JSON.stringify(f)}`, () => api.listTodos(f));
  const places = useQuery('places', () => api.listPlaces());
  if (res.error) return <ErrorView error={res.error} />;
  const data = res.data;
  const visible = new Set((data?.todos ?? []).filter((x) => x.state !== 'in_task').map((x) => x.id));
  const sel = selected.filter((id) => visible.has(id));
  const selTodos = (data?.todos ?? []).filter((x) => sel.includes(x.id));
  const toggleState = (s: TodoState) => set({ state: f.state.includes(s) ? f.state.filter((x) => x !== s) : STATES.filter((x) => x === s || f.state.includes(x)) });
  const filtered = f.place || f.tags.length || f.q || f.state.join() !== 'open';

  return (
    <div className="page list-page todos-page">
      <aside className={`list-side${showFilters ? ' open' : ''}`}>
        <div className="filters">
          <div className="filter-group">
            <div className="filter-head">{t('todo.states')}</div>
            <div className="status-list">
              {STATES.map((s) => (
                <button key={s} type="button" className={`status-filter${f.state.includes(s) ? ' on' : ''}`} aria-pressed={f.state.includes(s)} onClick={() => toggleState(s)}>
                  <span className={`check${f.state.includes(s) ? ' on' : ''}`} aria-hidden="true">
                    {f.state.includes(s) && <Icon name="check" size={11} />}
                  </span>
                  <TodoStateBadge state={s} plural />
                  <span className="count">{data?.state_counts[s] ?? ''}</span>
                </button>
              ))}
            </div>
          </div>
          <PlaceFacet places={places.data ?? []} value={f.place} onChange={(place) => set({ place })} count={(p) => p.open_todos} />
          {(data?.tag_counts.length ?? 0) > 0 && (
            <div className="filter-group">
              <div className="filter-head">{t('list.tags')}</div>
              <ul className="facet-list">
                {data!.tag_counts.map(({ tag, count: n }) => {
                  const on = f.tags.includes(tag);
                  return (
                    <li key={tag}>
                      <button type="button" className={`tag-facet${on ? ' on' : ''}`} aria-pressed={on} onClick={() => set({ tags: on ? f.tags.filter((x) => x !== tag) : [...f.tags, tag] })}>
                        <span className={`check${on ? ' on' : ''}`} aria-hidden="true">
                          {on && <Icon name="check" size={11} />}
                        </span>
                        <span className="tag-facet-name">{tag}</span>
                        <span className="count">{n}</span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          )}
        </div>
      </aside>
      <section className="list-main">
        <h1 className="sr-only">{t('nav.todos')}</h1>
        <div className="list-toolbar">
          <label className="search">
            <Icon name="search" size={16} />
            <input type="search" value={text} onChange={(e) => setText(e.target.value)} placeholder={t('todo.search')} aria-label={t('todo.search')} />
          </label>
          <div className="list-controls">
            <button type="button" className={`btn btn-sm filters-toggle${showFilters ? ' on' : ''}`} onClick={() => setShowFilters((s) => !s)} aria-expanded={showFilters}>
              <Icon name="filter" size={14} />
              {t('list.tagsFilter')}
            </button>
            <button type="button" className="btn btn-sm btn-primary" onClick={() => setNewTodo('')}>
              <Icon name="plus" size={14} />
              {t('todo.new')}
            </button>
          </div>
        </div>
        <QuickAdd
          defaultPlaces={f.place && f.place !== 'none' ? [f.place] : []}
          onMore={(text) => setNewTodo(text)}
        />
        <div className="list-summary">
          <span className="muted">{data ? count(data.total, 'todo') : ' '}</span>
          {f.place && (
            <Tag tag={f.place === 'none' ? t('place.none') : (places.data?.find((p) => p.key === f.place)?.label ?? f.place)} active onRemove={() => set({ place: undefined })} />
          )}
          {f.tags.map((tag) => (
            <Tag key={tag} tag={tag} active onRemove={() => set({ tags: f.tags.filter((x) => x !== tag) })} />
          ))}
          {filtered && (
            <button
              type="button"
              className="link-btn"
              onClick={() => {
                setText('');
                navigate(paths.todos(), true);
              }}
            >
              {t('list.clear')}
            </button>
          )}
        </div>
        {sel.length > 0 && (
          <div className="selbar selbar-todos" role="toolbar">
            <span className="selbar-n">{t('todo.selected', { n: sel.length })}</span>
            <span className="selbar-actions">
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => api.bulkUpdateTodos(sel, 'done').then(() => setSelected([]))}
                disabled={!selTodos.some((x) => x.state !== 'done')}
              >
                <Icon name="check" size={14} />
                {t('bulk.todoDone')}
              </button>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => api.bulkUpdateTodos(sel, 'dropped').then(() => setSelected([]))}
                disabled={!selTodos.some((x) => x.state !== 'dropped')}
              >
                {t('bulk.todoDrop')}
              </button>
              <button
                type="button"
                className="btn btn-sm"
                onClick={() => api.bulkUpdateTodos(sel, 'open').then(() => setSelected([]))}
                disabled={!selTodos.some((x) => x.state !== 'open')}
              >
                {t('bulk.todoReopen')}
              </button>
              <button
                type="button"
                className="btn btn-primary btn-sm"
                onClick={() => setDialog(true)}
                disabled={!selTodos.every((x) => x.state === 'open')}
                title={selTodos.every((x) => x.state === 'open') ? undefined : t('todo.onlyOpen')}
              >
                <Icon name="plus" size={14} />
                {t('todo.createTask')}
              </button>
              <button type="button" className="link-btn" onClick={() => setSelected([])}>
                {t('todo.clearSel')}
              </button>
            </span>
          </div>
        )}
        {data && data.todos.length === 0 ? (
          <Empty title={t('todo.empty')} hint={t('todo.emptyHint')} />
        ) : (
          <ul className={`rows todo-rows${res.loading ? ' stale' : ''}`}>
            {data?.todos.map((x) => (
              <TodoRow key={x.id} x={x} selected={sel.includes(x.id)} onToggle={() => setSelected((s) => (s.includes(x.id) ? s.filter((y) => y !== x.id) : [...s, x.id]))} />
            ))}
          </ul>
        )}
      </section>
      {dialog && (
        <NewTaskDialog
          todos={selTodos}
          onClose={() => {
            setDialog(false);
            setSelected([]);
          }}
        />
      )}
      {newTodo !== null && (
        <NewTodoDialog
          initialText={newTodo}
          initialPlaces={f.place && f.place !== 'none' ? [f.place] : []}
          places={places.data ?? []}
          onClose={() => setNewTodo(null)}
        />
      )}
    </div>
  );
}

/** One-line todo entry above the list: Enter adds it (with the filtered place, if any). */
function QuickAdd({ defaultPlaces, onMore }: { defaultPlaces: string[]; onMore: (text: string) => void }) {
  const { t } = useI18n();
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [added, setAdded] = useState<string | null>(null);
  return (
    <form
      className="quick-add"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!text.trim() || busy) return;
        setBusy(true);
        try {
          const x = await api.createTodo({ text: text.trim(), places: defaultPlaces });
          setAdded(x.id);
          setText('');
        } finally {
          setBusy(false);
        }
      }}
    >
      <Icon name="plus" size={15} />
      <input value={text} onChange={(e) => setText(e.target.value)} placeholder={t('todo.quickPh')} aria-label={t('todo.new')} />
      {added && !text && <span className="quick-added">{t('todo.added', { x: added })}</span>}
      <button type="button" className="link-btn" onClick={() => onMore(text)}>
        {t('todo.details')}
      </button>
    </form>
  );
}

function NewTodoDialog({
  initialText,
  initialPlaces,
  places,
  onClose,
}: {
  initialText: string;
  initialPlaces: string[];
  places: PlaceSummary[];
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [text, setText] = useState(initialText);
  const [note, setNote] = useState('');
  const [keys, setKeys] = useState<string[]>(initialPlaces);
  const [tags, setTags] = useState<string[]>([]);
  const [error, setError] = useState('');
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  return (
    <dialog ref={dialog} className="dialog" onClose={onClose} onCancel={onClose} aria-labelledby="new-todo-title">
      <form
        method="dialog"
        onSubmit={async (e) => {
          e.preventDefault();
          try {
            await api.createTodo({
              text: text.trim(),
              note: note.trim() || undefined,
              places: keys,
              tags,
            });
            onClose();
          } catch (err) {
            setError((err as Error).message);
          }
        }}
      >
        <h2 id="new-todo-title">{t('todo.new')}</h2>
        <label className="field">
          <span>{t('todo.text')}</span>
          <input value={text} onChange={(e) => setText(e.target.value)} required autoFocus />
        </label>
        <label className="field">
          <span>{t('todo.noteField')}</span>
          <textarea rows={3} value={note} onChange={(e) => setNote(e.target.value)} />
        </label>
        <div className="field">
          <span>{t('place.title')}</span>
          <PlacePicker places={places} value={keys} onChange={setKeys} multiple allowNew label={t('place.add')} icon="plus" />
        </div>
        <div className="field">
          <span>{t('list.tags')}</span>
          <TagInput value={tags} onAdd={(tag) => setTags((ts) => [...ts, tag])} onRemove={(tag) => setTags((ts) => ts.filter((x) => x !== tag))} placeholder="topic:backup, area:personal" />
        </div>
        {error && <p className="notice notice-warn">{error}</p>}
        <div className="form-actions">
          <button type="button" className="btn btn-ghost" onClick={onClose}>
            {t('task.cancel')}
          </button>
          <button type="submit" className="btn btn-primary" disabled={!text.trim()}>
            {t('todo.create')}
          </button>
        </div>
      </form>
    </dialog>
  );
}

export function TodoScreen({ id }: { id: string }) {
  const { t } = useI18n();
  const res = useQuery(`todo:${id}`, () => api.getTodo(id));
  const places = useQuery('places', () => api.listPlaces());
  const [dialog, setDialog] = useState(false);
  if (res.error) return <ErrorView error={res.error} />;
  if (!res.data) return <Loading />;
  const x = res.data;
  return (
    <div className="page page-narrow todo-page">
      <nav className="crumbs">
        <a href={href(paths.todos())}>{t('todo.back')}</a>
        <Icon name="chevronRight" size={12} />
        <span className="mono">{x.id}</span>
      </nav>
      <h1 className="todo-title">
        <InlineMarkdown text={x.text} />
      </h1>
      <div className="task-status-line">
        <TodoStateBadge state={x.state} />
        {x.task && (
          <span className="todo-link-task">
            {t('todo.inTask')} <TaskRef id={x.task} />
          </span>
        )}
        <span className="todo-actions">
          {x.state === 'open' ? (
            <>
              <button type="button" className="btn btn-sm btn-primary" onClick={() => setDialog(true)}>
                <Icon name="plus" size={14} />
                {t('new.title')}
              </button>
              <button type="button" className="btn btn-sm" onClick={() => api.updateTodo(x.id, { state: 'done' })}>
                <Icon name="check" size={14} />
                {t('todo.done')}
              </button>
              <button type="button" className="btn btn-sm btn-ghost" onClick={() => api.updateTodo(x.id, { state: 'dropped' })}>
                {t('todo.drop')}
              </button>
            </>
          ) : (
            <button type="button" className="btn btn-sm" onClick={() => api.updateTodo(x.id, { state: 'open' })}>
              {t('todo.reopen')}
            </button>
          )}
        </span>
      </div>
      {x.note && (
        <section className="card todo-note">
          <SmallMarkdown text={x.note} />
        </section>
      )}
      <dl className="dates todo-facts">
        <dt>{t('place.title')}</dt>
        <dd>
          <PlacePicker
            places={[...(places.data ?? []), ...x.places.filter((p) => !(places.data ?? []).some((q) => q.key === p.key))]}
            value={x.places.map((p) => p.key)}
            onChange={(keys) => {
              const was = x.places.map((p) => p.key);
              api.updateTodo(x.id, { places_add: keys.filter((k) => !was.includes(k)), places_remove: was.filter((k) => !keys.includes(k)) });
            }}
            multiple
            allowNew
            label={t('place.add')}
            icon="plus"
            className="pp-link"
          />
        </dd>
        <dt>{t('list.tags')}</dt>
        <dd>
          <TagInput
            value={x.tags}
            onAdd={(tag) => api.updateTodo(x.id, { tags_add: [tag] })}
            onRemove={(tag) => api.updateTodo(x.id, { tags_remove: [tag] })}
          />
        </dd>
        {x.origin_task && (
          <>
            <dt>{t('todo.from')}</dt>
            <dd>
              <TaskRef id={x.origin_task} />
            </dd>
          </>
        )}
        <dt>{t('todo.created')}</dt>
        <dd>
          <Time iso={x.created_at} abs />
          {x.source && <span className="jsource"> · {x.source}</span>}
        </dd>
        {x.closed_at && (
          <>
            <dt>{t('todo.closed')}</dt>
            <dd>
              <Time iso={x.closed_at} abs />
            </dd>
          </>
        )}
      </dl>
      {dialog && <NewTaskDialog todos={[x]} onClose={() => setDialog(false)} />}
    </div>
  );
}
