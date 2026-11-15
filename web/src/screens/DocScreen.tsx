import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { anchorOn, api, ConflictError, useQuery, type Anchor, type DocumentView } from '../api';
import { HistoryPanel } from '../components/HistoryPanel';
import { Icon } from '../components/Icon';
import { DocEditor } from '../components/DocEditor';
import { Markdown } from '../components/Markdown';
import type { ItemChange } from '../components/PlanItem';
import { Composer, ReviewPanel } from '../components/ReviewPanel';
import { AuthorBadge, ErrorView, Loading, Progress, Time } from '../components/ui';
import { offsetsToRanges, pointFromXY, rangeToOffsets } from '../lib/anchorDom';
import { WIDE, useMedia } from '../lib/hooks';
import { cpToUtf16, makeAnchor } from '../lib/markdown';
import { useI18n } from '../i18n';
import { href, navigate, paths } from '../router';

type Tab = 'review' | 'history';
type HlKind = 'open' | 'resolved' | 'outdated' | 'draft';

interface Sel {
  start: number;
  end: number;
  /** Position relative to the article, for the floating button. */
  x: number;
  y: number;
  bottom: number;
}

const HL_NAMES = ['c-search', 'c-open', 'c-resolved', 'c-outdated', 'c-draft', 'c-active', 'c-pending'];

/** Ranges of every case-insensitive occurrence of `word` in the rendered text of `root`. */
function findText(root: Element, word: string): Range[] {
  const out: Range[] = [];
  const needle = word.toLowerCase();
  if (!needle) return out;
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode() as Text | null; n; n = walker.nextNode() as Text | null) {
    if ((n.parentElement as Element | null)?.closest('[data-noanchor]')) continue;
    const s = n.data.toLowerCase();
    for (let i = s.indexOf(needle); i >= 0; i = s.indexOf(needle, i + needle.length)) {
      const r = document.createRange();
      r.setStart(n, i);
      r.setEnd(n, i + needle.length);
      out.push(r);
    }
  }
  return out;
}

/** CSS Custom Highlight API: highlights without touching React's DOM. */
function setHighlights(groups: Record<string, Range[]>) {
  const reg = (globalThis as { CSS?: { highlights?: Map<string, unknown> } }).CSS?.highlights;
  const H = (globalThis as { Highlight?: new (...r: Range[]) => unknown }).Highlight;
  if (!reg || !H) return;
  for (const name of HL_NAMES) {
    const rs = groups[name];
    if (rs?.length) reg.set(name, new H(...rs));
    else reg.delete(name);
  }
}

function useHighlights(
  article: React.RefObject<HTMLElement | null>,
  view: DocumentView | undefined,
  active: number | null,
  pending: Anchor | null,
  search: { word?: string; at?: [number, number] } | null,
) {
  const ranges = useRef(new Map<number, Range[]>());
  const searchRanges = useRef<Range[]>([]);
  useLayoutEffect(() => {
    const el = article.current;
    if (!el || !view) return;
    const body = view.revision.body;
    const groups: Record<string, Range[]> = {};
    // A search hit: ?at=start-end (code points in the body), or ?hl=word from older servers.
    searchRanges.current = search?.at
      ? offsetsToRanges(el, cpToUtf16(body, search.at[0]), cpToUtf16(body, search.at[1]), body)
      : search?.word
        ? findText(el, search.word)
        : [];
    groups['c-search'] = searchRanges.current;
    ranges.current = new Map();
    for (const r of view.rounds)
      for (const c of r.comments) {
        const a = anchorOn(c, view.revision.n);
        if (!a) continue;
        // On the revision where its text disappeared an outdated comment is shown as such.
        const kind: HlKind = r.state === 'draft' ? 'draft' : c.state;
        const rs = offsetsToRanges(el, a.start, a.end, body);
        ranges.current.set(c.id, rs);
        const name = c.id === active ? 'c-active' : `c-${kind}`;
        // Resolved comments stay quiet unless active.
        if (kind === 'resolved' && c.id !== active) continue;
        (groups[name] ??= []).push(...rs);
      }
    if (pending) groups['c-pending'] = offsetsToRanges(el, pending.start, pending.end, body);
    setHighlights(groups);
  });
  useEffect(() => () => setHighlights({}), []);
  return { ranges, searchRanges };
}

