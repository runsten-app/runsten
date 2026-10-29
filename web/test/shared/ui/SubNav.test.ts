import { flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { SubNav } from '@/shared/ui'
import { mountWith, testRouter } from '@test/utils'

const items = [
  { to: '/settings', label: 'General' },
  { to: '/preferences', label: 'Preferences' },
]

describe('SubNav', () => {
  it('is a labelled nav of links, the page shown marked as current', async () => {
    const router = testRouter()
    await router.push('/preferences')
    const { wrapper } = mountWith(SubNav, { router, props: { label: 'Areas', items } })
    await flushPromises()
    const nav = wrapper.find('nav')
    expect(nav.attributes('aria-label')).toBe('Areas')
    expect(nav.find('[role="tablist"]').exists()).toBe(false)
    const links = nav.findAll('li > a')
    expect(links.map((a) => a.text())).toEqual(['General', 'Preferences'])
    expect(links.map((a) => a.attributes('aria-current'))).toEqual([undefined, 'page'])
  })
})
