import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SoftwareUpdate, UpdateStatus } from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'

// useUpdateText words the update waiting for the software (a Tesla's): its status, the
// version it brings, its progress while it downloads or installs, and when it is set to
// install.
export function useUpdateText() {
  const { t } = useI18n()
  const f = useFormat()
  const labels = computed<Record<UpdateStatus, string>>(() => ({
    available: t('state.update.available'),
    scheduled: t('state.update.scheduled'),
    downloading: t('state.update.downloading'),
    waiting_wifi: t('state.update.waitingWifi'),
    installing: t('state.update.installing'),
    unknown: t('state.update.unknown'),
  }))
  return (u: SoftwareUpdate, now: number): string => {
    const { status, version, download_pct, install_pct, scheduled_at } = u
    const progress =
      status === 'downloading' ? download_pct : status === 'installing' ? install_pct : null
    const label =
      progress !== null
        ? t('state.update.progress', {
            status: labels.value[status],
            pct: f.quantity(progress, 'percent'),
          })
        : status === 'scheduled' && scheduled_at
          ? t('state.update.scheduledAt', { when: f.dateTime(scheduled_at, now) })
          : labels.value[status]
    return version ? t('state.update.version', { version, status: label }) : label
  }
}
