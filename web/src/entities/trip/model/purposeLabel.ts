import { useI18n } from 'vue-i18n'
import type { PurposeFilter } from './Trip'

// usePurposeLabel names a purpose, or the trips without one (Unmarked): each key written
// out, so that the linter sees it used.
export function usePurposeLabel() {
  const { t } = useI18n()
  return (p: PurposeFilter): string => {
    switch (p) {
      case 'business':
        return t('purpose.business')
      case 'commute':
        return t('purpose.commute')
      case 'personal':
        return t('purpose.personal')
      case 'none':
        return t('purpose.unmarked')
    }
  }
}
