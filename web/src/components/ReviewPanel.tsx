import { useEffect, useRef, useState } from 'react';
import { anchorOn, api, type Comment, type DocumentView, type ReviewRound } from '../api';
import { useI18n } from '../i18n';
import { href, paths } from '../router';
import { Icon } from './Icon';
import { SmallMarkdown } from './Markdown';
import { Time } from './ui';

function Quote({ c, onShow, viewRev, latest, taskId, slug }: {
  c: Comment;
  onShow: (id: number) => void;
  viewRev: number;
  latest: number;
  taskId: string;
  slug: string;
}) {
  const { t } = useI18n();
  if (!c.anchor) return <span className="c-general">{t('rv.generalLabel')}</span>;
  const quote = c.anchor.quote.length > 160 ? c.anchor.quote.slice(0, 157) + '…' : c.anchor.quote;
  const missing = !anchorOn(c, viewRev);
  const gone = c.outdated_in !== undefined && c.outdated_in <= viewRev;
  // Where the comment can be seen: its newest position, or the reviewed revision when gone.
  const showRev = gone ? c.revision : (c.current?.revision ?? c.revision);
  return (
    <div className={`c-quote-wrap${missing ? ' missing' : ''}${gone ? ' gone' : ''}`}>
      {missing ? (
        <blockquote className="c-quote">{quote}</blockquote>
      ) : (
        <button type="button" className="c-quote" onClick={() => onShow(c.id)}>
          {quote}
        </button>
      )}
      {missing && (
        <div className="c-missing">
          {gone ? t('rv.goneSince', { n: c.outdated_in! }) : t('rv.notHere')}{' '}
          <a href={href(`${paths.doc(taskId, slug, showRev === latest ? undefined : showRev)}?c=${c.id}`)}>{t('rv.showOn', { n: showRev })}</a>
        </div>
      )}
    </div>
  );
}

function Composer({
  placeholder,
  submitLabel,
  initial = '',
  onSubmit,
  onCancel,
  autoFocus,
  rows = 3,
}: {
  placeholder: string;
  submitLabel: string;
  initial?: string;
  onSubmit: (text: string) => Promise<unknown> | void;
  onCancel?: () => void;
  autoFocus?: boolean;
  rows?: number;
}) {
  const { t } = useI18n();
  const [text, setText] = useState(initial);
  const [busy, setBusy] = useState(false);
  const submit = async () => {
    if (!text.trim() || busy) return;
    setBusy(true);
    try {
      await onSubmit(text.trim());
      setText('');
    } finally {
      setBusy(false);
    }
  };
  return (
    <form
      className="composer"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <textarea
        value={text}
        rows={rows}
        placeholder={placeholder}
        aria-label={placeholder}
        autoFocus={autoFocus}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            submit();
          }
          if (e.key === 'Escape' && onCancel) onCancel();
        }}
      />
      <div className="form-actions">
        <span className="hint-kbd">Ctrl+Enter</span>
        {onCancel && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={onCancel}>
            {t('sel.cancel')}
          </button>
        )}
        <button type="submit" className="btn btn-primary btn-sm" disabled={!text.trim() || busy}>
          {submitLabel}
        </button>
      </div>
    </form>
  );
}

