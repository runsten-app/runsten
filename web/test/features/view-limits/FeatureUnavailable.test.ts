import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { FeatureUnavailable } from '@/features/view-limits'
import { i18n, type Locale } from '@/shared/i18n'
import { mountWith } from '@test/utils'

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

async function notice(feature: 'costs' | 'stats' | 'mqtt') {
  const { wrapper } = mountWith(FeatureUnavailable, { props: { feature } })
  await flushPromises()
  return wrapper.find('.feature-unavailable')
}

describe('FeatureUnavailable', () => {
  // Read from the messages: an instance may word them otherwise.
  it.each<[Locale, 'costs' | 'stats' | 'mqtt']>([
    ['en', 'stats'],
    ['fr', 'stats'],
    ['sv', 'costs'],
    ['fr', 'mqtt'],
  ])('names the function, in %s', async (locale, feature) => {
    i18n.global.locale.value = locale
    expect((await notice(feature)).text()).toContain(i18n.global.t(`limits.${feature}`))
  })

  it('leads to the page that lifts the limits, when the instance has one', async () => {
    expect((await notice('costs')).find('a').exists()).toBe(false)
    extension.limitsPage = '/plans'
    const link = (await notice('costs')).find('a')
    expect(link.attributes('href')).toBe('/plans')
    expect(link.text()).toBe(i18n.global.t('limits.lift'))
  })
})
