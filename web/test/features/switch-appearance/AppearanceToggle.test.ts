import { afterEach, describe, expect, it } from 'vitest'
import { vuetify } from '@/app/providers/vuetify'
import { AppearanceToggle } from '@/features/switch-appearance'
import { storedAppearance } from '@/shared/theme'
import { mountWith } from '@test/utils'

afterEach(() => {
  vuetify.theme.change('system')
  localStorage.clear()
})

describe('AppearanceToggle', () => {
  it('follows the system until another scheme is chosen, and keeps the choice', async () => {
    const { wrapper } = mountWith(AppearanceToggle)
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['Automatic', 'Light', 'Dark'])
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['true', 'false', 'false'])
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Appearance')

    await buttons[2]?.trigger('click')
    expect(vuetify.theme.name.value).toBe('dark')
    expect(vuetify.theme.isSystem.value).toBe(false)
    expect(storedAppearance()).toBe('dark')
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['false', 'false', 'true'])

    await buttons[0]?.trigger('click')
    expect(vuetify.theme.isSystem.value).toBe(true)
    expect(storedAppearance()).toBe('system')
  })

  it('ignores what it did not store', () => {
    localStorage.setItem('runsten.appearance', 'sepia')
    expect(storedAppearance()).toBe('system')
  })
})
