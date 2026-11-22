import { describe, expect, it } from 'vitest'
import { nextTick, reactive } from 'vue'
import { TileFigure } from '@/shared/ui'
import { mountWith } from '@test/utils'

describe('TileFigure’s rolling digits', () => {
  it('keeps the text as written, each digit on its strip', () => {
    const { wrapper } = mountWith(TileFigure, { props: { text: '1 234,5 km' } })
    expect(wrapper.find('.figure-value').text()).toBe('1 234,5')
    const digits = wrapper.findAll('.roll')
    expect(digits.map((d) => d.text()).join('')).toBe('12345')
    expect(digits.map((d) => d.attributes('style'))).toEqual(
      ['1', '2', '3', '4', '5'].map((n) => `--digit: ${n};`),
    )
  })

  it('rolls the digits in place from the right, and brings a new one in', async () => {
    const props = reactive({ text: '99 %' })
    const { wrapper } = mountWith(TileFigure, { props })
    const [tens, units] = wrapper.findAll('.roll').map((d) => d.element)
    props.text = '100 %'
    await nextTick()
    const after = wrapper.findAll('.roll')
    expect(after.map((d) => d.text()).join('')).toBe('100')
    // The units and the tens are the same elements, rolled; the hundreds is new.
    expect(after[2]?.element).toBe(units)
    expect(after[1]?.element).toBe(tens)
  })
})