function CommentCard({
  c,
  round,
  view,
  active,
  onActivate,
}: {
  c: Comment;
  round: ReviewRound;
  view: DocumentView;
  active: boolean;
  onActivate: (id: number) => void;
}) {
  const { t } = useI18n();
  const [replying, setReplying] = useState(false);
  const [editing, setEditing] = useState(false);
  const ref = useRef<HTMLLIElement>(null);
  const draft = round.state === 'draft';
  useEffect(() => {
    if (active) ref.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  }, [active]);
  return (
    <li
      ref={ref}
      id={`c-${c.id}`}
      className={`ccard ccard-${draft ? 'draft' : c.state}${active ? ' active' : ''}`}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest('button, a, textarea, input')) return;
        if (anchorOn(c, view.revision.n)) onActivate(c.id);
      }}
    >
      <div className="ccard-head">
        {!draft && <span className={`cstate cstate-${c.state}`}>{t(`rv.cstate.${c.state}`)}</span>}
        <Time iso={c.created_at} />
      </div>
      <Quote c={c} onShow={onActivate} viewRev={view.revision.n} latest={view.latest} taskId={view.task.id} slug={view.slug} />
      {editing ? (
        <Composer
          initial={c.body}
          placeholder={t('sel.placeholder')}
          submitLabel={t('rv.save')}
          autoFocus
          onCancel={() => setEditing(false)}
          onSubmit={(text) => api.updateDraftComment(c.id, text).then(() => setEditing(false))}
        />
      ) : (
        <SmallMarkdown text={c.body} />
      )}
      {c.replies.length > 0 && (
        <ol className="replies">
          {c.replies.map((r) => (
            <li key={r.id} className={`reply reply-${r.author}`}>
              <div className="reply-head">
                <span className={`author author-${r.author}`}>{t(`author.${r.author}`)}</span>
                <Time iso={r.created_at} />
              </div>
              <SmallMarkdown text={r.body} />
            </li>
          ))}
        </ol>
      )}
      {replying && (
        <Composer
          placeholder={t('rv.replyPh')}
          submitLabel={t('rv.send')}
          autoFocus
          rows={2}
          onCancel={() => setReplying(false)}
          onSubmit={(text) => api.reply(c.id, text).then(() => setReplying(false))}
        />
      )}
      {!editing && !replying && (
        <div className="ccard-actions">
          {draft ? (
            <>
              <button type="button" className="link-btn" onClick={() => setEditing(true)}>
                <Icon name="edit" size={13} />
                {t('rv.edit')}
              </button>
              <button type="button" className="link-btn danger" onClick={() => api.deleteDraftComment(c.id)}>
                <Icon name="trash" size={13} />
                {t('rv.delete')}
              </button>
            </>
          ) : (
            <>
              <button type="button" className="link-btn" onClick={() => setReplying(true)}>
                <Icon name="reply" size={13} />
                {t('rv.reply')}
              </button>
              {c.state === 'open' && (
                <button type="button" className="link-btn" onClick={() => api.setCommentState(c.id, 'resolved')}>
                  <Icon name="check" size={13} />
                  {t('rv.resolve')}
                </button>
              )}
              {c.state === 'resolved' && (
                <button type="button" className="link-btn" onClick={() => api.setCommentState(c.id, 'open')}>
                  {t('rv.reopen')}
                </button>
              )}
            </>
          )}
        </div>
      )}
    </li>
  );
}

/** Comments sorted: anchored by position in the viewed revision, then the rest, then general. */
function ordered(cs: Comment[], n: number): Comment[] {
  const pos = (c: Comment) => anchorOn(c, n)?.start ?? (c.anchor ? 1e9 : 2e9);
  return [...cs].sort((a, b) => pos(a) - pos(b));
}

