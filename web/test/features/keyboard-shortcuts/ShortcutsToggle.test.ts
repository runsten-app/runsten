import { afterEach, describe, expect, it } from 'vitest'
import { ShortcutsToggle, shortcutsEnabled } from '@/features/keyboard-shortcuts'
import { enableShortcuts } from '@/features/keyboard-shortcuts/model/enabled'
import { mountWith } from '@test/utils'

afterEach(() => {
  enableShortcuts(true)
  localStorage.clear()
})

describe('ShortcutsToggle', () => {
  it('turns the shortcuts off and on, and keeps the choice', async () => {
    const { wrapper } = mountWith(ShortcutsToggle)
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['On', 'Off'])
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['true', 'false'])
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Keyboard shortcuts')

    await buttons[1]?.trigger('click')
    expect(shortcutsEnabled.value).toBe(false)
    expect(localStorage.getItem('runsten.shortcuts')).toBe('off')
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['false', 'true'])

    await buttons[0]?.trigger('click')
    expect(shortcutsEnabled.value).toBe(true)
    expect(localStorage.getItem('runsten.shortcuts')).toBe('on')
  })
})
