import { describe, expect, it } from 'vitest'
import { LocaleSelect } from '@/features/switch-locale'
import { i18n } from '@/shared/i18n'
import { mountWith } from '@test/utils'

describe('LocaleSelect', () => {
  it('names each language in itself and switches', async () => {
    i18n.global.locale.value = 'en'
    const { wrapper } = mountWith(LocaleSelect)
    const select = wrapper.findComponent({ name: 'VSelect' })
    expect(select.props('items')).toEqual([
      { value: 'en', title: 'English' },
      { value: 'fr', title: 'Français' },
      { value: 'sv', title: 'Svenska' },
    ])
    select.vm.$emit('update:modelValue', 'fr')
    expect(i18n.global.locale.value).toBe('fr')
    select.vm.$emit('update:modelValue', 'de')
    expect(i18n.global.locale.value).toBe('fr')
    i18n.global.locale.value = 'en'
  })
})