function DraftCard({ view, draft, active, onActivate }: {
  view: DocumentView;
  draft: ReviewRound | undefined;
  active: number | null;
  onActivate: (id: number) => void;
}) {
  const { t, count } = useI18n();
  const [summary, setSummary] = useState(draft?.summary ?? '');
  const [sent, setSent] = useState(false);
  useEffect(() => setSummary(draft?.summary ?? ''), [draft?.id, draft?.summary]);
  const n = draft?.comments.length ?? 0;
  return (
    <section className="draft-card" aria-label={t('rv.draftTitle')}>
      <div className="draft-head">
        <h3>{t('rv.draftTitle')}</h3>
        {draft && <span className="round-state round-draft">{t('rv.draftOn', { n: draft.revision })}</span>}
      </div>
      {sent && !draft && <p className="notice notice-ok">{t('rv.submittedNote')}</p>}
      {draft && draft.revision !== view.latest && <p className="notice notice-warn">{t('rv.draftOld', { n: draft.revision })}</p>}
      {n === 0 && !sent && <p className="muted small">{t('rv.startHint')}</p>}
      {draft && n > 0 && (
        <ul className="ccards">
          {ordered(draft.comments, view.revision.n).map((c) => (
            <CommentCard key={c.id} c={c} round={draft} view={view} active={active === c.id} onActivate={onActivate} />
          ))}
        </ul>
      )}
      <details className="general-box">
        <summary>
          <Icon name="plus" size={13} />
          {t('rv.general')}
        </summary>
        <Composer
          placeholder={t('rv.generalPh')}
          submitLabel={t('rv.add')}
          rows={2}
          onSubmit={(text) => api.addDraftComment(view.task.id, view.slug, { anchor: null, body: text }).then(() => setSent(false))}
        />
      </details>
      {draft && (
        <>
          <label className="field">
            <span>{t('rv.summary')}</span>
            <textarea
              rows={2}
              value={summary}
              placeholder={t('rv.summaryPh')}
              onChange={(e) => setSummary(e.target.value)}
              onBlur={() => summary !== (draft.summary ?? '') && api.setRoundSummary(draft.id, summary)}
            />
          </label>
          <div className="draft-actions">
            <button
              type="button"
              className="link-btn danger"
              onClick={() => {
                if (confirm(t('rv.discardConfirm', { n }))) api.discardRound(draft.id);
              }}
            >
              {t('rv.discard')}
            </button>
            <button
              type="button"
              className="btn btn-primary"
              disabled={n === 0 && !summary.trim()}
              onClick={async () => {
                if (summary !== (draft.summary ?? '')) await api.setRoundSummary(draft.id, summary);
                await api.submitRound(draft.id);
                setSent(true);
              }}
            >
              {n ? t('rv.submitN', { n: count(n, 'comment') }) : t('rv.submit')}
            </button>
          </div>
        </>
      )}
    </section>
  );
}

/**
 * The owner's "go": approve the latest revision (with the draft's comments, if any), see
 * that it is approved, withdraw it. An older revision only says whether it was approved.
 */
