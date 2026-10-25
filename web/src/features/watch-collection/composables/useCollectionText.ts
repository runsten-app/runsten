import { useI18n } from 'vue-i18n'
import type { CollectionState, Connection, FailureKind } from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'

// useCollectionText tells the user how a vehicle is read, in their words: the vendor's
// API names and the collector's kinds of failure are written out, each key in full. The
// vendor is the vehicle's connection's: Volvo's API, or Polestar's Data Portal.
type Provider = Connection['provider']

export function useCollectionText() {
  const { t } = useI18n()
  const f = useFormat()

  // api names what an API of Volvo gives, and Polestar's; an API the front end does not
  // know keeps the vendor's name.
  function api(name: string): string {
    switch (name) {
      case 'connected-vehicle':
        return t('connection.api.connectedVehicle')
      case 'energy':
        return t('connection.api.energy')
      case 'location':
        return t('connection.api.location')
      case 'polestar':
        return t('connection.api.polestar')
      default:
        return name
    }
  }

  function failure(kind: FailureKind, provider: Provider = 'volvo'): string {
    const polestar = provider === 'polestar'
    const labels: Record<FailureKind, string> = {
      quota: t('connection.failure.quota'),
      rate_limited: t('connection.failure.rateLimited'),
      unauthorized: t('connection.failure.unauthorized'),
      not_found: polestar
        ? t('connection.failure.notFoundPolestar')
        : t('connection.failure.notFound'),
      unavailable: polestar
        ? t('connection.failure.unavailablePolestar')
        : t('connection.failure.unavailable'),
      token: polestar ? t('connection.failure.tokenPolestar') : t('connection.failure.token'),
      key_refused: t('connection.failure.keyRefused'),
      market: t('connection.failure.market'),
      credential_refused: t('connection.failure.credentialRefused'),
      unshared: t('connection.failure.unshared'),
      not_granted: t('connection.failure.notGranted'),
      other: t('connection.failure.other'),
    }
    return labels[kind]
  }

  // state is one sentence: the headline of the vehicle on the connection page.
  function state(s: CollectionState, now: number, provider: Provider = 'volvo'): string {
    const polestar = provider === 'polestar'
    switch (s.kind) {
      case 'unread':
        return t('connection.state.unread')
      case 'notConfigured':
        return t('connection.state.notConfigured')
      case 'reauth':
        return t('connection.state.reauth')
      case 'key':
        return s.refused ? t('connection.state.keyRefused') : t('connection.state.keyMissing')
      case 'credential':
        return s.refused
          ? t('connection.state.credentialRefused')
          : t('connection.state.credentialMissing')
      case 'unshared':
        return t('connection.state.unshared')
      case 'notGranted':
        return t('connection.state.notGranted')
      case 'waiting':
        return t('connection.state.waiting')
      case 'stopped':
        return t('connection.state.stopped', { age: f.age(s.since, now) })
      case 'market':
        return t('connection.state.market')
      case 'paused':
        return polestar
          ? t('connection.state.pausedPolestar', { time: f.dateTime(s.until, now) })
          : t('connection.state.paused', { time: f.dateTime(s.until, now) })
      case 'quota':
        // The Data Portal's quota is the credential's, over every domain: one API.
        return polestar
          ? t('connection.state.quotaPolestar', { time: f.dateTime(s.until, now) })
          : t('connection.state.quota', {
              time: f.dateTime(s.until, now),
              apis: s.apis.map(api).join(', '),
            })
      case 'reading':
        return t('connection.state.reading')
    }
  }

  return { api, failure, state }
}
