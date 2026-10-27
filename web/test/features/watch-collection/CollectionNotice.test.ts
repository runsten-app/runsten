import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Vehicle } from '@/entities/vehicle'
import { CollectionNotice } from '@/features/watch-collection'
import { i18n, type Locale } from '@/shared/i18n'
import vehicles from '@fixtures/vehicles.json'
import { mountWith } from '@test/utils'

const { fetchVehicle } = vi.hoisted(() => ({ fetchVehicle: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  fetchVehicle,
}))

// The page that lifts the limits: none in the public build, one in a test.
const extension = vi.hoisted(() => ({ limitsPage: null as string | null }))
vi.mock('@/entities/session/extensions', () => ({
  get limitsPage() {
    return extension.limitsPage
  },
}))

const [car, lost, never, unserved, polestar] = vehicles.items as Vehicle[]
const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

beforeEach(() => {
  fetchVehicle.mockReset()
  vi.useFakeTimers({ now: Date.parse('2026-09-28T07:05:00Z'), toFake: ['Date'] })
})
afterEach(() => {
  extension.limitsPage = null
  vi.useRealTimers()
  i18n.global.locale.value = 'en'
})

async function notice(v: Vehicle | undefined) {
  fetchVehicle.mockResolvedValue(v)
  const { wrapper } = mountWith(CollectionNotice, { props: { vehicle: v?.id } })
  await flushPromises()
  return wrapper.find('.collection-notice')
}

// shared is the Polestar of the fixtures, shared again, its collection patched.
function shared(patch: Partial<NonNullable<Vehicle['collection']>>): Vehicle | undefined {
  const c = polestar?.collection
  return polestar && c
    ? { ...polestar, unshared_at: null, collection: { ...c, ...patch } }
    : undefined
}

function withCollection(patch: Partial<NonNullable<Vehicle['collection']>>): Vehicle | undefined {
  const c = car?.collection
  return car && c ? { ...car, collection: { ...c, quota: [], ...patch } } : undefined
}

