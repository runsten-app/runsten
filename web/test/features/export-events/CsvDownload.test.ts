import { describe, expect, it } from 'vitest'
import { CsvDownload } from '@/features/export-events'
import { mountWith } from '@test/utils'

describe('CsvDownload', () => {
  it('downloads the trips of the period through a link, saved by the browser', () => {
    const { wrapper } = mountWith(CsvDownload, {
      props: {
        kind: 'trips',
        vehicle: 'v1',
        from: '2026-09-01T00:00:00.000Z',
        to: '2026-10-01T00:00:00.000Z',
      },
    })
    const link = wrapper.find('a.csv-download')
    expect(link.attributes('href')).toBe(
      'api/v1/vehicles/v1/trips.csv?from=2026-09-01T00%3A00%3A00.000Z&to=2026-10-01T00%3A00%3A00.000Z',
    )
    expect(link.attributes()).toHaveProperty('download')
    expect(link.text()).toBe('Download CSV')
  })

  it('downloads the whole history of the charges without a period', () => {
    const { wrapper } = mountWith(CsvDownload, { props: { kind: 'charges', vehicle: 'v1' } })
    expect(wrapper.find('a.csv-download').attributes('href')).toBe('api/v1/vehicles/v1/charges.csv')
  })
})
