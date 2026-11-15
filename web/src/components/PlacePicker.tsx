import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { Place } from '../api/types';
import { useI18n } from '../i18n';
import { Icon } from './Icon';

/** Special values of a filter picker. */
export const ANY = '';
export const NONE = 'none';

interface Props<P extends Place> {
  places: P[];
  /** Selected keys (one at most unless `multiple`). For filters, [] = any place, ['none'] = no place. */
  value: string[];
  onChange: (keys: string[]) => void;
  multiple?: boolean;
  /** Offer "Any place" and "No place" (filters). */
  filter?: boolean;
  /** Allow a key that is not in the list (a new repository or folder). */
  allowNew?: boolean;
  count?: (p: P) => number;
  /** Trigger text when nothing is selected. */
  label: ReactNode;
  icon?: 'filter' | 'plus' | 'repo';
  /** Always-open list with the search box on top (filters), instead of a button. */
  inline?: boolean;
  className?: string;
}

interface Option {
  key: string;
  label: string;
  sub?: string;
  kind?: Place['kind'];
  n?: number;
  special?: boolean;
}

/**
 * A place chooser that stays usable with many places: a button that opens a searchable
 * list (type to filter by name or key, arrows + Enter to pick, Escape to close).
 */
export function PlacePicker<P extends Place>({ places, value, onChange, multiple, filter, allowNew, count, label, icon, inline, className }: Props<P>) {
  const { t } = useI18n();
  const [openState, setOpen] = useState(false);
  const open = inline || openState;
  const [q, setQ] = useState('');
  const [active, setActive] = useState(0);
  const host = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLUListElement>(null);

  useEffect(() => {
    if (!open) return;
    // Close on a click elsewhere — on `click`, not `mousedown`: the open list is in the page
    // flow in panels and dialogs, and closing it on mousedown would move the button under
    // the pointer before the click lands.
    const onClick = (e: MouseEvent) => !host.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener('click', onClick);
    return () => document.removeEventListener('click', onClick);
  }, [open]);

  const options = useMemo<Option[]>(() => {
    const needle = q.trim().toLowerCase();
    const hit = (p: Place) => !needle || p.label.toLowerCase().includes(needle) || p.key.toLowerCase().includes(needle);
    const rank = (p: Place) => (needle && p.label.toLowerCase().startsWith(needle) ? 0 : 1);
    const ps = places
      .filter(hit)
      .sort((a, b) => rank(a) - rank(b) || (count ? count(b) - count(a) : 0) || a.label.localeCompare(b.label))
      .map<Option>((p) => ({ key: p.key, label: p.label, sub: p.key, kind: p.kind, n: count?.(p) }));
    const out: Option[] = [];
    if (filter && !needle) out.push({ key: ANY, label: t('place.any'), special: true });
    out.push(...ps);
    if (filter && (!needle || t('place.none').toLowerCase().includes(needle))) out.push({ key: NONE, label: t('place.none'), special: true });
    if (allowNew && needle && !places.some((p) => p.key.toLowerCase() === needle)) out.push({ key: q.trim(), label: t('pp.useKey', { x: q.trim() }), special: true });
    return out;
  }, [places, q, filter, allowNew, count, t]);

  useEffect(() => setActive(0), [q, open]);
  useEffect(() => {
    // Keep the active option visible by scrolling the list only, never the page.
    const ul = list.current;
    const el = ul?.querySelector<HTMLElement>(`[data-i="${active}"]`);
    if (!ul || !el) return;
    if (el.offsetTop < ul.scrollTop) ul.scrollTop = el.offsetTop;
    else if (el.offsetTop + el.offsetHeight > ul.scrollTop + ul.clientHeight) ul.scrollTop = el.offsetTop + el.offsetHeight - ul.clientHeight;
  }, [active]);

  const pick = (o: Option) => {
    if (multiple) {
      onChange(value.includes(o.key) ? value.filter((k) => k !== o.key) : [...value, o.key]);
      setQ('');
    } else {
      onChange(o.key === ANY ? [] : [o.key]);
      setOpen(false);
      setQ('');
    }
  };

  const byKey = new Map(places.map((p) => [p.key, p]));
  const single = !multiple && value[0] !== undefined ? value[0] : undefined;
  const singleLabel = single === NONE ? t('place.none') : single ? (byKey.get(single)?.label ?? single) : undefined;

  return (
    <div className={`pp ${className ?? ''}`} ref={host}>
      {multiple && value.length > 0 && (
        <span className="pp-chips">
          {value.map((k) => {
            const p = byKey.get(k);
            return (
              <span key={k} className="tag place-chip" title={k}>
                <Icon name={(p?.kind ?? (k.includes(':/') ? 'path' : 'git')) === 'git' ? 'repo' : 'folder'} size={12} />
                <span className="tag-name">{p?.label ?? k}</span>
                <button type="button" className="tag-x" aria-label={t('place.remove', { x: p?.label ?? k })} onClick={() => onChange(value.filter((x) => x !== k))}>
                  <Icon name="x" size={12} />
                </button>
              </span>
            );
          })}
        </span>
      )}
      {!inline && (
        <button
          type="button"
          className={`pp-trigger${singleLabel ? ' set' : ''}`}
          aria-haspopup="listbox"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
          title={single && single !== NONE ? single : undefined}
        >
          <Icon name={singleLabel && single !== NONE ? (byKey.get(single!)?.kind === 'path' ? 'folder' : 'repo') : (icon ?? 'repo')} size={13} />
          <span className="pp-label">{singleLabel ?? label}</span>
          <Icon name="chevronDown" size={13} />
        </button>
      )}
      {open && (
        <div className={`pp-pop${inline ? ' pp-inline' : ''}`} role={inline ? undefined : 'dialog'}>
          <label className="pp-search">
            <Icon name="search" size={13} />
            <input
              autoFocus={!inline}
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={t('pp.placeholder')}
              aria-label={t('pp.placeholder')}
              role="combobox"
              aria-expanded="true"
              aria-controls="pp-list"
              onKeyDown={(e) => {
                if (e.key === 'ArrowDown') {
                  e.preventDefault();
                  setActive((a) => Math.min(options.length - 1, a + 1));
                } else if (e.key === 'ArrowUp') {
                  e.preventDefault();
                  setActive((a) => Math.max(0, a - 1));
                } else if (e.key === 'Enter') {
                  e.preventDefault();
                  if (options[active]) pick(options[active]);
                } else if (e.key === 'Escape') {
                  setOpen(false);
                }
              }}
            />
          </label>
          <ul className="pp-list" role="listbox" id="pp-list" ref={list} aria-multiselectable={multiple}>
            {options.length === 0 && <li className="pp-empty">{t('pp.empty')}</li>}
            {options.map((o, i) => {
              const on = multiple ? value.includes(o.key) : o.key === ANY ? value.length === 0 : value[0] === o.key;
              return (
                <li
                  key={`${o.key}|${o.label}`}
                  data-i={i}
                  role="option"
                  aria-selected={on}
                  className={`pp-opt${i === active ? ' active' : ''}${on ? ' on' : ''}${o.special ? ' special' : ''}`}
                  onMouseEnter={() => setActive(i)}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => pick(o)}
                >
                  <span className="pp-mark" aria-hidden="true">
                    {on && <Icon name="check" size={12} />}
                  </span>
                  {o.kind && <Icon name={o.kind === 'git' ? 'repo' : 'folder'} size={12} className="pp-kind" />}
                  <span className="pp-text">
                    <span className="pp-name">{o.label}</span>
                    {o.sub && o.sub !== o.label && <span className="pp-key">{o.sub}</span>}
                  </span>
                  {o.n !== undefined && <span className="count">{o.n}</span>}
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}
