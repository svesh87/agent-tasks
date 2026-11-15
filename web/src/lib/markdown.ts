// Markdown helpers shared by the fake API and the UI: task-list lines and text anchors.
import type { Anchor } from '../api/types';

export interface TaskLine {
  /** 1-based line number in the body. */
  line: number;
  text: string;
  checked: boolean;
}

const TASK_RE = /^\s*(?:[-*+]|\d+[.)])\s+\[( |x|X)\]\s+(.*)$/;
const FENCE_RE = /^\s*(```|~~~)/;

/** Every task-list line (`- [ ] text`) outside fenced code blocks. */
export function taskLines(body: string): TaskLine[] {
  const out: TaskLine[] = [];
  let fence: string | null = null;
  body.split('\n').forEach((raw, i) => {
    const f = raw.match(FENCE_RE);
    if (f) {
      if (fence === null) fence = f[1];
      else if (f[1] === fence) fence = null;
      return;
    }
    if (fence !== null) return;
    const m = raw.match(TASK_RE);
    if (m) out.push({ line: i + 1, text: m[2].trim(), checked: m[1] !== ' ' });
  });
  return out;
}

function bigrams(s: string): Map<string, number> {
  const m = new Map<string, number>();
  const t = s.toLowerCase().replace(/\s+/g, ' ');
  for (let i = 0; i < t.length - 1; i++) {
    const g = t.slice(i, i + 2);
    m.set(g, (m.get(g) ?? 0) + 1);
  }
  return m;
}

/** Sørensen–Dice similarity over character bigrams, 0..1. */
export function similarity(a: string, b: string): number {
  if (a === b) return 1;
  const A = bigrams(a);
  const B = bigrams(b);
  let inter = 0;
  let total = 0;
  A.forEach((n, g) => {
    inter += Math.min(n, B.get(g) ?? 0);
    total += n;
  });
  B.forEach((n) => (total += n));
  return total === 0 ? 0 : (2 * inter) / total;
}

/**
 * Matches the task lines of a new revision to the items of the previous one: exact text
 * first, then the most similar unmatched item within a few positions. Returns, for every
 * new line, the index of the previous item or -1 (a new item).
 */
export function matchItems(prev: string[], next: string[]): number[] {
  const used = new Set<number>();
  const res = next.map(() => -1);
  next.forEach((t, i) => {
    const j = prev.findIndex((p, k) => !used.has(k) && p === t);
    if (j >= 0) {
      res[i] = j;
      used.add(j);
    }
  });
  next.forEach((t, i) => {
    if (res[i] >= 0) return;
    let best = -1;
    let bestScore = 0.55;
    prev.forEach((p, k) => {
      if (used.has(k) || Math.abs(k - i) > 3) return;
      const s = similarity(p, t);
      if (s > bestScore) {
        best = k;
        bestScore = s;
      }
    });
    if (best >= 0) {
      res[i] = best;
      used.add(best);
    }
  });
  return res;
}

/** UTF-16 index (JS strings) → Unicode code point offset (the API's unit). */
export function utf16ToCp(s: string, i: number): number {
  return Array.from(s.slice(0, i)).length;
}

/** Unicode code point offset → UTF-16 index. */
export function cpToUtf16(s: string, cp: number): number {
  let i = 0;
  for (let n = 0; n < cp && i < s.length; n++) i += s.codePointAt(i)! > 0xffff ? 2 : 1;
  return i;
}

const CTX = 32;

export function makeAnchor(body: string, start: number, end: number): Anchor {
  return {
    quote: body.slice(start, end),
    prefix: body.slice(Math.max(0, start - CTX), start),
    suffix: body.slice(end, end + CTX),
    start,
    end,
  };
}

/** Anchor for the first occurrence of `quote` (fixtures use it). */
export function anchorOf(body: string, quote: string, occurrence = 0): Anchor {
  let at = -1;
  for (let i = 0; i <= occurrence; i++) at = body.indexOf(quote, at + 1);
  if (at < 0) throw new Error(`fixture quote not found: ${quote}`);
  return makeAnchor(body, at, at + quote.length);
}

/**
 * Finds an anchor in another revision: exact quote, preferring the occurrence whose
 * prefix/suffix agree and which lies closest to the old position. Null means outdated.
 * (The server adds a fuzzy pass; see docs/contract-proposals.md.)
 */
export function relocate(body: string, a: Anchor): Anchor | null {
  if (!a.quote) return null;
  let best: { at: number; score: number } | null = null;
  for (let at = body.indexOf(a.quote); at >= 0; at = body.indexOf(a.quote, at + 1)) {
    let score = -Math.abs(at - a.start) / 1000;
    if (a.prefix && body.slice(Math.max(0, at - a.prefix.length), at) === a.prefix) score += 2;
    if (a.suffix && body.slice(at + a.quote.length, at + a.quote.length + a.suffix.length) === a.suffix) score += 2;
    if (!best || score > best.score) best = { at, score };
  }
  return best ? makeAnchor(body, best.at, best.at + a.quote.length) : null;
}
