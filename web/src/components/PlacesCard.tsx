import { api, useQuery, type Task } from '../api';
import { useI18n } from '../i18n';
import { href, paths } from '../router';
import { Icon } from './Icon';
import { PlacePicker } from './PlacePicker';
import { CopyButton, Time } from './ui';

/**
 * The task's places with their known paths on every machine: a task moves between
 * machines, so all locations are shown (most recently seen first), not one "this machine".
 */
export function PlacesCard({ task }: { task: Task }) {
  const { t } = useI18n();
  const all = useQuery('places', () => api.listPlaces());
  const sorted = [...task.places].sort((a, b) => (a.role === b.role ? 0 : a.role === 'primary' ? -1 : 1));
  return (
    <section className="card">
      <h2 className="card-title">{t('place.title')}</h2>
      {sorted.length === 0 && <p className="muted small">{t('place.empty')}</p>}
      <ul className="places">
        {sorted.map((p) => {
          const locs = [...(p.locations ?? [])].sort((a, b) => b.seen_at.localeCompare(a.seen_at));
          return (
            <li key={p.key} className={`place place-${p.role}`}>
              <Icon name={p.kind === 'git' ? 'repo' : 'folder'} size={16} className="place-icon" />
              <div className="place-main">
                <div className="place-line">
                  <a className="place-label" href={href(paths.list(new URLSearchParams({ place: p.key })))} title={t(`place.kind.${p.kind}`)}>
                    {p.label}
                  </a>
                  <span className={`role role-${p.role}`}>{t(`place.${p.role}`)}</span>
                  <span className="place-key mono">{p.key}</span>
                </div>
                {p.note && <div className="place-note">{p.note}</div>}
                {locs.length ? (
                  <ul className="locs">
                    {locs.map((l) => (
                      <li key={`${l.machine}|${l.path}`} className="loc">
                        <span className="loc-machine" title={l.seen_at}>
                          <Icon name="system" size={12} />
                          {l.machine}
                        </span>
                        <span className="copy-line copy-line-sm">
                          <code title={l.path}>{l.path}</code>
                          <CopyButton text={l.path} />
                        </span>
                        <span className="loc-seen">
                          <Time iso={l.seen_at} />
                        </span>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <div className="place-absent">{t('place.noLocations')}</div>
                )}
              </div>
              <button
                type="button"
                className="btn btn-ghost btn-icon place-x"
                aria-label={t('place.remove', { x: p.label })}
                title={t('place.remove', { x: p.label })}
                onClick={() => api.updateTask(task.id, { places_remove: [p.key] })}
              >
                <Icon name="x" size={14} />
              </button>
            </li>
          );
        })}
      </ul>
      <PlacePicker
        places={(all.data ?? []).filter((p) => !task.places.some((x) => x.key === p.key))}
        value={[]}
        onChange={([key]) => key && api.updateTask(task.id, { places_add: [{ place: key, role: task.places.length ? 'touched' : 'primary' }] })}
        allowNew
        label={t('place.add')}
        icon="plus"
        className="pp-link"
      />
    </section>
  );
}
