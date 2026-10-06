import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { PreferencesPage } from '@/pages/preferences'
import { i18n } from '@/shared/i18n'
import { withMapTiles, withoutMapTiles } from '@test/mapMeta'
import { mountWith } from '@test/utils'

afterEach(() => {
  i18n.global.locale.value = 'en'
  withoutMapTiles()
})

describe('PreferencesPage', () => {
  it('chooses the language, under its heading', async () => {
    const { wrapper } = mountWith(PreferencesPage)
    await flushPromises()
    expect(wrapper.find('.v-main h1').text()).toBe('Preferences')
    expect(wrapper.find('.v-main h2').text()).toBe('Language')
    wrapper.findComponent({ name: 'VSelect' }).vm.$emit('update:modelValue', 'fr')
    expect(i18n.global.locale.value).toBe('fr')
  })

  it('offers to hide the maps only where the instance has them', async () => {
    const without = mountWith(PreferencesPage).wrapper
    await flushPromises()
    expect(without.find('.maps-toggle').exists()).toBe(false)
    withMapTiles()
    const w = mountWith(PreferencesPage).wrapper
    await flushPromises()
    expect(w.find('#preferences-maps-title').text()).toBe('Maps')
    expect(w.find('.maps-toggle').exists()).toBe(true)
  })
})
