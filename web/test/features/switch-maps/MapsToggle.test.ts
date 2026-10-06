import { afterEach, describe, expect, it } from 'vitest'
import { MapsToggle } from '@/features/switch-maps'
import { mapsShown, showMaps } from '@/shared/lib'
import { mountWith } from '@test/utils'

afterEach(() => {
  showMaps(true)
  localStorage.clear()
})

describe('MapsToggle', () => {
  it('hides the maps and shows them again, and keeps the choice', async () => {
    const { wrapper } = mountWith(MapsToggle)
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['Shown', 'Hidden'])
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Maps')

    await buttons[1]?.trigger('click')
    expect(mapsShown.value).toBe(false)
    expect(localStorage.getItem('runsten.maps')).toBe('off')
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['false', 'true'])

    await buttons[0]?.trigger('click')
    expect(mapsShown.value).toBe(true)
  })
})