function ApprovalCard({ view, draft }: { view: DocumentView; draft: ReviewRound | undefined }) {
  const { t, dateTime } = useI18n();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const n = view.revision.n;
  const isLatest = n === view.latest;
  if (!isLatest) {
    if (!view.approved_revisions.includes(n)) return null;
    return (
      <p className="approval-past">
        <Icon name="check" size={13} />
        {t('ap.wasApproved', { n })}
      </p>
    );
  }
  const run = async (f: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await f();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  const comments = draft?.comments.length ?? 0;
  if (view.approved) {
    return (
      <section className="approval-card approved" aria-label={t('ap.title')}>
        <span className="approval-state">
          <Icon name="check" size={14} />
          {t('ap.approved', { n: view.approved.revision })}
        </span>
        <span className="muted small">{dateTime(view.approved.at)}</span>
        <button type="button" className="link-btn" disabled={busy} onClick={() => run(() => api.revokeApproval(view.task.id, view.slug))}>
          {t('ap.revoke')}
        </button>
        {error && <p className="notice notice-warn">{error}</p>}
      </section>
    );
  }
  return (
    <section className="approval-card" aria-label={t('ap.title')}>
      <button
        type="button"
        className="btn btn-approve"
        disabled={busy}
        title={t('ap.hint')}
        onClick={() => run(() => api.approveDocument(view.task.id, view.slug, n, comments > 0))}
      >
        <Icon name="check" size={14} />
        {comments > 0 ? t('ap.approveWith', { n, c: comments }) : t('ap.approve', { n })}
      </button>
      {error && <p className="notice notice-warn">{error}</p>}
    </section>
  );
}

function RoundBlock({ round, view, filter, active, onActivate, focus }: {
  round: ReviewRound;
  view: DocumentView;
  filter: 'open' | 'all';
  active: number | null;
  onActivate: (id: number) => void;
  focus?: boolean;
}) {
  const { t, dateTime } = useI18n();
  const ref = useRef<HTMLElement>(null);
  useEffect(() => {
    if (focus) ref.current?.scrollIntoView({ block: 'start' });
  }, [focus]);
  const comments = ordered(round.comments, view.revision.n).filter((c) => filter === 'all' || c.state === 'open');
  if (!comments.length && filter === 'open' && !focus) return null;
  return (
    <section ref={ref} id={`round-${round.id}`} className={`round round-block-${round.state}${focus ? ' focus' : ''}`}>
      <header className="round-head">
        <span className="round-title">
          {t('rv.round', { n: round.n })} <span className="muted">{t('rv.onRev', { n: round.revision })}</span>
        </span>
        <span className={`round-state round-${round.state}`}>{t(`rv.state.${round.state}`)}</span>
        {round.addressed_in && (
          <a className="round-link" href={href(paths.diff(view.task.id, view.slug, round.revision, round.addressed_in))}>
            {t('rv.addressedIn', { n: round.addressed_in })}
          </a>
        )}
      </header>
      {round.summary && (
        <div className="round-summary">
          <span className="author author-owner">{t('author.owner')}</span>
          {round.submitted_at && <time title={dateTime(round.submitted_at)}>{dateTime(round.submitted_at)}</time>}
          <SmallMarkdown text={round.summary} />
        </div>
      )}
      {round.addressed_message && (
        <div className="round-summary round-summary-agent">
          <span className="author author-agent">{t('author.agent')}</span>
          {round.addressed_at && <time>{dateTime(round.addressed_at)}</time>}
          <SmallMarkdown text={round.addressed_message} />
        </div>
      )}
      <ul className="ccards">
        {comments.map((c) => (
          <CommentCard key={c.id} c={c} round={round} view={view} active={active === c.id} onActivate={onActivate} />
        ))}
      </ul>
    </section>
  );
}

export function ReviewPanel({ view, active, onActivate, focusRound }: {
  view: DocumentView;
  active: number | null;
  onActivate: (id: number) => void;
  /** A search hit in a round summary (?round=): show all and scroll to it. */
  focusRound?: number;
}) {
  const { t } = useI18n();
  const isLatest = view.revision.n === view.latest;
  const [filter, setFilter] = useState<'open' | 'all'>(isLatest && focusRound === undefined ? 'open' : 'all');
  const draft = view.rounds.find((r) => r.state === 'draft');
  // An older revision shows the rounds made on it; the latest shows all of them.
  const others = view.rounds.filter((r) => r.state !== 'draft' && (isLatest || r.revision === view.revision.n || r.id === focusRound));
  const all = others.flatMap((r) => r.comments);
  const openN = all.filter((c) => c.state === 'open').length;
  // Activating a resolved/outdated comment from the document switches the filter to show it.
  useEffect(() => {
    if (active && all.some((c) => c.id === active && c.state !== 'open')) setFilter('all');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active]);
  const visible = others.some((r) => r.comments.some((c) => filter === 'all' || c.state === 'open'));
  return (
    <div className="review-panel">
      <ApprovalCard view={view} draft={draft} />
      {isLatest && <DraftCard view={view} draft={draft} active={active} onActivate={onActivate} />}
      {others.length > 0 && (
        <>
          <div className="panel-filter">
            <div className="seg seg-sm" role="group">
              <button type="button" className={`seg-item${filter === 'open' ? ' on' : ''}`} aria-pressed={filter === 'open'} onClick={() => setFilter('open')}>
                {t('rv.filter.open')} <span className="count">{openN}</span>
              </button>
              <button type="button" className={`seg-item${filter === 'all' ? ' on' : ''}`} aria-pressed={filter === 'all'} onClick={() => setFilter('all')}>
                {t('rv.filter.all')} <span className="count">{all.length}</span>
              </button>
            </div>
          </div>
          {visible ? (
            others.map((r) => <RoundBlock key={r.id} round={r} view={view} filter={filter} active={active} onActivate={onActivate} focus={r.id === focusRound} />)
          ) : (
            <p className="muted small panel-empty">{t('rv.noComments')}</p>
          )}
        </>
      )}
    </div>
  );
}

export { Composer };
