import { useState } from 'react';
import type { By, PlanItem } from '../api/types';
import { useI18n } from '../i18n';
import { Icon } from './Icon';
import { InlineMarkdown } from './Markdown';

export type ItemChange = (item: PlanItem, change: { done?: boolean; note?: string }) => Promise<unknown> | void;

function ByLabel({ by }: { by: By }) {
  const { t } = useI18n();
  return <span className={`by by-${by}`}>{t(`by.${by}`)}</span>;
}

/** The item's box: a toggle when `onChange` is given (latest revision), else read-only. */
export function ItemBox({ item, done, onChange }: { item?: PlanItem; done: boolean; onChange?: ItemChange }) {
  const { t, dateTime } = useI18n();
  const [busy, setBusy] = useState(false);
  const who = item?.done && item.done_by ? t('item.tickedBy', { x: t(`by.${item.done_by}`) }) + (item.done_at ? ` · ${dateTime(item.done_at)}` : '') : '';
  const check = (
    <svg viewBox="0 0 16 16" aria-hidden="true">
      <path d="M3.5 8.5l3 3 6-7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
  if (!item || !onChange)
    return (
      <span className={`box${done ? ' done' : ''}`} role="img" aria-label={done ? '☑' : '☐'} title={who || t('doc.itemReadonly')} data-noanchor>
        {done && check}
      </span>
    );
  return (
    <button
      type="button"
      className={`box box-btn${done ? ' done' : ''}${busy ? ' busy' : ''}`}
      role="checkbox"
      aria-checked={done}
      aria-label={item.text}
      title={(who ? who + '\n' : '') + (done ? t('item.untick') : t('item.tick'))}
      data-noanchor
      onClick={async (e) => {
        e.stopPropagation();
        setBusy(true);
        try {
          await onChange(item, { done: !done });
        } finally {
          setBusy(false);
        }
      }}
    >
      {done && check}
    </button>
  );
}

/** Notes under an item (with who wrote them), who ticked it, and an "add note" field. */
export function ItemMeta({ item, onChange, compact }: { item: PlanItem; onChange?: ItemChange; compact?: boolean }) {
  const { t, dateTime } = useI18n();
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState('');
  const notes = compact ? item.notes.slice(-1) : item.notes;
  const showBy = item.done && item.done_by;
  if (!notes.length && !showBy && !onChange) return null;
  return (
    <div className="item-meta" data-noanchor>
      {notes.length > 0 && (
        <ul className="item-notes">
          {notes.map((n, i) => (
            <li key={i}>
              <span className="note-when">
                <time dateTime={n.created_at}>{dateTime(n.created_at)}</time>
                <ByLabel by={n.author} />
              </span>
              <span className="note-body">
                <InlineMarkdown text={n.body} />
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="item-foot">
        {showBy && (
          <span className="ticked-by" title={item.done_at ? dateTime(item.done_at) : undefined}>
            <Icon name="check" size={11} />
            {t('item.tickedBy', { x: t(`by.${item.done_by!}`) })}
          </span>
        )}
        {onChange && !adding && (
          <button type="button" className="link-btn add-note" onClick={() => setAdding(true)}>
            <Icon name="plus" size={12} />
            {t('item.addNote')}
          </button>
        )}
      </div>
      {onChange && adding && (
        <form
          className="note-form"
          onSubmit={async (e) => {
            e.preventDefault();
            if (!text.trim()) return;
            await onChange(item, { note: text.trim() });
            setText('');
            setAdding(false);
          }}
        >
          <input
            autoFocus
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => e.key === 'Escape' && setAdding(false)}
            placeholder={t('item.notePh')}
            aria-label={t('item.addNote')}
          />
          <button type="submit" className="btn btn-sm btn-primary" disabled={!text.trim()}>
            {t('rv.add')}
          </button>
          <button type="button" className="btn btn-sm btn-ghost" onClick={() => setAdding(false)}>
            {t('sel.cancel')}
          </button>
        </form>
      )}
    </div>
  );
}
