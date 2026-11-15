import { useEffect, useState } from 'react';
import { api, useQuery, type ClosedReason, type JournalKind, type Task, type TaskDetail, type TaskStatus } from '../api';
import { Icon } from '../components/Icon';
import { InlineMarkdown, SmallMarkdown } from '../components/Markdown';
import { PlacesCard } from '../components/PlacesCard';
import { ItemBox, ItemMeta, type ItemChange } from '../components/PlanItem';
import { TagInput } from '../components/TagInput';
import { AuthorBadge, CopyButton, ErrorView, Loading, Popover, Progress, StatusBadge, Time } from '../components/ui';
import { useI18n } from '../i18n';
import { href, paths } from '../router';

function TaskTodos({ d }: { d: TaskDetail }) {
  const { t } = useI18n();
  if (!d.todos.length) return null;
  return (
    <section className="card">
      <h2 className="card-title">{t('task.todos')}</h2>
      <ul className="mini-todos">
        {d.todos.map((x) => (
          <li key={x.id}>
            <a className="mono todo-id" href={href(paths.todo(x.id))}>
              {x.id}
            </a>
            <span>
              <InlineMarkdown text={x.text} />
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

const STATUSES: TaskStatus[] = ['active', 'waiting', 'later', 'archived'];

function StatusChanger({ task }: { task: Task }) {
  const { t } = useI18n();
  const [status, setStatus] = useState<TaskStatus>(task.status);
  const [waitingOn, setWaitingOn] = useState(task.waiting_on ?? '');
  const [reason, setReason] = useState<ClosedReason>(task.closed_reason ?? 'done');
  return (
    <Popover
      trigger={({ toggle, open }) => (
        <button type="button" className="btn btn-sm" onClick={toggle} aria-expanded={open}>
          {t('task.changeStatus')}
          <Icon name="chevronDown" size={14} />
        </button>
      )}
    >
      {(close) => (
        <form
          className="status-form"
          onSubmit={(e) => {
            e.preventDefault();
            api
              .updateTask(task.id, {
                status,
                waiting_on: status === 'waiting' ? waitingOn : undefined,
                closed_reason: status === 'archived' ? reason : undefined,
              })
              .then(close);
          }}
        >
          <fieldset>
            {STATUSES.map((s) => (
              <label key={s} className="radio">
                <input type="radio" name="status" checked={status === s} onChange={() => setStatus(s)} />
                <StatusBadge status={s} />
              </label>
            ))}
          </fieldset>
          {status === 'waiting' && (
            <label className="field">
              <span>{t('task.waitingOn')}</span>
              <input value={waitingOn} onChange={(e) => setWaitingOn(e.target.value)} placeholder={t('task.waitingOnPh')} autoFocus />
            </label>
          )}
          {status === 'archived' && (
            <fieldset className="field">
              <span>{t('task.closedReason')}</span>
              <div className="seg">
                {(['done', 'cancelled'] as const).map((r) => (
                  <label key={r} className={`seg-item${reason === r ? ' on' : ''}`}>
                    <input type="radio" name="reason" checked={reason === r} onChange={() => setReason(r)} />
                    {t(`closed.${r}`)}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
          <div className="form-actions">
            <button type="button" className="btn btn-ghost btn-sm" onClick={close}>
              {t('task.cancel')}
            </button>
            <button type="submit" className="btn btn-primary btn-sm">
              {t('task.apply')}
            </button>
          </div>
        </form>
      )}
    </Popover>
  );
}

function TagEditor({ task }: { task: Task }) {
  return (
    <TagInput
      value={task.tags}
      onAdd={(tag) => api.updateTask(task.id, { tags_add: [tag] })}
      onRemove={(tag) => api.updateTask(task.id, { tags_remove: [tag] })}
    />
  );
}

function Reviews({ d }: { d: TaskDetail }) {
  const { t, count } = useI18n();
  if (!d.rounds.length) return null;
  return (
    <section className="card">
      <h2 className="card-title">{t('task.reviews')}</h2>
      <ul className="review-list">
        {d.rounds.map(({ slug, doc_title, ...round }) => (
          <li key={round.id}>
            <a className={`review-link review-${round.state}`} href={href(paths.doc(d.task.id, slug))}>
              <Icon name={round.state === 'draft' ? 'edit' : 'comment'} size={15} />
              <span className="review-what">
                <strong>{doc_title}</strong>
                <span className="muted">
                  {' · '}
                  {t('rv.round', { n: round.n })} {t('rv.onRev', { n: round.revision })} · {count(round.comments, 'comment')}
                </span>
              </span>
              <span className={`round-state round-${round.state}`}>{t(`rv.state.${round.state}`)}</span>
            </a>
          </li>
        ))}
      </ul>
    </section>
  );
}

function PlanCard({ d }: { d: TaskDetail }) {
  const { t } = useI18n();
  const p = d.plan;
  if (!p) return null;
  const onItem: ItemChange = (it, change) => api.setItem(d.task.id, p.slug, it.id, change);
  return (
    <section className="card">
      <div className="card-head">
        <h2 className="card-title">{t('task.plan')}</h2>
        <Progress done={p.done} total={p.total} />
        <a className="card-link" href={href(paths.doc(d.task.id, p.slug))}>
          {t('task.openPlan')}
          <Icon name="arrowRight" size={14} />
        </a>
      </div>
      <ul className="plan-items">
        {p.items.map((it) => (
          <li key={it.id} className={it.done ? 'done' : ''}>
            <ItemBox item={it} done={it.done} onChange={onItem} />
            <div className="plan-main">
              <span className="plan-text">
                <InlineMarkdown text={it.text} />
              </span>
              <ItemMeta item={it} onChange={onItem} compact />
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

function Documents({ d }: { d: TaskDetail }) {
  const { t } = useI18n();
  return (
    <section className="card">
      <h2 className="card-title">{t('task.documents')}</h2>
      {d.documents.length === 0 ? (
        <p className="muted small">{t('task.noDocuments')}</p>
      ) : (
        <ul className="doc-list">
          {d.documents.map((doc) => (
            <li key={doc.slug}>
              <a className="doc-row" href={href(paths.doc(d.task.id, doc.slug))}>
                <Icon name="doc" size={16} className="doc-icon" />
                <span className="doc-main">
                  <span className="doc-title">{doc.title}</span>
                  <span className="doc-sub">
                    <span className={`kind kind-${doc.kind}`}>{t(`kind.${doc.kind}`)}</span>
                    <span className="mono">{doc.slug}</span>
                    <span>
                      {t('doc.rev', { n: doc.latest })} · {doc.latest_author && (
                        <>
                          <AuthorBadge author={doc.latest_author} /> ·{' '}
                        </>
                      )}
                      <Time iso={doc.updated_at} />
                    </span>
                  </span>
                </span>
                <span className="doc-facts">
                  {doc.plan && <Progress done={doc.plan.done} total={doc.plan.total} />}
                  {doc.open_comments > 0 && (
                    <span className="fact">
                      <Icon name="comment" size={13} />
                      {t('doc.openComments', { n: doc.open_comments })}
                    </span>
                  )}
                  {doc.approved && (
                    <span className="approved-badge" title={t('ap.approved', { n: doc.approved.revision })}>
                      <Icon name="check" size={12} />
                      {t('ap.badge')} r{doc.approved.revision}
                    </span>
                  )}
                  {doc.submitted_round && <span className="round-state round-submitted">{t('rv.state.submitted')}</span>}
                  {doc.draft_round && <span className="round-state round-draft">{t('rv.state.draft')}</span>}
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Journal({ d, focus }: { d: TaskDetail; focus?: number }) {
  const { t } = useI18n();
  const [kind, setKind] = useState<JournalKind | 'all'>('all');
  // A search hit in the journal opens the task with ?j=<entry>: show enough entries to include it.
  const [limit, setLimit] = useState(() => Math.max(5, focus !== undefined ? d.journal.findIndex((j) => j.id === focus) + 1 : 0));
  const entries = d.journal.filter((j) => kind === 'all' || j.kind === kind);
  const shown = entries.slice(0, limit);
  return (
    <section className="card">
      <div className="card-head">
        <h2 className="card-title">{t('task.journal')}</h2>
        <div className="seg seg-sm" role="group">
          {(['all', 'context', 'decision', 'log'] as const).map((k) => (
            <button key={k} type="button" className={`seg-item${kind === k ? ' on' : ''}`} aria-pressed={kind === k} onClick={() => setKind(k)}>
              {t(`journal.${k}`)}
            </button>
          ))}
        </div>
      </div>
      {entries.length === 0 ? (
        <p className="muted small">{t('task.noJournal')}</p>
      ) : (
        <ol className="journal">
          {shown.map((j) => (
            <li key={j.id} id={`j-${j.id}`} className={`jentry jentry-${j.kind}${j.id === focus ? ' focus' : ''}`}>
              <div className="jentry-head">
                <span className={`jkind jkind-${j.kind}`}>{t(`journal.${j.kind}`)}</span>
                <Time iso={j.created_at} />
                {j.source && <span className="jsource">{j.source}</span>}
              </div>
              <SmallMarkdown text={j.body} />
            </li>
          ))}
        </ol>
      )}
      {entries.length > limit && (
        <button type="button" className="link-btn" onClick={() => setLimit((l) => l + 10)}>
          {t('journal.more', { n: entries.length - limit })}
        </button>
      )}
    </section>
  );
}

/** The task title with an inline rename. */
function TitleEditor({ task }: { task: Task }) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [val, setVal] = useState(task.title);
  if (!editing)
    return (
      <h1 className="task-title">
        <span className="task-id mono">{task.id}</span> {task.title}
        <button
          type="button"
          className="btn btn-ghost btn-icon btn-sm inline-edit"
          onClick={() => {
            setVal(task.title);
            setEditing(true);
          }}
          title={t('task.rename')}
          aria-label={t('task.rename')}
        >
          <Icon name="edit" size={14} />
        </button>
      </h1>
    );
  return (
    <form
      className="title-form"
      onSubmit={async (e) => {
        e.preventDefault();
        if (!val.trim()) return;
        if (val.trim() !== task.title) await api.updateTask(task.id, { title: val.trim() });
        setEditing(false);
      }}
    >
      <span className="task-id mono">{task.id}</span>
      <input autoFocus value={val} onChange={(e) => setVal(e.target.value)} onKeyDown={(e) => e.key === 'Escape' && setEditing(false)} aria-label={t('task.rename')} />
      <button type="submit" className="btn btn-sm btn-primary" disabled={!val.trim()}>
        {t('task.save')}
      </button>
      <button type="button" className="btn btn-sm btn-ghost" onClick={() => setEditing(false)}>
        {t('task.cancel')}
      </button>
    </form>
  );
}

/** The summary (markdown) with an inline editor. */
function SummaryEditor({ task }: { task: Task }) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [val, setVal] = useState(task.summary ?? '');
  if (!editing)
    return (
      <section className="task-summary">
        {task.summary && <SmallMarkdown text={task.summary} />}
        <button
          type="button"
          className="link-btn"
          onClick={() => {
            setVal(task.summary ?? '');
            setEditing(true);
          }}
        >
          <Icon name={task.summary ? 'edit' : 'plus'} size={13} />
          {task.summary ? t('task.editSummary') : t('task.addSummary')}
        </button>
      </section>
    );
  return (
    <form
      className="summary-form"
      onSubmit={async (e) => {
        e.preventDefault();
        await api.updateTask(task.id, { summary: val });
        setEditing(false);
      }}
    >
      <textarea autoFocus rows={4} value={val} onChange={(e) => setVal(e.target.value)} placeholder={t('task.summaryPh')} aria-label={t('task.editSummary')} />
      <div className="form-actions">
        <button type="button" className="btn btn-sm btn-ghost" onClick={() => setEditing(false)}>
          {t('task.cancel')}
        </button>
        <button type="submit" className="btn btn-sm btn-primary">
          {t('task.save')}
        </button>
      </div>
    </form>
  );
}

export function TaskScreen({ id, query }: { id: string; query?: URLSearchParams }) {
  const { t } = useI18n();
  const res = useQuery(`task:${id}`, () => api.getTask(id));
  const jump = query?.get('j') ?? undefined;
  useEffect(() => {
    if (!jump || !res.data) return;
    const el = document.getElementById(`j-${jump}`);
    el?.scrollIntoView({ block: 'center' });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jump, !!res.data]);
  if (res.error) return <ErrorView error={res.error} />;
  if (!res.data) return <Loading />;
  const d = res.data;
  const task = d.task;
  return (
    <div className="page task-page">
      <nav className="crumbs" aria-label="breadcrumbs">
        <a href={href('/')}>{t('nav.tasks')}</a>
        <Icon name="chevronRight" size={12} />
        <span className="mono">{task.id}</span>
      </nav>
      <header className="task-head">
        <TitleEditor key={task.title} task={task} />
        <div className="task-status-line">
          <StatusBadge task={task} />
          {task.status === 'waiting' && task.waiting_on && <span className="waiting-on">{t('waiting.on', { x: task.waiting_on })}</span>}
          <StatusChanger key={`${task.status}${task.updated_at}`} task={task} />
        </div>
      </header>
      <div className="task-grid">
        <div className="task-main">
          <SummaryEditor key={task.summary ?? ''} task={task} />
          <Reviews d={d} />
          <PlacesCard task={task} />
          <PlanCard d={d} />
          <Documents d={d} />
          <TaskTodos d={d} />
          <Journal d={d} focus={jump ? Number(jump) : undefined} />
        </div>
        <aside className="task-side">
          <section className="side-block">
            <h2 className="side-title">{t('list.tags')}</h2>
            <TagEditor task={task} />
          </section>
          <section className="side-block">
            <h2 className="side-title">{t('task.workspace')}</h2>
            <div className="copy-line">
              <Icon name="folder" size={14} />
              <code>{d.workspace}</code>
              <CopyButton text={d.workspace} />
            </div>
          </section>
          <section className="side-block">
            <h2 className="side-title">{t('task.link')}</h2>
            <div className="copy-line">
              <Icon name="link" size={14} />
              <code className="truncate">{d.url.replace(/^https?:\/\//, '')}</code>
              <CopyButton text={d.url} />
            </div>
          </section>
          <section className="side-block">
            <dl className="dates">
              <dt>{t('task.created')}</dt>
              <dd>
                <Time iso={task.created_at} abs />
              </dd>
              <dt>{t('task.updated')}</dt>
              <dd>
                <Time iso={task.updated_at} />
              </dd>
              {task.archived_at && (
                <>
                  <dt>{t('task.archived')}</dt>
                  <dd>
                    <Time iso={task.archived_at} abs />
                  </dd>
                </>
              )}
            </dl>
          </section>
        </aside>
      </div>
    </div>
  );
}
