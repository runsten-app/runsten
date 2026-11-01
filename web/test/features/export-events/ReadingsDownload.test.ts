import { describe, expect, it } from 'vitest'
import { ReadingsDownload } from '@/features/export-events'
import { maxReadingsMs, readingsPeriod } from '@/features/export-events/model/readingsWindow'
import { mountWith } from '@test/utils'

describe('ReadingsDownload', () => {
  it('downloads every reading of the period through a link, saved by the browser', () => {
    const { wrapper } = mountWith(ReadingsDownload, {
      props: { vehicle: 'v1', from: '2026-09-30T22:00:00.000Z', to: '2026-10-31T23:00:00.000Z' },
    })
    const link = wrapper.find('a.readings-download')
    expect(link.attributes('href')).toBe(
      'api/v1/vehicles/v1/readings.csv?from=2026-09-30T22%3A00%3A00.000Z&to=2026-10-31T23%3A00%3A00.000Z',
    )
    expect(link.attributes()).toHaveProperty('download')
    expect(link.text()).toBe('Download every reading (CSV)')
  })

  it.each([
    ['longer than a month', { from: '2026-09-01T00:00:00.000Z', to: '2026-10-02T01:00:01.000Z' }],
    ['open at an end', { from: '2026-09-01T00:00:00.000Z' }],
    ['the whole history', {}],
  ])('is disabled, and says why, for a period %s', (_, period) => {
    const { wrapper } = mountWith(ReadingsDownload, { props: { vehicle: 'v1', ...period } })
    expect(wrapper.find('a.readings-download').exists()).toBe(false)
    const button = wrapper.find('button.readings-download')
    expect(button.attributes()).toHaveProperty('disabled')
    const hint = wrapper.find(`#${button.attributes('aria-describedby') ?? 'none'}`)
    expect(hint.text()).toBe('Every reading: choose 31 days at most.')
  })
})

describe('readingsPeriod', () => {
  it('takes 31 days, one of 25 hours, at most', () => {
    const from = '2026-10-01T00:00:00.000Z'
    const to = new Date(Date.parse(from) + maxReadingsMs).toISOString()
    expect(readingsPeriod(from, to)).toEqual({ from, to })
    expect(
      readingsPeriod(from, new Date(Date.parse(from) + maxReadingsMs + 1).toISOString()),
    ).toBeUndefined()
  })
})