function DocHeader({ view, onEdit, onRevert }: { view: DocumentView; onEdit?: () => void; onRevert: (n: number) => void }) {
  const { t } = useI18n();
  const r = view.revision;
  const isLatest = r.n === view.latest;
  const done = view.items.filter((i) => i.done).length;
  return (
    <header className="doc-head">
      <nav className="crumbs" aria-label="breadcrumbs">
        <a href={href('/')}>{t('nav.tasks')}</a>
        <Icon name="chevronRight" size={12} />
        <a href={href(paths.task(view.task.id))} className="crumb-task">
          <span className="mono">{view.task.id}</span> <span className="crumb-title">{view.task.title}</span>
        </a>
      </nav>
      <h1>{view.title}</h1>
      <div className="doc-meta">
        <span className={`kind kind-${view.kind}`}>{t(`kind.${view.kind}`)}</span>
        <label className="rev-pick">
          <span className="sr-only">{t('hist.title')}</span>
          <select
            value={r.n}
            onChange={(e) => {
              const n = Number(e.target.value);
              navigate(paths.doc(view.task.id, view.slug, n === view.latest ? undefined : n));
            }}
          >
            {view.revisions.map((x) => (
              <option key={x.n} value={x.n}>
                {t('doc.revOf', { n: x.n, m: view.latest })}
                {x.n === view.latest ? ` · ${t('doc.latest')}` : ''}
              </option>
            ))}
          </select>
          <Icon name="chevronDown" size={14} />
        </label>
        <span className="doc-by">
          <AuthorBadge author={r.author} /> <Time iso={r.created_at} />
        </span>
        {view.items.length > 0 && <Progress done={done} total={view.items.length} />}
        {view.approved_revisions.includes(r.n) && (
          <span className="approved-badge" title={r.n === view.latest ? t('ap.approved', { n: r.n }) : t('ap.wasApproved', { n: r.n })}>
            <Icon name="check" size={12} />
            {t('ap.badge')}
          </span>
        )}
        {r.n > 1 && (
          <a className="btn btn-sm btn-ghost" href={href(paths.diff(view.task.id, view.slug, r.n - 1, r.n))}>
            <Icon name="diff" size={14} />
            {t('doc.diffTo', { n: r.n - 1 })}
          </a>
        )}
        {onEdit && (
          <button type="button" className="btn btn-sm doc-edit-btn" onClick={onEdit}>
            <Icon name="edit" size={14} />
            {t('edit.start')}
          </button>
        )}
      </div>
      {r.message && <p className="doc-msg">{r.message}</p>}
      {!isLatest && (
        <div className="banner">
          <p>{t('doc.oldBanner', { n: r.n, m: view.latest })}</p>
          <div className="banner-actions">
            <a className="btn btn-sm btn-primary" href={href(paths.doc(view.task.id, view.slug))}>
              {t('doc.toLatest')}
            </a>
            <a className="btn btn-sm" href={href(paths.diff(view.task.id, view.slug, r.n, view.latest))}>
              <Icon name="diff" size={14} />
              r{r.n} → r{view.latest}
            </a>
            <button type="button" className="btn btn-sm" onClick={() => onRevert(r.n)}>
              <Icon name="history" size={14} />
              {t('rev.button', { n: r.n })}
            </button>
          </div>
        </div>
      )}
    </header>
  );
}

