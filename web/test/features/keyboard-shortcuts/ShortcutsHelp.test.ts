import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { ShortcutsHelp } from '@/features/keyboard-shortcuts'
import { mountWith } from '@test/utils'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('ShortcutsHelp', () => {
  it('lists the sections by their digit, then the keys of every page', async () => {
    mountWith(ShortcutsHelp, {
      props: { modelValue: true, sections: ['State', 'Battery'] },
      attachTo: document.body.appendChild(document.createElement('div')),
    })
    await flushPromises()
    const dialog = document.querySelector('.shortcuts-help')
    expect(dialog?.querySelector('h2')?.textContent?.trim()).toBe('Keyboard shortcuts')
    const rows = [...(dialog?.querySelectorAll('.row') ?? [])].map((r) => [
      r.querySelector('kbd')?.textContent?.trim(),
      r.querySelector('dd')?.textContent?.trim(),
    ])
    expect(rows).toEqual([
      ['1', 'State'],
      ['2', 'Battery'],
      [',', 'Settings'],
      ['?', 'This list'],
      ['Shift, held', 'Show the keys on the page'],
    ])
    expect(dialog?.querySelector('a')?.getAttribute('href')).toBe('/preferences')
  })
})
