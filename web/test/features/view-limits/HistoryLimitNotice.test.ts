import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys, type Session } from '@/entities/session'
import { HistoryLimitNotice } from '@/features/view-limits'
import { i18n, type Locale } from '@/shared/i18n'
import session from '@fixtures/session.json'
import { mountWith, testQueryClient } from '@test/utils'

// The page that lifts the limits: none in the public build, one in a test.
const extension = vi.hoisted(() => ({ limitsPage: null as string | null }))
vi.mock('@/entities/session/extensions', () => ({
  get limitsPage() {
    return extension.limitsPage
  },
}))

afterEach(() => {
  extension.limitsPage = null
  i18n.global.locale.value = 'en'
})

async function notice(s: Session) {
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), s)
  const { wrapper } = mountWith(HistoryLimitNotice, { queryClient })
  await flushPromises()
  return wrapper.find('.history-limit')
}

const limited: Session = {
  ...(session as Session),
  limits: { history_from: '2026-08-29T10:00:00Z', unavailable: [], unread_vehicles: [] },
}

describe('HistoryLimitNotice', () => {
  it('says nothing without a limit', async () => {
    expect((await notice(session as Session)).exists()).toBe(false)
    expect(
      (
        await notice({
          ...(session as Session),
          limits: { history_from: null, unavailable: [], unread_vehicles: [] },
        })
      ).exists(),
    ).toBe(false)
  })

  it.each<[Locale, string]>([
    ['en', 'History shown from Saturday, 29 August 2026. The earlier trips'],
    ['fr', 'Historique affiché depuis le samedi 29 août 2026. Les trajets'],
    ['sv', 'Historik visas från lördag 29 augusti 2026. Äldre resor'],
  ])('tells from when the history is shown, in %s', async (locale, text) => {
    i18n.global.locale.value = locale
    expect((await notice(limited)).text()).toContain(text)
  })

  it('leads to the page that lifts the limit, when the instance has one', async () => {
    expect((await notice(limited)).find('a').exists()).toBe(false)
    extension.limitsPage = '/plans'
    const link = (await notice(limited)).find('a')
    expect(link.attributes('href')).toBe('/plans')
    expect(link.text()).toBe('Show the whole history')
  })
})
