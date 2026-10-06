import { useI18n } from 'vue-i18n'
import type { CollectionState, FailureKind } from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'

// useCollectionText tells the user how a vehicle is read, in their words: the vendor's
// API names and the collector's kinds of failure are written out, each key in full.
export function useCollectionText() {
  const { t } = useI18n()
  const f = useFormat()

  // api names what an API of Volvo gives; an API the front end does not know keeps
  // the vendor's name.
  function api(name: string): string {
    switch (name) {
      case 'connected-vehicle':
        return t('connection.api.connectedVehicle')
      case 'energy':
        return t('connection.api.energy')
      case 'location':
        return t('connection.api.location')
      default:
        return name
    }
  }

  function failure(kind: FailureKind): string {
    const labels: Record<FailureKind, string> = {
      quota: t('connection.failure.quota'),
      rate_limited: t('connection.failure.rateLimited'),
      unauthorized: t('connection.failure.unauthorized'),
      not_found: t('connection.failure.notFound'),
      unavailable: t('connection.failure.unavailable'),
      token: t('connection.failure.token'),
      key_refused: t('connection.failure.keyRefused'),
      other: t('connection.failure.other'),
    }
    return labels[kind]
  }

  // state is one sentence: the headline of the vehicle on the connection page.
  function state(s: CollectionState, now: number): string {
    switch (s.kind) {
      case 'unread':
        return t('connection.state.unread')
      case 'reauth':
        return t('connection.state.reauth')
      case 'key':
        return s.refused ? t('connection.state.keyRefused') : t('connection.state.keyMissing')
      case 'waiting':
        return t('connection.state.waiting')
      case 'stopped':
        return t('connection.state.stopped', { age: f.age(s.since, now) })
      case 'paused':
        return t('connection.state.paused', { time: f.dateTime(s.until, now) })
      case 'quota':
        return t('connection.state.quota', {
          time: f.dateTime(s.until, now),
          apis: s.apis.map(api).join(', '),
        })
      case 'reading':
        return t('connection.state.reading')
    }
  }

  return { api, failure, state }
}
