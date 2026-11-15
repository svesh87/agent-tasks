import { diffLines, diffWords } from 'diff';
import { useMemo, useState, type ReactNode } from 'react';
import { api, useQuery, type DiffView } from '../api';
import { Icon } from '../components/Icon';
import { SmallMarkdown } from '../components/Markdown';
import { AuthorBadge, ErrorView, Loading, Time } from '../components/ui';
import { WIDE, useMedia } from '../lib/hooks';
import { similarity } from '../lib/markdown';
import { load, save } from '../lib/storage';
import { useI18n } from '../i18n';
import { href, navigate, paths } from '../router';

type Kind = 'same' | 'add' | 'del';
interface Line {
  kind: Kind;
  a?: number;
  b?: number;
  text: string;
  /** Word-level parts when the line pairs with a changed line on the other side. */
  parts?: { text: string; changed: boolean }[];
}
type Block = { kind: 'lines'; lines: Line[] } | { kind: 'gap'; lines: Line[] };

const CONTEXT = 3;
// Word boundaries for any script (diff's own tokenizer splits Cyrillic words into pieces).
const segmenter = new Intl.Segmenter(undefined, { granularity: 'word' });

function buildLines(a: string, b: string): Line[] {
  const out: Line[] = [];
  let na = 1;
  let nb = 1;
  const changes = diffLines(a, b);
  for (let i = 0; i < changes.length; i++) {
    const c = changes[i];
    const ls = c.value.replace(/\n$/, '').split('\n');
    if (!c.added && !c.removed) {
      ls.forEach((text) => out.push({ kind: 'same', a: na++, b: nb++, text }));
      continue;
    }
    if (c.removed && changes[i + 1]?.added) {
      // Pair removed and added lines one to one for word-level highlights.
      const add = changes[i + 1].value.replace(/\n$/, '').split('\n');
      const dels: Line[] = ls.map((text) => ({ kind: 'del', a: na++, text }));
      const adds: Line[] = add.map((text) => ({ kind: 'add', b: nb++, text }));
      // Pair each removed line with the most similar later added line (order kept).
      let from = 0;
      for (const d of dels) {
        let best = -1;
        let score = 0.5;
        for (let k = from; k < adds.length; k++) {
          const s = similarity(d.text, adds[k].text);
          if (s > score) {
            best = k;
            score = s;
          }
        }
        if (best < 0) continue;
        const a = adds[best];
        from = best + 1;
        const w = diffWords(d.text, a.text, { intlSegmenter: segmenter });
        const dp = w.filter((p) => !p.added).map((p) => ({ text: p.value, changed: !!p.removed }));
        const ap = w.filter((p) => !p.removed).map((p) => ({ text: p.value, changed: !!p.added }));
        // diffWords normalises whitespace in common parts; use the parts only if they rebuild the line.
        if (dp.map((p) => p.text).join('') === d.text) d.parts = dp;
        if (ap.map((p) => p.text).join('') === a.text) a.parts = ap;
      }
      out.push(...dels, ...adds);
      i++;
      continue;
    }
    ls.forEach((text) => out.push(c.added ? { kind: 'add', b: nb++, text } : { kind: 'del', a: na++, text }));
  }
  return out;
}

function toBlocks(lines: Line[]): Block[] {
  const changed = lines.map((l) => l.kind !== 'same');
  const keep = lines.map((_, i) => {
    for (let k = Math.max(0, i - CONTEXT); k <= Math.min(lines.length - 1, i + CONTEXT); k++) if (changed[k]) return true;
    return false;
  });
  const blocks: Block[] = [];
  lines.forEach((l, i) => {
    const kind = keep[i] ? 'lines' : 'gap';
    const last = blocks[blocks.length - 1];
    if (last && last.kind === kind) last.lines.push(l);
    else blocks.push({ kind, lines: [l] } as Block);
  });
  return blocks;
}

function Text({ line }: { line: Line }): ReactNode {
  if (!line.parts) return line.text || ' ';
  return line.parts.map((p, i) => (p.changed ? <mark key={i}>{p.text}</mark> : <span key={i}>{p.text}</span>));
}

