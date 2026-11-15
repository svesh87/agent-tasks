// Mapping between the rendered document (DOM) and offsets in its markdown source.
//
// The markdown renderer wraps every text node in <span class="src" data-s data-e> carrying
// the source range of that text (or of its nearest positioned ancestor, e.g. a code
// block). Inside a span, characters are aligned to the source: first as a verbatim
// substring, otherwise as a subsequence (skipping markup such as list indentation).

const cache = new WeakMap<Element, { body: string; map: number[] }>();

function spanMap(span: Element, body: string): number[] {
  const hit = cache.get(span);
  if (hit && hit.body === body) return hit.map;
  const text = span.textContent ?? '';
  const s = Number(span.getAttribute('data-s'));
  const e = Number(span.getAttribute('data-e'));
  const slice = body.slice(s, e);
  const map: number[] = new Array(text.length);
  const at = slice.indexOf(text);
  if (at >= 0) {
    for (let i = 0; i < text.length; i++) map[i] = s + at + i;
  } else {
    let j = 0;
    for (let i = 0; i < text.length; i++) {
      const k = slice.indexOf(text[i], j);
      if (k >= 0) {
        map[i] = s + k;
        j = k + 1;
      } else map[i] = s + j;
    }
  }
  cache.set(span, { body, map });
  return map;
}

function spans(container: Element): Element[] {
  return Array.from(container.querySelectorAll('span.src[data-s]'));
}

function srcSpanOf(node: Node): Element | null {
  const el = node.nodeType === Node.TEXT_NODE ? node.parentElement : (node as Element);
  return el?.closest('span.src[data-s]') ?? null;
}

/** Source offset of a DOM boundary point. `bias` picks the side when the point is between spans. */
function offsetAt(container: Element, node: Node, offset: number, body: string, bias: 'start' | 'end'): number | null {
  const own = srcSpanOf(node);
  if (own && container.contains(own)) {
    const map = spanMap(own, body);
    if (!map.length) return Number(own.getAttribute('data-s'));
    let i = offset;
    if (node.nodeType !== Node.TEXT_NODE) i = offset === 0 ? 0 : map.length;
    if (i >= map.length) return map[map.length - 1] + 1;
    return map[i];
  }
  const point = document.createRange();
  point.setStart(node, offset);
  const all = spans(container);
  const nextIdx = all.findIndex((sp) => sp.firstChild && point.comparePoint(sp.firstChild, 0) === 1);
  if (bias === 'start') {
    const sp = nextIdx >= 0 ? all[nextIdx] : null;
    return sp ? (spanMap(sp, body)[0] ?? null) : null;
  }
  const sp = nextIdx > 0 ? all[nextIdx - 1] : nextIdx < 0 ? all[all.length - 1] : null;
  if (!sp) return null;
  const map = spanMap(sp, body);
  return map.length ? map[map.length - 1] + 1 : null;
}

/** Markdown offsets of a DOM range inside `container`, or null when it cannot be mapped. */
export function rangeToOffsets(container: Element, range: Range, body: string): { start: number; end: number } | null {
  if (!container.contains(range.commonAncestorContainer)) return null;
  const start = offsetAt(container, range.startContainer, range.startOffset, body, 'start');
  const end = offsetAt(container, range.endContainer, range.endOffset, body, 'end');
  if (start === null || end === null || end <= start) return null;
  // Trim whitespace the user dragged over at the edges.
  let s = start;
  let e = end;
  while (s < e && /\s/.test(body[s])) s++;
  while (e > s && /\s/.test(body[e - 1])) e--;
  return e > s ? { start: s, end: e } : null;
}

/** DOM ranges covering the markdown range [start, end). */
export function offsetsToRanges(container: Element, start: number, end: number, body: string): Range[] {
  const out: Range[] = [];
  for (const sp of spans(container)) {
    const s = Number(sp.getAttribute('data-s'));
    const e = Number(sp.getAttribute('data-e'));
    if (e <= start || s >= end) continue;
    const text = sp.firstChild;
    if (!text || text.nodeType !== Node.TEXT_NODE) continue;
    const map = spanMap(sp, body);
    let i0 = -1;
    let i1 = -1;
    for (let i = 0; i < map.length; i++) {
      if (map[i] >= start && map[i] < end) {
        if (i0 < 0) i0 = i;
        i1 = i;
      }
    }
    if (i0 < 0) continue;
    const r = document.createRange();
    r.setStart(text, i0);
    r.setEnd(text, i1 + 1);
    out.push(r);
  }
  return out;
}

/** DOM point under the pointer, across browsers. */
export function pointFromXY(x: number, y: number): { node: Node; offset: number } | null {
  const d = document as Document & {
    caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
    caretRangeFromPoint?: (x: number, y: number) => Range | null;
  };
  if (d.caretPositionFromPoint) {
    const p = d.caretPositionFromPoint(x, y);
    return p ? { node: p.offsetNode, offset: p.offset } : null;
  }
  if (d.caretRangeFromPoint) {
    const r = d.caretRangeFromPoint(x, y);
    return r ? { node: r.startContainer, offset: r.startOffset } : null;
  }
  return null;
}
