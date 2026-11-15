import type { PlaceSummary } from '../api/types';
import { useI18n } from '../i18n';
import { PlacePicker } from './PlacePicker';

/** Place filter for the side panels: any place, one place, or none — with search. */
export function PlaceFacet({
  places,
  value,
  onChange,
  count,
}: {
  places: PlaceSummary[];
  value?: string;
  onChange: (v: string | undefined) => void;
  count: (p: PlaceSummary) => number;
}) {
  const { t } = useI18n();
  return (
    <div className="filter-group">
      <div className="filter-head">{t('place.title')}</div>
      <PlacePicker
        places={places}
        value={value ? [value] : []}
        onChange={(keys) => onChange(keys[0] || undefined)}
        filter
        count={count}
        label={t('place.any')}
        inline
        className="pp-block"
      />
    </div>
  );
}