function Rail({ view, tab, setTab, active, onActivate, onClose, focusRound, onRevert }: {
  view: DocumentView;
  tab: Tab;
  setTab: (t: Tab) => void;
  active: number | null;
  onActivate: (id: number) => void;
  onClose?: () => void;
  focusRound?: number;
  onRevert?: (n: number) => void;
}) {
  const { t } = useI18n();
  const draft = view.rounds.find((r) => r.state === 'draft');
  const open = view.rounds.filter((r) => r.state !== 'draft').flatMap((r) => r.comments).filter((c) => c.state === 'open').length;
  return (
    <div className="rail-inner">
      <div className="rail-tabs" role="tablist">
        <button type="button" role="tab" aria-selected={tab === 'review'} className={`rail-tab${tab === 'review' ? ' on' : ''}`} onClick={() => setTab('review')}>
          <Icon name="comment" size={14} />
          {t('doc.review')}
          {(draft?.comments.length ?? 0) + open > 0 && <span className="pill">{(draft?.comments.length ?? 0) + open}</span>}
        </button>
        <button type="button" role="tab" aria-selected={tab === 'history'} className={`rail-tab${tab === 'history' ? ' on' : ''}`} onClick={() => setTab('history')}>
          <Icon name="history" size={14} />
          {t('doc.history')}
          <span className="pill pill-quiet">{view.latest}</span>
        </button>
        {onClose && (
          <button type="button" className="btn btn-ghost btn-icon rail-close" onClick={onClose} aria-label={t('rv.close')}>
            <Icon name="x" size={16} />
          </button>
        )}
      </div>
      <div className="rail-body">
        {tab === 'review' ? (
          <ReviewPanel view={view} active={active} onActivate={onActivate} focusRound={focusRound} />
        ) : (
          <HistoryPanel taskId={view.task.id} slug={view.slug} revisions={view.revisions} rounds={view.rounds} current={view.revision.n} onRevert={onRevert} approved={view.approved_revisions} />
        )}
      </div>
    </div>
  );
}