describe('CollectionNotice', () => {
  it.each<[Locale, string, string]>([
    ['en', 'Re-authentication required', 'Since 06:30 · reason: refresh token expired'],
    ['fr', 'Nouvelle authentification requise', 'Depuis 06:30 · raison : refresh token expired'],
    ['sv', 'Ny inloggning krävs', 'Sedan 06:30 · orsak: refresh token expired'],
  ])('asks to connect the Volvo ID again, in %s', async (locale, title, since) => {
    i18n.global.locale.value = locale
    const alert = await notice(lost)
    expect(alert.find('.v-alert-title').text()).toBe(title)
    expect(plain(alert.find('.reauth-since').text())).toBe(since)
    expect(alert.find('a').attributes('href')).toBe('auth/volvo/start')
  })

  it('tells an exhausted quota, and leads to the connection', async () => {
    const alert = await notice(car)
    expect(plain(alert.find('.v-alert-title').text())).toBe(
      'Volvo quota reached until 09:00: battery and charging, position',
    )
    expect(alert.text()).toContain('Reading resumes by itself.')
    expect(alert.find('a').attributes('href')).toBe(`/connection?vehicle=${car?.id}`)
    expect(alert.classes()).toContain('text-warning')
  })

  it("tells an exhausted quota of the account's own key", async () => {
    const own = car && { ...car, connection: { ...car.connection, api_key: 'own' as const } }
    const alert = await notice(own)
    expect(alert.text()).toContain('The daily quota of calls of your Volvo key is used up.')
  })

  it.each([
    [
      'refused',
      'Not read: Volvo refused the application key',
      'Give a new one on the connection page: the Volvo ID stays connected.',
    ],
    [
      'missing',
      'Not read: no Volvo key given',
      'Runsten reads your vehicles with your Volvo key: give it on the connection page.',
    ],
  ] as const)('tells a key %s, and leads to the connection', async (key, title, body) => {
    const v = car && { ...car, connection: { ...car.connection, api_key: key } }
    const alert = await notice(v)
    expect(plain(alert.find('.v-alert-title').text())).toBe(title)
    expect(alert.text()).toContain(body)
    expect(alert.find('a').attributes('href')).toBe(`/connection?vehicle=${car?.id}`)
  })

  it('tells a pause', async () => {
    const alert = await notice(withCollection({ paused_until: '2026-09-28T07:30:00Z' }))
    expect(plain(alert.find('.v-alert-title').text())).toBe('Paused by Volvo until 07:30')
    expect(alert.text()).toContain('Volvo limited the number of calls.')
  })

  it.each<[Locale, string]>([
    ['en', "Not read: Volvo's API does not serve this vehicle's market"],
    ['fr', "Non lu : l'API de Volvo ne dessert pas le marché de ce véhicule"],
    ['sv', 'Läses inte: Volvos API betjänar inte det här fordonets marknad'],
  ])("tells a market Volvo's API does not serve, in %s", async (locale, title) => {
    i18n.global.locale.value = locale
    const alert = await notice(unserved)
    expect(plain(alert.find('.v-alert-title').text())).toBe(plain(title))
    expect(alert.text()).toContain(i18n.global.t('connection.notice.market'))
    expect(alert.find('a').attributes('href')).toBe(`/connection?vehicle=${unserved?.id}`)
  })

  it('tells a stopped collector, as an error', async () => {
    const alert = await notice(withCollection({ passed_at: '2026-09-28T05:00:00Z' }))
    // Read from the messages: an instance may word them otherwise.
    expect(plain(alert.find('.v-alert-title').text())).toBe(
      i18n.global.t('connection.state.stopped', { age: '2 hr ago' }),
    )
    expect(alert.text()).toContain(i18n.global.t('connection.notice.stopped'))
    expect(alert.classes()).toContain('text-error')
  })

  it.each([
    ['while the vehicle is read', withCollection({})],
    ["before the collector's first pass", never],
  ])('says nothing %s', async (_, v) => {
    expect((await notice(v)).exists()).toBe(false)
  })

  // Read from the messages: an instance may word them otherwise.
  it.each<Locale>(['en', 'fr', 'sv'])(
    'tells a vehicle the offer leaves unread, in %s',
    async (locale) => {
      i18n.global.locale.value = locale
      extension.limitsPage = '/plans'
      fetchVehicle.mockResolvedValue(lost)
      const { wrapper } = mountWith(CollectionNotice, {
        props: { vehicle: lost?.id, unread: true },
      })
      await flushPromises()
      const alert = wrapper.find('.collection-notice')
      expect(plain(alert.find('.v-alert-title').text())).toBe(
        plain(i18n.global.t('connection.state.unread')),
      )
      expect(alert.text()).toContain(i18n.global.t('connection.notice.unread'))
      expect(alert.find('a').attributes('href')).toBe('/plans')
      expect(alert.classes()).toContain('text-info')
    },
  )

  it('tells a vehicle left unread without a link where no page lifts the limits', async () => {
    fetchVehicle.mockResolvedValue(car)
    const { wrapper } = mountWith(CollectionNotice, { props: { vehicle: car?.id, unread: true } })
    await flushPromises()
    const alert = wrapper.find('.collection-notice')
    expect(plain(alert.find('.v-alert-title').text())).toBe(
      plain(i18n.global.t('connection.state.unread')),
    )
    expect(alert.find('a').exists()).toBe(false)
  })

  it.each<[string, Vehicle | undefined, string, string]>([
    [
      'its credential refused',
      polestar && {
        ...polestar,
        unshared_at: null,
        connection: { ...polestar.connection, credential: 'refused' },
      },
      'Not read: Polestar refused the credential',
      'it expired after its 90 days',
    ],
    [
      'its credential removed',
      polestar && {
        ...polestar,
        unshared_at: null,
        connection: { ...polestar.connection, credential: 'missing' },
      },
      'Not read: no Polestar credential given',
      'which was removed',
    ],
    [
      'no longer shared',
      polestar,
      'Not read: the vehicle is no longer shared with the credential',
      'Let it read the car again on the portal',
    ],
    [
      'a domain not granted',
      shared({
        last_failure: {
          at: '2026-09-28T07:00:00Z',
          endpoint: 'location',
          status: 403,
          kind: 'not_granted',
        },
      }),
      'Read in part: the credential was not granted every domain',
      'one of the domains Runsten reads (location)',
    ],
  ])('tells a Polestar %s, and leads to the connection', async (_, v, title, body) => {
    const alert = await notice(v)
    expect(plain(alert.find('.v-alert-title').text())).toBe(title)
    expect(alert.text()).toContain(body)
    expect(alert.find('a').attributes('href')).toBe(`/connection?vehicle=${polestar?.id}`)
  })

  it('tells an exhausted quota of the Polestar credential', async () => {
    const v = shared({ quota: [{ api: 'polestar', until: '2026-09-28T09:00:00Z' }] })
    const alert = await notice(v)
    expect(plain(alert.find('.v-alert-title').text())).toBe('Polestar quota reached until 09:00')
    expect(alert.text()).toContain(
      'The daily quota of calls of your Polestar credential is used up.',
    )
  })

  it("tells a pause of the Data Portal as Polestar's", async () => {
    const v = shared({ paused_until: '2026-09-28T07:30:00Z' })
    const alert = await notice(v)
    expect(plain(alert.find('.v-alert-title').text())).toBe('Paused by Polestar until 07:30')
    expect(alert.text()).toContain('Polestar limited the number of calls.')
  })

  // tesla is the Polestar of the fixtures as a Tesla, its collection patched.
  function tesla(
    patch: Partial<Vehicle> = {},
    collection = {},
    credential: Vehicle['connection']['credential'] = 'set',
  ): Vehicle | undefined {
    const v = shared(collection)
    return v && { ...v, connection: { ...v.connection, provider: 'tesla', credential }, ...patch }
  }

  it.each<[string, Vehicle | undefined, string, string]>([
    [
      'its token refused',
      tesla({}, {}, 'refused'),
      'Not read: Tesla refused the refresh token',
      'signed out of the Tesla account',
    ],
    [
      'its token removed',
      tesla({}, {}, 'missing'),
      'Not read: no Tesla refresh token given',
      'which was removed',
    ],
    [
      'no longer in the account',
      tesla({ unshared_at: '2026-09-28T07:00:00Z' }),
      'Not read: the vehicle is no longer in the Tesla account',
      'Your Tesla account no longer lists this vehicle',
    ],
    [
      'its calls of the day used up',
      tesla({}, { quota: [{ api: 'tesla', until: '2026-09-28T09:00:00Z' }] }),
      'Daily calls to Tesla reached until 09:00',
      'at most 3,000 calls a day to Tesla',
    ],
    [
      'a pause',
      tesla({}, { paused_until: '2026-09-28T07:30:00Z' }),
      'Paused by Tesla until 07:30',
      'Tesla limited the number of calls.',
    ],
  ])('tells a Tesla %s', async (_, v, title, body) => {
    const alert = await notice(v)
    expect(plain(alert.find('.v-alert-title').text())).toBe(title)
    expect(alert.text()).toContain(body)
  })

  it('says nothing before the vehicle is read', async () => {
    fetchVehicle.mockReturnValue(new Promise(() => {}))
    const { wrapper } = mountWith(CollectionNotice, { props: { vehicle: 'v' } })
    await flushPromises()
    expect(wrapper.find('.collection-notice').exists()).toBe(false)
  })
})
