import { useEffect, useRef, useState, type ReactNode } from 'react';
import type { Author, Place, Task, TaskStatus } from '../api/types';
import { useI18n } from '../i18n';
import { href, paths } from '../router';
import { Icon } from './Icon';

export function StatusBadge({ task, status }: { task?: Task; status?: TaskStatus }) {
  const { t } = useI18n();
  const s = task?.status ?? status!;
  let extra = '';
  if (task?.status === 'archived' && task.closed_reason) extra = ` · ${t(`closed.${task.closed_reason}`)}`;
  return (
    <span className={`status status-${s}${task?.closed_reason && task.closed_reason !== 'done' ? ' cancelled' : ''}`}>
      <span className="status-dot" aria-hidden="true" />
      {t(`status.${s}`)}
      {extra}
    </span>
  );
}

export function PlaceChip({ place, link, active }: { place: Place; link?: 'tasks' | 'todos'; active?: boolean }) {
  const content = (
    <>
      <Icon name={place.kind === 'git' ? 'repo' : 'folder'} size={12} />
      <span className="tag-name">{place.label}</span>
    </>
  );
  const cls = `tag place-chip place-${place.kind}${active ? ' active' : ''}`;
  if (!link)
    return (
      <span className={cls} title={place.key}>
        {content}
      </span>
    );
  const q = new URLSearchParams({ place: place.key });
  return (
    <a className={cls} title={place.key} href={href(link === 'todos' ? paths.todos(q) : paths.list(q))}>
      {content}
    </a>
  );
}

export function TaskRef({ id }: { id: string }) {
  return (
    <a className="task-ref mono" href={href(paths.task(id))}>
      {id}
    </a>
  );
}

/** Splits `repo:github.com/a/b` into prefix and name; repo names are shortened for display. */
export function tagParts(tag: string): { ns: string; name: string; short: string } {
  const i = tag.indexOf(':');
  if (i <= 0) return { ns: '', name: tag, short: tag };
  const ns = tag.slice(0, i);
  const name = tag.slice(i + 1);
  let short = name;
  if (ns === 'repo') {
    const segs = name.split('/');
    short = segs.length > 2 ? segs.slice(-2).join('/') : name;
  }
  return { ns, name, short };
}

export function Tag({
  tag,
  active,
  onClick,
  onRemove,
  link = true,
}: {
  tag: string;
  active?: boolean;
  onClick?: () => void;
  onRemove?: () => void;
  link?: boolean;
}) {
  const { t } = useI18n();
  const { ns, short } = tagParts(tag);
  const content = (
    <>
      {ns === 'repo' ? <Icon name="repo" size={12} /> : ns ? <span className="tag-ns">{ns}:</span> : null}
      <span className="tag-name">{short}</span>
    </>
  );
  const cls = `tag tag-${ns || 'plain'}${active ? ' active' : ''}`;
  if (onRemove)
    return (
      <span className={cls} title={tag}>
        {content}
        <button type="button" className="tag-x" onClick={onRemove} aria-label={t('task.removeTag', { x: tag })}>
          <Icon name="x" size={12} />
        </button>
      </span>
    );
  if (onClick)
    return (
      <button type="button" className={cls} title={tag} onClick={onClick} aria-pressed={active}>
        {content}
      </button>
    );
  if (link)
    return (
      <a className={cls} title={tag} href={href(paths.list(new URLSearchParams({ tag })))}>
        {content}
      </a>
    );
  return (
    <span className={cls} title={tag}>
      {content}
    </span>
  );
}

export function Progress({ done, total, label = true }: { done: number; total: number; label?: boolean }) {
  const pct = total ? Math.round((done / total) * 100) : 0;
  return (
    <span className="progress" title={`${done}/${total}`}>
      <span className="progress-bar" aria-hidden="true">
        <span style={{ width: `${pct}%` }} />
      </span>
      {label && (
        <span className="progress-label">
          {done}/{total}
        </span>
      )}
    </span>
  );
}

export function AuthorBadge({ author }: { author: Author }) {
  const { t } = useI18n();
  return <span className={`author author-${author}`}>{t(`author.${author}`)}</span>;
}

export function Time({ iso, abs }: { iso: string; abs?: boolean }) {
  const { ago, dateTime } = useI18n();
  return (
    <time dateTime={iso} title={dateTime(iso)}>
      {abs ? dateTime(iso) : ago(iso)}
    </time>
  );
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const { t } = useI18n();
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      className="btn btn-ghost btn-sm"
      onClick={() => {
        navigator.clipboard?.writeText(text).catch(() => undefined);
        setDone(true);
        setTimeout(() => setDone(false), 1500);
      }}
      title={label ?? t('task.copy')}
    >
      <Icon name={done ? 'check' : 'copy'} size={14} />
      <span className="sr-only-sm">{done ? t('task.copied') : t('task.copy')}</span>
    </button>
  );
}

/** A disclosure popover anchored to its trigger; closes on outside click and Escape. */
export function Popover({
  trigger,
  children,
  align = 'start',
  className,
}: {
  trigger: (p: { open: boolean; toggle: () => void }) => ReactNode;
  children: (close: () => void) => ReactNode;
  align?: 'start' | 'end';
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false);
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);
  return (
    <div className={`popover-host ${className ?? ''}`} ref={ref}>
      {trigger({ open, toggle: () => setOpen((o) => !o) })}
      {open && <div className={`popover popover-${align}`}>{children(() => setOpen(false))}</div>}
    </div>
  );
}

export function Empty({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="empty">
      <p className="empty-title">{title}</p>
      {hint && <p className="empty-hint">{hint}</p>}
    </div>
  );
}

export function ErrorView({ error }: { error: Error }) {
  const { t } = useI18n();
  return (
    <div className="page page-narrow">
      <div className="empty">
        <p className="empty-title">{t('err.title')}</p>
        <p className="empty-hint mono">{error.message}</p>
        <a className="btn" href={href('/')}>
          {t('err.home')}
        </a>
      </div>
    </div>
  );
}

export function Loading() {
  const { t } = useI18n();
  return <div className="loading" aria-busy="true">{t('loading')}</div>;
}
