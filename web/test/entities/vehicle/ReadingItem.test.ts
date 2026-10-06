import { describe, expect, it } from 'vitest'
import { h } from 'vue'
import { detailsStaleAfter, ReadingItem } from '@/entities/vehicle'
import { mountWith } from '@test/utils'

const now = Date.parse('2026-09-28T19:03:00Z')
const reading = {
  value: 64,
  reported_at: null,
  fetched_at: '2026-09-28T17:26:00Z',
  checked_at: '2026-09-28T19:00:00Z',
}
const active = { status: 'active' as const }

function item(props: Record<string, unknown>) {
  return mountWith(ReadingItem, {
    props: { label: 'State of charge', reading, value: '64%', now, connection: active, ...props },
  }).wrapper
}

describe('ReadingItem', () => {
  it('shows the value, its last check and since when it holds', () => {
    const w = item({})
    expect(w.find('dt').text()).toBe('State of charge')
    expect(w.find('.value').text()).toBe('64%')
    const times = w.findAll('time')
    expect(times.map((t) => t.attributes('datetime'))).toEqual([
      reading.checked_at,
      reading.fetched_at,
    ])
    expect(times.map((t) => t.text().replace(/\s/g, ' '))).toEqual([
      'checked 3 min ago',
      'since 17:26',
    ])
    expect(w.find('.reading').classes()).not.toContain('stale')
  })

  it.each([
    ['old', { now: now + 3 * 60 * 60_000 }],
    ['lost connection', { connection: { status: 'reauth_required' } }],
  ])('is stale: %s', (_, props) => {
    const w = item(props)
    expect(w.find('.reading').classes()).toContain('stale')
    expect(w.text()).toContain('stale')
  })

  it('takes the threshold it is given', () => {
    const w = item({ now: now + 3 * 60 * 60_000, staleAfter: detailsStaleAfter })
    expect(w.find('.reading').classes()).not.toContain('stale')
  })

  it.each([
    ['no reading', { reading: null, value: null }],
    ['a reading without a value', { value: null }],
  ])('is unknown with %s', (_, props) => {
    const w = item(props)
    expect(w.find('.value').text()).toBe('Unknown')
  })

  it('has no time without a reading', () => {
    expect(item({ reading: null, value: null }).findAll('time')).toHaveLength(0)
  })

  it('puts its slot in the <dd>, a <dl> holding only its items', () => {
    const props = { label: 'State of charge', reading, value: '64%', now, connection: active }
    const Host = { render: () => h(ReadingItem, props, () => h('div', { class: 'gauge' })) }
    const w = mountWith(Host).wrapper
    expect(w.find('dd .gauge').exists()).toBe(true)
  })
})