function Gap({ lines, render }: { lines: Line[]; render: (ls: Line[]) => ReactNode }) {
  const { count } = useI18n();
  const [open, setOpen] = useState(false);
  if (open) return <>{render(lines)}</>;
  return (
    <button type="button" className="diff-gap" onClick={() => setOpen(true)}>
      <Icon name="chevronDown" size={13} />
      {count(lines.length, 'line')}
    </button>
  );
}

function Unified({ blocks }: { blocks: Block[] }) {
  const render = (ls: Line[]) =>
    ls.map((l, i) => (
      <div key={i} className={`dl dl-${l.kind}`}>
        <span className="dn">{l.a ?? ''}</span>
        <span className="dn">{l.b ?? ''}</span>
        <span className="dm" aria-hidden="true">{l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ''}</span>
        <span className="dt">
          <Text line={l} />
        </span>
      </div>
    ));
  return (
    <div className="diff diff-unified">
      {blocks.map((b, i) => (b.kind === 'gap' ? <Gap key={i} lines={b.lines} render={render} /> : <div key={i}>{render(b.lines)}</div>))}
    </div>
  );
}

function Split({ blocks }: { blocks: Block[] }) {
  const render = (ls: Line[]) => {
    // Rows: same lines on both sides; runs of del/add side by side.
    const rows: [Line | null, Line | null][] = [];
    for (let i = 0; i < ls.length; ) {
      if (ls[i].kind === 'same') {
        rows.push([ls[i], ls[i]]);
        i++;
        continue;
      }
      const dels: Line[] = [];
      const adds: Line[] = [];
      while (i < ls.length && ls[i].kind === 'del') dels.push(ls[i++]);
      while (i < ls.length && ls[i].kind === 'add') adds.push(ls[i++]);
      for (let k = 0; k < Math.max(dels.length, adds.length); k++) rows.push([dels[k] ?? null, adds[k] ?? null]);
    }
    return rows.map(([l, r], i) => (
      <div key={i} className="drow">
        <div className={`dl dl-${l ? (l.kind === 'same' ? 'same' : 'del') : 'empty'}`}>
          <span className="dn">{l?.a ?? ''}</span>
          <span className="dt">{l ? <Text line={l} /> : ' '}</span>
        </div>
        <div className={`dl dl-${r ? (r.kind === 'same' ? 'same' : 'add') : 'empty'}`}>
          <span className="dn">{r?.b ?? ''}</span>
          <span className="dt">{r ? <Text line={r} /> : ' '}</span>
        </div>
      </div>
    ));
  };
  return (
    <div className="diff diff-split">
      {blocks.map((b, i) => (b.kind === 'gap' ? <Gap key={i} lines={b.lines} render={render} /> : <div key={i}>{render(b.lines)}</div>))}
    </div>
  );
}

function Rounds({ v }: { v: DiffView }) {
  const { t, count } = useI18n();
  return (
    <section className="diff-rounds">
      <h2 className="side-title">{t('diff.rounds')}</h2>
      {v.rounds.length === 0 && <p className="muted small">{t('diff.noRounds')}</p>}
      {v.rounds.map((r) => (
        <div key={r.id} className="round">
          <header className="round-head">
            <span className="round-title">
              {t('rv.round', { n: r.n })} <span className="muted">{t('rv.onRev', { n: r.revision })}</span>
            </span>
            <span className={`round-state round-${r.state}`}>{t(`rv.state.${r.state}`)}</span>
          </header>
          <ul className="mini-comments">
            {r.comments.map((c) => (
              <li key={c.id}>
                <span className={`cstate cstate-${c.state}`}>{t(`rv.cstate.${c.state}`)}</span>
                {c.anchor && <q className="mini-quote">{c.anchor.quote.length > 80 ? c.anchor.quote.slice(0, 77) + '…' : c.anchor.quote}</q>}
                <SmallMarkdown text={c.body} />
                {c.replies.filter((x) => x.author === 'agent').slice(-1).map((x) => (
                  <div key={x.id} className="mini-reply">
                    <span className="author author-agent">{t('author.agent')}</span> <SmallMarkdown text={x.body} />
                  </div>
                ))}
              </li>
            ))}
          </ul>
          <a className="link-btn" href={href(`${paths.doc(v.task.id, v.slug, r.revision === v.latest ? undefined : r.revision)}`)}>
            {t('rv.showOn', { n: r.revision })} · {count(r.comments.length, 'comment')}
          </a>
        </div>
      ))}
    </section>
  );
}

