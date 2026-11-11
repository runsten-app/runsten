import { useI18n } from 'vue-i18n'
import type { CollectionState, Connection, FailureKind } from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'

// useCollectionText tells the user how a vehicle is read, in their words: the vendor's
// API names and the collector's kinds of failure are written out, each key in full. The
// vendor is the vehicle's connection's: Volvo's API, Polestar's Data Portal, Tesla's
// Owner API, the Polestar app's API, read with the Polestar ID, or BMW's CarData.
type Provider = Connection['provider']

// byProvider picks the text of the provider: Volvo's for any other; the Data Portal's for
// the Polestar ID, the Owner API's for Tesla's Fleet API, and Volvo's for BMW, unless they
// have their own.
function byProvider<T>(
  provider: Provider,
  texts: { volvo: T; polestar: T; tesla: T; polestarId?: T; teslaFleet?: T; bmw?: T; pleos?: T },
): T {
  if (provider === 'polestar_id') return texts.polestarId ?? texts.polestar
  if (provider === 'tesla_fleet') return texts.teslaFleet ?? texts.tesla
  if (provider === 'bmw') return texts.bmw ?? texts.volvo
  // The three brands of the Pleos Vehicle Data API: one text for the three.
  if (provider.startsWith('pleos_')) return texts.pleos ?? texts.volvo
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
      case 'polestar_id':
        return t('connection.api.polestarId')
      case 'tesla_fleet':
        return t('connection.api.teslaFleet')
      case 'bmw':
        return t('connection.api.bmw')
      case 'pleos':
        return t('connection.api.pleos')
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
        bmw: t('connection.failure.notFoundBmw'),
        pleos: t('connection.failure.notFoundPleos'),
      }),
      unavailable: byProvider(provider, {
        volvo: t('connection.failure.unavailable'),
        polestar: t('connection.failure.unavailablePolestar'),
        tesla: t('connection.failure.unavailableTesla'),
        bmw: t('connection.failure.unavailableBmw'),
        pleos: t('connection.failure.unavailablePleos'),
      }),
      token: byProvider(provider, {
        volvo: t('connection.failure.token'),
        polestar: t('connection.failure.tokenPolestar'),
        tesla: t('connection.failure.tokenTesla'),
        bmw: t('connection.failure.tokenBmw'),
        pleos: t('connection.failure.tokenPleos'),
      }),
      key_refused: t('connection.failure.keyRefused'),
      market: t('connection.failure.market'),
      credential_refused: byProvider(provider, {
        volvo: t('connection.failure.credentialRefused'),
        polestar: t('connection.failure.credentialRefused'),
        tesla: t('connection.failure.credentialRefusedTesla'),
        polestarId: t('connection.failure.credentialRefusedPolestarId'),
        teslaFleet: t('connection.failure.credentialRefusedTeslaFleet'),
        bmw: t('connection.failure.credentialRefusedBmw'),
        pleos: t('connection.failure.credentialRefusedPleos'),
      }),
      unshared: t('connection.failure.unshared'),
      not_granted: t('connection.failure.notGranted'),
      api_closed: t('connection.failure.apiClosed'),
      stream_held: t('connection.failure.streamHeld'),
      key_shared: t('connection.failure.keyShared'),
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
              polestarId: t('connection.state.credentialRefusedPolestarId'),
              teslaFleet: t('connection.state.credentialRefusedTeslaFleet'),
              bmw: t('connection.state.credentialRefusedBmw'),
              pleos: t('connection.state.credentialRefusedPleos'),
            })
          : byProvider(provider, {
              volvo: t('connection.state.credentialMissing'),
              polestar: t('connection.state.credentialMissing'),
              tesla: t('connection.state.credentialMissingTesla'),
              polestarId: t('connection.state.credentialMissingPolestarId'),
              teslaFleet: t('connection.state.credentialMissingTeslaFleet'),
              bmw: t('connection.state.credentialMissingBmw'),
              pleos: t('connection.state.credentialMissingPleos'),
            })
      case 'unshared':
        return byProvider(provider, {
          volvo: t('connection.state.unshared'),
          polestar: t('connection.state.unshared'),
          tesla: t('connection.state.unsharedTesla'),
          polestarId: t('connection.state.unsharedPolestarId'),
          bmw: t('connection.state.unsharedBmw'),
          pleos: t('connection.state.unsharedPleos'),
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
          bmw: t('connection.state.pausedBmw', { time: f.dateTime(s.until, now) }),
          pleos: t('connection.state.pausedPleos', { time: f.dateTime(s.until, now) }),
        })
      case 'quota':
        // The Data Portal's quota is the credential's, over every domain: one API; the
        // Owner API's, the refresh token's; the Polestar app's, the Polestar ID's.
        if (provider !== 'volvo')
          return byProvider(provider, {
            volvo: t('connection.state.quota', { time: f.dateTime(s.until, now) }),
            polestar: t('connection.state.quotaPolestar', { time: f.dateTime(s.until, now) }),
            tesla: t('connection.state.quotaTesla', { time: f.dateTime(s.until, now) }),
            bmw: t('connection.state.quotaBmw', { time: f.dateTime(s.until, now) }),
            pleos: t('connection.state.quotaPleos', { time: f.dateTime(s.until, now) }),
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
