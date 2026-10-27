import { useI18n } from 'vue-i18n'
import type { CollectionState, Connection, FailureKind } from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'

// useCollectionText tells the user how a vehicle is read, in their words: the vendor's
// API names and the collector's kinds of failure are written out, each key in full. The
// vendor is the vehicle's connection's: Volvo's API, Polestar's Data Portal, or Tesla's
// Owner API.
type Provider = Connection['provider']

// byProvider picks the text of the provider: Volvo's for any other.
function byProvider<T>(provider: Provider, texts: { volvo: T; polestar: T; tesla: T }): T {
  return provider === 'polestar' || provider === 'tesla' ? texts[provider] : texts.volvo
}

export { byProvider }

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
      case 'tesla':
        return t('connection.api.tesla')
      default:
        return name
    }
  }

  function failure(kind: FailureKind, provider: Provider = 'volvo'): string {
    const labels: Record<FailureKind, string> = {
      quota: t('connection.failure.quota'),
      rate_limited: t('connection.failure.rateLimited'),
      unauthorized: t('connection.failure.unauthorized'),
      not_found: byProvider(provider, {
        volvo: t('connection.failure.notFound'),
        polestar: t('connection.failure.notFoundPolestar'),
        tesla: t('connection.failure.notFoundTesla'),
      }),
      unavailable: byProvider(provider, {
        volvo: t('connection.failure.unavailable'),
        polestar: t('connection.failure.unavailablePolestar'),
        tesla: t('connection.failure.unavailableTesla'),
      }),
      token: byProvider(provider, {
        volvo: t('connection.failure.token'),
        polestar: t('connection.failure.tokenPolestar'),
        tesla: t('connection.failure.tokenTesla'),
      }),
      key_refused: t('connection.failure.keyRefused'),
      market: t('connection.failure.market'),
      credential_refused: byProvider(provider, {
        volvo: t('connection.failure.credentialRefused'),
        polestar: t('connection.failure.credentialRefused'),
        tesla: t('connection.failure.credentialRefusedTesla'),
      }),
      unshared: t('connection.failure.unshared'),
      not_granted: t('connection.failure.notGranted'),
      api_closed: t('connection.failure.apiClosed'),
      other: t('connection.failure.other'),
    }
    return labels[kind]
  }

  // state is one sentence: the headline of the vehicle on the connection page.
  function state(s: CollectionState, now: number, provider: Provider = 'volvo'): string {
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
          ? byProvider(provider, {
              volvo: t('connection.state.credentialRefused'),
              polestar: t('connection.state.credentialRefused'),
              tesla: t('connection.state.credentialRefusedTesla'),
            })
          : byProvider(provider, {
              volvo: t('connection.state.credentialMissing'),
              polestar: t('connection.state.credentialMissing'),
              tesla: t('connection.state.credentialMissingTesla'),
            })
      case 'unshared':
        return byProvider(provider, {
          volvo: t('connection.state.unshared'),
          polestar: t('connection.state.unshared'),
          tesla: t('connection.state.unsharedTesla'),
        })
      case 'notGranted':
        return t('connection.state.notGranted')
      case 'waiting':
        return t('connection.state.waiting')
      case 'stopped':
        return t('connection.state.stopped', { age: f.age(s.since, now) })
      case 'market':
        return t('connection.state.market')
      case 'paused':
        return byProvider(provider, {
          volvo: t('connection.state.paused', { time: f.dateTime(s.until, now) }),
          polestar: t('connection.state.pausedPolestar', { time: f.dateTime(s.until, now) }),
          tesla: t('connection.state.pausedTesla', { time: f.dateTime(s.until, now) }),
        })
      case 'quota':
        // The Data Portal's quota is the credential's, over every domain: one API; the
        // Owner API's, the refresh token's.
        if (provider === 'polestar' || provider === 'tesla')
          return byProvider(provider, {
            volvo: t('connection.state.quota', { time: f.dateTime(s.until, now) }),
            polestar: t('connection.state.quotaPolestar', { time: f.dateTime(s.until, now) }),
            tesla: t('connection.state.quotaTesla', { time: f.dateTime(s.until, now) }),
          })
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
