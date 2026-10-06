import { describe, expect, it } from 'vitest'
import type { Place } from '@/entities/place'
import {
  dayPresets,
  draftOf,
  emptyDraft,
  firstVersionAfter,
  move,
  newWindow,
  nextVersion,
  windowSpan,
} from '@/features/edit-place/model/draft'
import place from '@fixtures/place.json'
import places from '@fixtures/places.json'

describe('draft', () => {
  it('holds a place as typed, numbers as text with a point', () => {
    const d = draftOf(place as Place)
    expect(d).toMatchObject({
      name: 'Maison de Tante Agathe',
      lat: '45.76400',
      lon: '4.83570',
      radius: '100',
      withoutPosition: true,
      maxPower: '11',
      efficiency: '',
      timeZone: 'Europe/Paris',
    })
    expect(d.tariff.map((v) => [v.validFrom, v.price])).toEqual([
      ['2026-02-01', '0.2516'],
      ['2026-08-01', '0.2142'],
    ])
    expect(d.tariff[1]?.windows.map((w) => [w.from, w.to, w.allYear, w.months, w.price])).toEqual([
      ['23:00', '07:00', false, [11, 12, 1, 2, 3], '0.1589'],
      ['02:00', '07:00', false, [4, 5, 6, 7, 8, 9, 10], '0.1589'],
      ['00:00', '00:00', true, [], '0.19'],
    ])
    const work = draftOf(places.items[1] as Place)
    expect([work.maxPower, work.efficiency, work.tariff[0]?.price]).toEqual(['', '0.9', '0'])
  })

  it('copies what it holds: editing the draft leaves the place alone', () => {
    const d = draftOf(place as Place)
    d.tariff[0]?.windows[0]?.days.pop()
    expect(place.tariff[0]?.windows[0]?.days).toHaveLength(7)
  })

  it('starts a new place in the given zone, day and radius, at a position if any', () => {
    expect(
      emptyDraft({ timeZone: 'Europe/Stockholm', today: '2026-09-27', radiusM: 150 }),
    ).toMatchObject({
      name: '',
      lat: '',
      lon: '',
      radius: '150',
      timeZone: 'Europe/Stockholm',
      tariff: [{ validFrom: '2026-09-27', price: '', windows: [] }],
    })
    const at = emptyDraft({
      timeZone: 'UTC',
      today: '2026-09-27',
      radiusM: 100,
      lat: 45.764,
      lon: -4.8,
    })
    expect([at.lat, at.lon]).toEqual(['45.76400', '-4.80000'])
  })

  it('opens a new window every day, all year', () => {
    expect(newWindow()).toMatchObject({
      days: ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'],
      from: '',
      to: '',
      allYear: true,
      months: [],
      price: '',
    })
  })

  it('gives each version and window its own key', () => {
    const d = draftOf(place as Place)
    const keys = [
      ...d.tariff.map((v) => v.key),
      ...d.tariff.flatMap((v) => v.windows.map((w) => w.key)),
    ]
    expect(new Set(keys).size).toBe(keys.length)
  })

  describe('nextVersion', () => {
    const tariff = draftOf(place as Place).tariff

    it('copies the latest version, from today', () => {
      const next = nextVersion(tariff, '2026-09-27')
      expect(next.validFrom).toBe('2026-09-27')
      expect(next.price).toBe('0.2142')
      expect(next.windows.map((w) => w.from)).toEqual(['23:00', '02:00', '00:00'])
      expect(next.key).not.toBe(tariff[1]?.key)
      // A copy: the latest version is left as it was.
      next.windows[0]?.days.pop()
      next.windows[0]?.months.pop()
      expect(tariff[1]?.windows[0]?.days).toHaveLength(7)
      expect(tariff[1]?.windows[0]?.months).toHaveLength(5)
    })

    it('starts the day after the latest when it is today or later', () => {
      expect(nextVersion(tariff, '2026-08-01').validFrom).toBe('2026-08-02')
      expect(nextVersion(tariff, '2026-05-01').validFrom).toBe('2026-08-02')
    })

    it('starts today without a date or a version', () => {
      const undated = [{ ...tariff[0], validFrom: '' }] as typeof tariff
      expect(nextVersion(undated, '2026-09-27').validFrom).toBe('2026-09-27')
      expect(nextVersion([], '2026-09-27')).toMatchObject({
        validFrom: '2026-09-27',
        price: '',
        windows: [],
      })
    })
  })

  it.each([
    ['22:00', '06:00', 'overnight'],
    ['00:00', '00:00', 'allDay'],
    ['08:00', '18:00', null],
    ['', '06:00', null],
    ['22:00', '', null],
  ])('a window %s–%s is %s', (from, to, want) => expect(windowSpan({ from, to })).toBe(want))

  it('has shortcuts for the days', () => {
    expect(dayPresets.weekdays).toEqual(['mon', 'tue', 'wed', 'thu', 'fri'])
    expect(dayPresets.weekend).toEqual(['sat', 'sun'])
    expect(dayPresets.everyDay).toHaveLength(7)
  })

  it('moves an item', () => {
    const items = ['a', 'b', 'c']
    move(items, 0, 2)
    expect(items).toEqual(['b', 'c', 'a'])
    move(items, 2, 0)
    expect(items).toEqual(['a', 'b', 'c'])
    move(items, 5, 0)
    expect(items).toEqual(['a', 'b', 'c'])
  })
})

describe('firstVersionAfter', () => {
  const tariff = (...days: string[]) =>
    days.map((validFrom, key) => ({ key, validFrom, price: '0.2', windows: [] }))
  it.each([
    ['the earliest, wherever it is', tariff('2026-08-01', '2026-02-01'), '2026-01-20', 1],
    ['none when it starts on the day', tariff('2026-02-01'), '2026-02-01', -1],
    ['none when it starts before', tariff('2026-02-01'), '2026-03-01', -1],
    ['the earliest written day', tariff('2026-0', '2026-08-01'), '2026-01-20', 1],
    ['none without a day written', tariff(''), '2026-01-20', -1],
  ])('%s', (_, t, day, want) => {
    expect(firstVersionAfter(t, day)).toBe(want)
  })
})
