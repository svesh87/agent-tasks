import { useState } from 'react';
import type { ReviewRound, RevisionMeta } from '../api';
import { useI18n } from '../i18n';
import { href, navigate, paths } from '../router';
import { Icon } from './Icon';
import { AuthorBadge, Time } from './ui';

export function HistoryPanel({
  taskId,
  slug,
  revisions,
  rounds,
  current,
  onRevert,
  approved,
}: {
  onRevert?: (n: number) => void;
  /** Revisions the owner approved. */
  approved?: number[];
  taskId: string;
  slug: string;
  /** Newest first. */
  revisions: RevisionMeta[];
  rounds: ReviewRound[];
  current?: number;
}) {
  const { t, count } = useI18n();
  const latest = revisions[0]?.n ?? 1;
  const [from, setFrom] = useState(Math.max(1, latest - 1));
  const [to, setTo] = useState(latest);
  return (
    <div className="history">
      {latest > 1 && (
        <form
          className="compare"
          onSubmit={(e) => {
            e.preventDefault();
            if (from < to) navigate(paths.diff(taskId, slug, from, to));
          }}
        >
          <label>
            <span>{t('hist.from')}</span>
            <select value={from} onChange={(e) => setFrom(Number(e.target.value))}>
              {revisions.map((r) => (
                <option key={r.n} value={r.n} disabled={r.n >= to}>
                  r{r.n}
                </option>
              ))}
            </select>
          </label>
          <Icon name="arrowRight" size={14} />
          <label>
            <span>{t('hist.to')}</span>
            <select value={to} onChange={(e) => setTo(Number(e.target.value))}>
              {revisions.map((r) => (
                <option key={r.n} value={r.n} disabled={r.n <= from}>
                  r{r.n}
                </option>
              ))}
            </select>
          </label>
          <button type="submit" className="btn btn-sm" disabled={from >= to}>
            <Icon name="diff" size={14} />
            {t('hist.compare')}
          </button>
        </form>
      )}
      <ol className="revs">
        {revisions.map((r) => {
          const rs = rounds.filter((x) => x.revision === r.n && x.state !== 'draft');
          return (
            <li key={r.n} className={`rev${r.n === current ? ' current' : ''}`}>
              <div className="rev-line">
                <a className="rev-n mono" href={href(paths.doc(taskId, slug, r.n === latest ? undefined : r.n))}>
                  r{r.n}
                </a>
                <AuthorBadge author={r.author} />
                <Time iso={r.created_at} />
                {r.n === latest && <span className="chip">{t('doc.latest')}</span>}
                {r.n === current && <span className="chip chip-accent">{t('hist.current')}</span>}
                {approved?.includes(r.n) && (
                  <span className="approved-badge">
                    <Icon name="check" size={12} />
                    {t('ap.badge')}
                  </span>
                )}
              </div>
              {r.message && <p className="rev-msg">{r.message}</p>}
              <div className="rev-actions">
                {r.n !== current && (
                  <a className="link-btn" href={href(paths.doc(taskId, slug, r.n === latest ? undefined : r.n))}>
                    {t('hist.view')}
                  </a>
                )}
                {r.n > 1 && (
                  <a className="link-btn" href={href(paths.diff(taskId, slug, r.n - 1, r.n))}>
                    <Icon name="diff" size={13} />
                    {t('hist.diffPrev', { n: r.n - 1 })}
                  </a>
                )}
                {onRevert && r.n !== latest && (
                  <button type="button" className="link-btn" onClick={() => onRevert(r.n)} title={t('rev.button', { n: r.n })}>
                    <Icon name="history" size={13} />
                    {t('rev.short')}
                  </button>
                )}
              </div>
              {rs.map((x) => (
                <div key={x.id} className="rev-round">
                  <Icon name="comment" size={13} />
                  {t('rv.round', { n: x.n })} · {count(x.comments.length, 'comment')} ·{' '}
                  <span className={`round-state round-${x.state}`}>{t(`rv.state.${x.state}`)}</span>
                </div>
              ))}
            </li>
          );
        })}
      </ol>
    </div>
  );
}
