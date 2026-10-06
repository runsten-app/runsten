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

const [car, lost, never] = vehicles.items as Vehicle[]
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
      'This instance has no Volvo key of its own: give yours on the connection page.',
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

  it('says nothing before the vehicle is read', async () => {
    fetchVehicle.mockReturnValue(new Promise(() => {}))
    const { wrapper } = mountWith(CollectionNotice, { props: { vehicle: 'v' } })
    await flushPromises()
    expect(wrapper.find('.collection-notice').exists()).toBe(false)
  })
})
