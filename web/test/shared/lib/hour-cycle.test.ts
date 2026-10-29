import { afterEach, describe, expect, it } from 'vitest'
import { i18n } from '@/shared/i18n'
import { hourCycle, setHourCycle, useFormat } from '@/shared/lib'
import { withHourCycle } from '@/shared/lib/hour-cycle'
import { withSetup } from '@test/utils'

const plain = (s: string) => s.replace(/\s/g, ' ')

afterEach(() => {
  setHourCycle('auto')
  localStorage.clear()
  i18n.global.locale.value = 'en'
})

describe('withHourCycle', () => {
  it.each([
    ['en-US', 'auto', 'en-US'],
    ['en-US', 'h23', 'en-US-u-hc-h23'],
    ['fr', 'h12', 'fr-u-hc-h12'],
    ['not a locale', 'h23', 'not a locale'],
  ] as const)('writes %s with the clock %s as %s', (locale, c, want) => {
    expect(withHourCycle(locale, c)).toBe(want)
  })
})

describe('setHourCycle', () => {
  it('follows the locale by default, and keeps the choice', () => {
    expect(hourCycle.value).toBe('auto')
    setHourCycle('h23')
    expect(hourCycle.value).toBe('h23')
    expect(localStorage.getItem('runsten.clock')).toBe('h23')
  })

  it('writes every time on the clock chosen', () => {
    const { result: f } = withSetup(useFormat)
    const at = '2026-10-28T15:55:00Z'
    expect(f.time(at, '2026-10-28')).toBe('15:55')
    setHourCycle('h12')
    expect(plain(f.time(at, '2026-10-28'))).toBe('3:55 pm')
    i18n.global.locale.value = 'fr'
    setHourCycle('h23')
    expect(f.time(at, '2026-10-28')).toBe('15:55')
  })
})
