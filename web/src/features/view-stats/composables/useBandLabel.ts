import { useI18n } from 'vue-i18n'
import type { DistanceBand } from '@/entities/stats'
import { useFormat } from '@/shared/lib'

// useBandLabel names a band of trips by distance: "under 5 km", "5 to 20 km", "100 km
// and more".
export function useBandLabel() {
  const { t } = useI18n()
  const f = useFormat()
  return (b: DistanceBand): string => {
    if (b.max_km === null) return t('stats.band.atLeast', { min: f.quantity(b.min_km, 'km') })
    const max = f.quantity(b.max_km, 'km')
    if (b.min_km === 0) return t('stats.band.below', { max })
    return t('stats.band.between', { min: f.number(b.min_km), max })
  }
}
