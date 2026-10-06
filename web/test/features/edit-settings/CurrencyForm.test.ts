import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { reactive } from 'vue'
import type { Settings } from '@/entities/settings'
import { CurrencyForm } from '@/features/edit-settings'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { mountWith } from '@test/utils'

const { setCurrency } = vi.hoisted(() => ({ setCurrency: vi.fn() }))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  setCurrency,
}))

beforeEach(() => {
  setCurrency.mockReset()
  i18n.global.locale.value = 'en'
})

function form(s: Settings) {
  const m = mountWith(CurrencyForm, { props: { settings: s }, attachTo: document.body })
  const select = m.wrapper.findComponent({ name: 'VSelect' })
  return { ...m, select }
}

describe('CurrencyForm', () => {
  it('says costs need a currency, and offers the accepted ones by name', () => {
    const { wrapper, select } = form(unset as Settings)
    expect(wrapper.find('.no-currency').text()).toBe(
      'No currency chosen yet: the costs of charges cannot be computed.',
    )
    const items = select.props('items') as { value: string; title: string }[]
    expect(items).toHaveLength(unset.currencies.length)
    expect(items.find((i) => i.value === 'SEK')?.title).toBe('Swedish Krona (SEK)')
    expect(select.props('modelValue')).toBeNull()
    expect(wrapper.find('button.save').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('names the currencies in the chosen language', () => {
    i18n.global.locale.value = 'fr'
    const { wrapper, select } = form(settings as Settings)
    const items = select.props('items') as { value: string; title: string }[]
    expect(items.find((i) => i.value === 'EUR')?.title).toBe('euro (EUR)')
    expect(wrapper.find('.no-currency').exists()).toBe(false)
    wrapper.unmount()
  })

  it('saves the currency chosen, and says so where the focus stays', async () => {
    setCurrency.mockResolvedValue({ ...settings, currency: { code: 'SEK', minor_digits: 2 } })
    const { wrapper, select } = form(settings as Settings)
    expect(select.props('modelValue')).toBe('EUR')
    select.vm.$emit('update:modelValue', 'SEK')
    await flushPromises()
    const save = wrapper.find<HTMLButtonElement>('button.save')
    save.element.focus()
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(setCurrency).toHaveBeenCalledWith('SEK')
    expect(wrapper.find('[role="status"]').text()).toBe('Currency saved.')
    expect(document.activeElement).toBe(save.element)
    expect(wrapper.find('.v-alert').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    [
      new ApiError(409, 'currency_in_use', ''),
      'Prices or costs already exist in another currency: the currency can no longer change.',
    ],
    [new ApiError(0, 'network', ''), 'Runsten cannot be reached. Try again in a moment.'],
    [new ApiError(500, 'internal', ''), 'The currency could not be saved. Try again later.'],
  ])('explains %o, until another is chosen', async (error, text) => {
    setCurrency.mockRejectedValue(error)
    const { wrapper, select } = form(settings as Settings)
    select.vm.$emit('update:modelValue', 'SEK')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('.v-alert').text()).toBe(text)
    expect(wrapper.find('[role="status"]').text()).toBe('')
    select.vm.$emit('update:modelValue', 'EUR')
    await flushPromises()
    expect(wrapper.find('.v-alert').exists()).toBe(false)
    wrapper.unmount()
  })

  it('sends nothing without a currency', async () => {
    const { wrapper } = form(unset as Settings)
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    expect(setCurrency).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('follows the settings once they change', async () => {
    const props = reactive({ settings: unset as Settings })
    const { wrapper } = mountWith(CurrencyForm, { props })
    props.settings = settings as Settings
    await flushPromises()
    expect(wrapper.findComponent({ name: 'VSelect' }).props('modelValue')).toBe('EUR')
    props.settings = unset as Settings
    await flushPromises()
    expect(wrapper.findComponent({ name: 'VSelect' }).props('modelValue')).toBeNull()
  })
})
