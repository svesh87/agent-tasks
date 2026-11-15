import { useMemo, useRef, useState } from 'react';
import { api, useQuery } from '../api';
import { useI18n } from '../i18n';
import { Icon } from './Icon';
import { tagParts } from './ui';

/**
 * Tags as chips plus an input with suggestions from the tags already in use (tasks and
 * todos), most used first. Typing matches the whole tag or the part after a namespace
 * (`back` finds `topic:backup`). Enter, Tab or comma accepts; Backspace on an empty input
 * removes the last chip; a new tag is fine.
 */
export function TagInput({
  value,
  onAdd,
  onRemove,
  placeholder,
  autoFocus,
}: {
  value: string[];
  onAdd: (tag: string) => void;
  onRemove: (tag: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const { t } = useI18n();
  const [q, setQ] = useState('');
  const [focused, setFocused] = useState(false);
  const [active, setActive] = useState(0);
  const [navigated, setNavigated] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const tags = useQuery('tags', () => api.listTags());

  const suggestions = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const all = (tags.data ?? []).filter((x) => !value.includes(x.tag));
    const rank = (tag: string) => {
      const s = tag.toLowerCase();
      if (!needle) return 0;
      if (s.startsWith(needle)) return 0;
      if (tagParts(s).name.startsWith(needle)) return 1;
      if (s.includes(needle)) return 2;
      return -1;
    };
    return all
      .map((x) => ({ ...x, r: rank(x.tag) }))
      .filter((x) => x.r >= 0)
      .sort((a, b) => a.r - b.r || b.count - a.count)
      .slice(0, 8);
  }, [tags.data, q, value]);

  const accept = (tag: string) => {
    const v = tag.trim().replace(/,+$/, '');
    if (v && !value.includes(v)) onAdd(v);
    setQ('');
    setActive(0);
  };
  const open = focused && suggestions.length > 0;

  return (
    <div className={`tag-input${focused ? ' focused' : ''}`} onClick={() => input.current?.focus()}>
      {value.map((tag) => {
        const { ns, short } = tagParts(tag);
        return (
          <span key={tag} className={`tag tag-${ns || 'plain'}`} title={tag}>
            {ns && <span className="tag-ns">{ns}:</span>}
            <span className="tag-name">{short}</span>
            <button type="button" className="tag-x" aria-label={t('task.removeTag', { x: tag })} onClick={() => onRemove(tag)}>
              <Icon name="x" size={12} />
            </button>
          </span>
        );
      })}
      <input
        ref={input}
        value={q}
        autoFocus={autoFocus}
        placeholder={value.length ? '' : (placeholder ?? t('task.tagPh'))}
        aria-label={t('task.addTag')}
        role="combobox"
        aria-expanded={open}
        aria-autocomplete="list"
        onFocus={() => setFocused(true)}
        onBlur={() => {
          setFocused(false);
          if (q.trim()) accept(q);
        }}
        onChange={(e) => {
          const v = e.target.value;
          if (v.endsWith(',')) accept(v);
          else {
            setQ(v);
            setActive(0);
            setNavigated(false);
          }
        }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && open) {
            e.preventDefault();
            if (navigated) setActive((a) => Math.min(suggestions.length - 1, a + 1));
            setNavigated(true);
          } else if (e.key === 'ArrowUp' && open) {
            e.preventDefault();
            setActive((a) => Math.max(0, a - 1));
            setNavigated(true);
          } else if (e.key === 'Enter' || (e.key === 'Tab' && (q.trim() || open))) {
            if (!q.trim() && !navigated) return;
            e.preventDefault();
            // Tab completes to the highlighted suggestion; Enter takes it only after the arrows
            // were used, otherwise the text as typed (so a new tag is never replaced).
            const s = open ? suggestions[active] : undefined;
            accept(s && (e.key === 'Tab' || navigated) ? s.tag : q);
            setNavigated(false);
          } else if (e.key === 'Backspace' && !q && value.length) {
            onRemove(value[value.length - 1]);
          } else if (e.key === 'Escape') {
            setQ('');
          }
        }}
      />
      {open && (
        <ul className="tag-sugg" role="listbox">
          {suggestions.map((s, i) => (
            <li
              key={s.tag}
              role="option"
              aria-selected={i === active}
              className={i === active && navigated ? 'active' : ''}
              onMouseEnter={() => setActive(i)}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => accept(s.tag)}
            >
              <span className="tag-sugg-name">{s.tag}</span>
              <span className="count">{s.count}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
