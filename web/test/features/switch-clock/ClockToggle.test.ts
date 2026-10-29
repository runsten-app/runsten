import { afterEach, describe, expect, it } from 'vitest'
import { ClockToggle } from '@/features/switch-clock'
import { hourCycle, setHourCycle } from '@/shared/lib'
import { mountWith } from '@test/utils'

afterEach(() => {
  setHourCycle('auto')
  localStorage.clear()
})

describe('ClockToggle', () => {
  it('chooses the clock, and keeps the choice', async () => {
    const { wrapper } = mountWith(ClockToggle)
    const buttons = wrapper.findAll('button')
    expect(buttons.map((b) => b.text())).toEqual(['Automatic', '24-hour', '12-hour'])
    expect(wrapper.find('[role="group"]').attributes('aria-label')).toBe('Clock')

    await buttons[2]?.trigger('click')
    expect(hourCycle.value).toBe('h12')
    expect(localStorage.getItem('runsten.clock')).toBe('h12')
    expect(buttons.map((b) => b.attributes('aria-pressed'))).toEqual(['false', 'false', 'true'])
  })
})