export function DiffScreen({ id, slug, from, to }: { id: string; slug: string; from: number; to: number }) {
  const { t } = useI18n();
  const wide = useMedia(WIDE);
  const [mode, setMode] = useState<'unified' | 'split'>(() => (load('diffMode') === 'split' ? 'split' : 'unified'));
  const res = useQuery(`diff:${id}/${slug}/${from}..${to}`, () => api.getDiff(id, slug, from, to));
  const v = res.data;
  const lines = useMemo(() => (v ? buildLines(v.from.body, v.to.body) : []), [v]);
  const blocks = useMemo(() => toBlocks(lines), [lines]);
  if (res.error) return <ErrorView error={res.error} />;
  if (!v) return <Loading />;
  const added = lines.filter((l) => l.kind === 'add').length;
  const removed = lines.filter((l) => l.kind === 'del').length;
  const split = wide && mode === 'split';
  const setM = (m: 'unified' | 'split') => {
    setMode(m);
    save('diffMode', m);
  };
  const pick = (a: number, b: number) => a < b && navigate(paths.diff(id, slug, a, b));

  return (
    <div className="page diff-page">
      <nav className="crumbs" aria-label="breadcrumbs">
        <a href={href('/')}>{t('nav.tasks')}</a>
        <Icon name="chevronRight" size={12} />
        <a href={href(paths.task(v.task.id))} className="crumb-task">
          <span className="mono">{v.task.id}</span> <span className="crumb-title">{v.task.title}</span>
        </a>
        <Icon name="chevronRight" size={12} />
        <a href={href(paths.doc(v.task.id, v.slug))}>{v.title}</a>
      </nav>
      <header className="diff-head">
        <h1>{t('diff.title', { a: from, b: to })}</h1>
        <div className="diff-controls">
          <label className="rev-pick">
            <select value={from} onChange={(e) => pick(Number(e.target.value), to)} aria-label={t('hist.from')}>
              {v.revisions.map((r) => (
                <option key={r.n} value={r.n} disabled={r.n >= to}>
                  r{r.n}
                </option>
              ))}
            </select>
            <Icon name="chevronDown" size={14} />
          </label>
          <Icon name="arrowRight" size={14} />
          <label className="rev-pick">
            <select value={to} onChange={(e) => pick(from, Number(e.target.value))} aria-label={t('hist.to')}>
              {v.revisions.map((r) => (
                <option key={r.n} value={r.n} disabled={r.n <= from}>
                  r{r.n}
                </option>
              ))}
            </select>
            <Icon name="chevronDown" size={14} />
          </label>
          <span className="diff-stat">
            <span className="plus">{t('diff.added', { n: added })}</span> <span className="minus">{t('diff.removed', { n: removed })}</span>
          </span>
          {wide && (
            <div className="seg seg-sm" role="group">
              <button type="button" className={`seg-item${mode === 'unified' ? ' on' : ''}`} onClick={() => setM('unified')} aria-pressed={mode === 'unified'}>
                {t('diff.unified')}
              </button>
              <button type="button" className={`seg-item${mode === 'split' ? ' on' : ''}`} onClick={() => setM('split')} aria-pressed={mode === 'split'}>
                {t('diff.split')}
              </button>
            </div>
          )}
          <a className="btn btn-sm btn-ghost" href={href(paths.doc(v.task.id, v.slug, to === v.latest ? undefined : to))}>
            {t('diff.back')}
          </a>
        </div>
        <div className="diff-revs">
          {[v.from, v.to].map((r) => (
            <div key={r.n} className="diff-rev">
              <span className="mono rev-n">r{r.n}</span> <AuthorBadge author={r.author} /> <Time iso={r.created_at} />
              {r.message && <span className="rev-msg"> — {r.message}</span>}
            </div>
          ))}
        </div>
      </header>
      <div className={`diff-layout${split ? ' is-split' : ''}`}>
        <div className="diff-main">
          {added + removed === 0 ? <p className="muted">{t('diff.same')}</p> : split ? <Split blocks={blocks} /> : <Unified blocks={blocks} />}
        </div>
        <aside className="diff-side">
          <Rounds v={v} />
        </aside>
      </div>
    </div>
  );
}
