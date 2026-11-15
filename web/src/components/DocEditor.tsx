import { useEffect, useRef, useState } from 'react';
import { api, ConflictError, type DocumentView } from '../api';
import { useI18n } from '../i18n';
import { absolute, paths } from '../router';
import { Icon } from './Icon';
import { Markdown } from './Markdown';

/**
 * The owner's editor for the latest revision: plain markdown with a preview. Saving writes a
 * new revision; if another revision appeared meanwhile, nothing is overwritten — the owner
 * sees it, the text stays, and they can save on top of the newer revision.
 */
export function DocEditor({ view, onClose }: { view: DocumentView; onClose: (saved?: { n: number; changed: boolean }) => void }) {
  const { t } = useI18n();
  const [body, setBody] = useState(view.revision.body);
  const [message, setMessage] = useState('');
  const [tab, setTab] = useState<'write' | 'preview'>('write');
  const [base, setBase] = useState(view.revision.n);
  const [conflict, setConflict] = useState<number | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const area = useRef<HTMLTextAreaElement>(null);
  const dirty = body !== view.revision.body;

  // Grow the textarea with its content, so the page scrolls, not a small box.
  useEffect(() => {
    const el = area.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.max(320, el.scrollHeight + 4)}px`;
  }, [body, tab]);

  // Leaving the page with unsaved text asks first.
  useEffect(() => {
    if (!dirty) return;
    const onLeave = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener('beforeunload', onLeave);
    return () => window.removeEventListener('beforeunload', onLeave);
  }, [dirty]);

  const save = async () => {
    setBusy(true);
    setError('');
    try {
      const r = await api.writeDocument(view.task.id, view.slug, body, base, message);
      onClose({ n: r.n, changed: r.n !== base });
    } catch (e) {
      if (e instanceof ConflictError) setConflict(e.latest);
      else setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="doc-editor" aria-label={t('edit.title', { n: base })}>
      <div className="editor-bar">
        <span className="editor-title">{t('edit.title', { n: base })}</span>
        <div className="seg seg-sm" role="tablist">
          <button type="button" role="tab" aria-selected={tab === 'write'} className={`seg-item${tab === 'write' ? ' on' : ''}`} onClick={() => setTab('write')}>
            {t('edit.write')}
          </button>
          <button type="button" role="tab" aria-selected={tab === 'preview'} className={`seg-item${tab === 'preview' ? ' on' : ''}`} onClick={() => setTab('preview')}>
            {t('edit.preview')}
          </button>
        </div>
      </div>
      {tab === 'write' ? (
        <textarea
          ref={area}
          className="editor-area"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          spellCheck
          aria-label={t('edit.write')}
          onKeyDown={(e) => {
            if (e.key === 's' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              if (dirty && !busy) save();
            }
          }}
        />
      ) : (
        <div className="editor-preview">
          <Markdown body={body} />
        </div>
      )}
      {conflict !== null && (
        <div className="notice notice-warn editor-conflict">
          <p>{t('edit.conflict', { n: conflict })}</p>
          <div className="form-actions">
            <a className="btn btn-sm" href={absolute(paths.diff(view.task.id, view.slug, base, conflict))} target="_blank" rel="noreferrer">
              <Icon name="diff" size={14} />
              {t('edit.conflictOpen', { n: conflict })}
            </a>
            <button
              type="button"
              className="btn btn-sm btn-primary"
              onClick={() => {
                setBase(conflict);
                setConflict(null);
              }}
            >
              {t('edit.rebase', { n: conflict })}
            </button>
          </div>
        </div>
      )}
      {error && <p className="notice notice-warn">{error}</p>}
      <div className="editor-foot">
        <input value={message} onChange={(e) => setMessage(e.target.value)} placeholder={t('edit.message')} aria-label={t('edit.message')} />
        <button
          type="button"
          className="btn btn-ghost"
          onClick={() => {
            if (!dirty || confirm(t('edit.discard'))) onClose();
          }}
        >
          {t('edit.cancel')}
        </button>
        <button type="button" className="btn btn-primary" disabled={!dirty || busy || conflict !== null} onClick={save}>
          {t('edit.save', { n: base + 1 })}
        </button>
      </div>
    </section>
  );
}
