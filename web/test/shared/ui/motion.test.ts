import { describe, expect, it } from 'vitest'
import { nextTick, reactive } from 'vue'
import { LevelBar, StatusPill, TileSkeleton } from '@/shared/ui'
import { mountWith } from '@test/utils'

describe('LevelBar', () => {
  it('sets its level, and flows only while asked, again at each new value', async () => {
    const props = reactive({ value: 140, label: 'Battery', flowing: false })
    const { wrapper } = mountWith(LevelBar, { props })
    expect(wrapper.find('.fill').attributes('style')).toBe('--level: 100%;')
    expect(wrapper.find('.flow').exists()).toBe(false)
    props.flowing = true
    await nextTick()
    const first = wrapper.find('.fill .flow').element
    expect(first).toBeDefined()
    props.value = 60
    await nextTick()
    expect(wrapper.find('.fill .flow').element).not.toBe(first)
  })
})

describe('StatusPill', () => {
  it('breathes when live', () => {
    const { wrapper } = mountWith(StatusPill, { props: { live: true } })
    expect(wrapper.find('.dot.live').exists()).toBe(true)
  })
})

describe('TileSkeleton', () => {
  it('is a status named by its label, in either layout', () => {
    for (const layout of ['tiles', 'rows'] as const) {
      const { wrapper } = mountWith(TileSkeleton, { props: { label: 'Loading', layout } })
      const status = wrapper.find('.tile-skeleton')
      expect(status.attributes('role')).toBe('status')
      expect(status.attributes('aria-label')).toBe('Loading')
    }
  })
})