export function DocScreen({ id, slug, rev, query }: { id: string; slug: string; rev?: number; query: URLSearchParams }) {
  const { t } = useI18n();
  const wide = useMedia(WIDE);
  const res = useQuery(`doc:${id}/${slug}/${rev ?? ''}`, () => api.getDocument(id, slug, rev));
  const view = res.data;
  const article = useRef<HTMLElement>(null);
  const [active, setActive] = useState<number | null>(query.get('c') ? Number(query.get('c')) : null);
  const [tab, setTab] = useState<Tab>('review');
  const [sheet, setSheet] = useState(false);
  const [sel, setSel] = useState<Sel | null>(null);
  const [composing, setComposing] = useState<(Anchor & { y: number; x: number }) | null>(null);
  const isLatest = !!view && view.revision.n === view.latest;
  const [editing, setEditing] = useState(false);
  const [saved, setSaved] = useState<{ n: number; changed: boolean } | null>(null);
  const atParam = query.get('at')?.match(/^(\d+)-(\d+)$/);
  const hlWord = query.get('hl');
  const hl = useMemo(
    () => (atParam ? { at: [Number(atParam[1]), Number(atParam[2])] as [number, number] } : hlWord ? { word: hlWord } : null),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [query.get('at'), hlWord],
  );
  const focusRound = query.get('round') ? Number(query.get('round')) : undefined;
  const revertedFrom = query.get('reverted') ? Number(query.get('reverted')) : undefined;
  const { ranges, searchRanges } = useHighlights(editing ? { current: null } : article, view, active, composing, hl);
  const onItem = useCallback<ItemChange>((it, change) => api.setItem(id, slug, it.id, change), [id, slug]);

  // A search hit (?hl=word) scrolls to the first match once the document is rendered.
  const hlDone = useRef(false);
  useEffect(() => {
    if (!view || !hl || hlDone.current) return;
    hlDone.current = true;
    requestAnimationFrame(() => {
      const r = searchRanges.current[0];
      if (!r) return;
      const rect = r.getBoundingClientRect();
      window.scrollTo({ top: Math.max(0, rect.top + window.scrollY - window.innerHeight * 0.3) });
    });
  }, [view, hl, searchRanges]);

  // Scroll the document to a comment's text.
  const scrollToComment = useCallback(
    (cid: number) => {
      const rs = ranges.current.get(cid);
      if (!rs?.length) return;
      const rect = rs[0].getBoundingClientRect();
      const top = rect.top + window.scrollY - window.innerHeight * 0.3;
      window.scrollTo({ top: Math.max(0, top), behavior: 'smooth' });
    },
    [ranges],
  );

  const activate = useCallback(
    (cid: number) => {
      setActive(cid);
      setTab('review');
      if (!wide) setSheet(false);
      requestAnimationFrame(() => scrollToComment(cid));
    },
    [wide, scrollToComment],
  );

  // Deep link ?c=<comment> scrolls to it once the document is rendered.
  const linked = useRef(false);
  useEffect(() => {
    if (view && active && !linked.current) {
      linked.current = true;
      requestAnimationFrame(() => scrollToComment(active));
    }
  }, [view, active, scrollToComment]);

  // Track the text selection inside the article.
  useEffect(() => {
    if (!view || !isLatest) return;
    let timer = 0;
    const update = () => {
      const el = article.current;
      const s = document.getSelection();
      if (!el || !s || s.isCollapsed || s.rangeCount === 0) return setSel(null);
      const range = s.getRangeAt(0);
      if (!el.contains(range.commonAncestorContainer)) return setSel(null);
      const off = rangeToOffsets(el, range, view.revision.body);
      if (!off) return setSel(null);
      const rects = range.getClientRects();
      const first = rects[0] ?? range.getBoundingClientRect();
      const last = rects[rects.length - 1] ?? first;
      const box = el.getBoundingClientRect();
      setSel({ ...off, x: first.left - box.left + Math.min(first.width, 240) / 2, y: first.top - box.top, bottom: last.bottom - box.top });
    };
    const onChange = () => {
      clearTimeout(timer);
      timer = window.setTimeout(update, 120);
    };
    document.addEventListener('selectionchange', onChange);
    return () => {
      clearTimeout(timer);
      document.removeEventListener('selectionchange', onChange);
    };
  }, [view, isLatest]);

  const startComment = () => {
    if (!sel || !view) return;
    setComposing({ ...makeAnchor(view.revision.body, sel.start, sel.end), x: sel.x, y: sel.bottom });
    setSel(null);
    document.getSelection()?.removeAllRanges();
  };

  // Click on highlighted text activates its comment.
  const onArticleClick = (e: React.MouseEvent) => {
    const s = document.getSelection();
    if (s && !s.isCollapsed) return;
    const p = pointFromXY(e.clientX, e.clientY);
    if (!p) return;
    for (const [cid, rs] of ranges.current) {
      if (rs.some((r) => r.isPointInRange(p.node, p.offset))) {
        setActive(cid);
        setTab('review');
        if (!wide) setSheet(true);
        return;
      }
    }
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setComposing(null);
        setSheet(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  if (res.error) return <ErrorView error={res.error} />;
  if (!view) return <Loading />;

  // Revert: a new revision by the owner with the text of revision n.
  const revert = async (n: number) => {
    if (!confirm(t('rev.confirm', { n, m: view.latest + 1 }))) return;
    try {
      await api.revertDocument(id, slug, n, view.latest);
      navigate(`${paths.doc(id, slug)}?reverted=${n}`);
    } catch (e) {
      alert(e instanceof ConflictError ? t('rev.conflict', { n: e.latest }) : (e as Error).message);
    }
  };

  const draftN = view.rounds.find((r) => r.state === 'draft')?.comments.length ?? 0;
  const openN = view.rounds.filter((r) => r.state !== 'draft').flatMap((r) => r.comments).filter((c) => c.state === 'open').length;

  const composer = composing && (
    <div
      className={wide ? 'sel-composer' : 'sheet sheet-composer'}
      style={wide ? { top: composing.y + 8, left: Math.max(0, Math.min(composing.x - 180, (article.current?.clientWidth ?? 400) - 360)) } : undefined}
      role="dialog"
      aria-label={t('sel.comment')}
    >
      <blockquote className="c-quote">{composing.quote.length > 200 ? composing.quote.slice(0, 197) + '…' : composing.quote}</blockquote>
      <Composer
        placeholder={t('sel.placeholder')}
        submitLabel={t('sel.add')}
        autoFocus
        onCancel={() => setComposing(null)}
        onSubmit={async (text) => {
          const { x: _x, y: _y, ...anchor } = composing;
          const c = await api.addDraftComment(view.task.id, view.slug, { anchor, body: text });
          setComposing(null);
          setActive(c.id);
          setTab('review');
        }}
      />
    </div>
  );

  return (
    <div className={`page doc-page${wide ? ' wide' : ''}`}>
      <DocHeader
        view={view}
        onRevert={revert}
        onEdit={
          isLatest && !editing
            ? () => {
                setSaved(null);
                setEditing(true);
              }
            : undefined
        }
      />
      {saved && !editing && <p className="notice notice-ok doc-notice">{saved.changed ? t('edit.saved', { n: saved.n }) : t('edit.unchanged')}</p>}
      {revertedFrom && isLatest && <p className="notice notice-ok doc-notice">{t('rev.done', { n: revertedFrom, m: view.latest })}</p>}
      <div className="doc-layout">
        {editing ? (
          <DocEditor
            view={view}
            onClose={(result) => {
              setEditing(false);
              setSaved(result ?? null);
            }}
          />
        ) : (
        <article
          ref={article}
          className={`doc-body${isLatest ? ' commentable' : ''}`}
          onClick={onArticleClick}
          lang={/[а-яё]/i.test(view.revision.body.slice(0, 400)) ? 'ru' : 'en'}
        >
          <Markdown body={view.revision.body} items={view.items} anchors onItem={isLatest ? onItem : undefined} />
          {sel && wide && !composing && (
            <button
              type="button"
              className="sel-btn"
              style={{ top: Math.max(0, sel.y - 40), left: Math.max(0, sel.x - 50) }}
              onMouseDown={(e) => e.preventDefault()}
              onClick={startComment}
              title={t('sel.commentHint')}
            >
              <Icon name="comment" size={14} />
              {t('sel.comment')}
            </button>
          )}
          {wide && composer}
        </article>
        )}
        {wide && (
          <aside className="rail" aria-label={t('doc.review')}>
            <Rail view={view} tab={tab} setTab={setTab} active={active} onActivate={activate} focusRound={focusRound} onRevert={revert} />
          </aside>
        )}
      </div>

      {!wide && (
        <>
          <div className="bottom-bar">
            {sel && !composing ? (
              <button type="button" className="btn btn-primary bottom-main" onMouseDown={(e) => e.preventDefault()} onClick={startComment}>
                <Icon name="comment" size={16} />
                {t('sel.commentHint')}
              </button>
            ) : (
              <>
                <button
                  type="button"
                  className="btn bottom-main"
                  onClick={() => {
                    setTab('review');
                    setSheet(true);
                  }}
                >
                  <Icon name="comment" size={16} />
                  {t('doc.review')}
                  {draftN > 0 && <span className="pill pill-draft">{draftN}</span>}
                  {openN > 0 && <span className="pill">{openN}</span>}
                </button>
                <button
                  type="button"
                  className="btn"
                  onClick={() => {
                    setTab('history');
                    setSheet(true);
                  }}
                >
                  <Icon name="history" size={16} />
                  {t('doc.history')}
                </button>
              </>
            )}
          </div>
          {sheet && (
            <>
              <div className="scrim" onClick={() => setSheet(false)} />
              <div className="sheet" role="dialog" aria-label={t('doc.review')}>
                <Rail view={view} tab={tab} setTab={setTab} active={active} onActivate={activate} onClose={() => setSheet(false)} focusRound={focusRound} onRevert={revert} />
              </div>
            </>
          )}
          {composing && (
            <>
              <div className="scrim" onClick={() => setComposing(null)} />
              {composer}
            </>
          )}
        </>
      )}
    </div>
  );
}
