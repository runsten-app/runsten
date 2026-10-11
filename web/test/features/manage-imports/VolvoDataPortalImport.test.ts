import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tripKeys } from '@/entities/trip'
import { importKeys, type FileImport } from '@/entities/vehicle-import'
import { VehicleImports } from '@/features/manage-imports'
import { ApiError } from '@/shared/api'
import reportFixture from '@fixtures/import-volvo-data-portal.json'
import { mountWith, testQueryClient } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchImports: vi.fn(),
  fetchImportJob: vi.fn(),
  importVolvoDataPortal: vi.fn(),
}))
vi.mock('@/entities/vehicle-import', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle-import')>()),
  ...api,
}))

const report = reportFixture as FileImport
const trips = new File(['Start time,End time\n'], 'trips.csv', { type: 'text/csv' })
const waypoints = new File(['Timestamp\n'], 'waypoints.csv', { type: 'text/csv' })
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  api.fetchImports.mockResolvedValue([])
  api.fetchImportJob.mockResolvedValue(null)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted() {
  const queryClient = testQueryClient()
  const m = mountWith(VehicleImports, {
    queryClient,
    props: { vehicle: 'v1' },
    attachTo: document.body,
  })
  wrapper = m.wrapper
  await flushPromises()
  return { w: m.wrapper, queryClient }
}

// choose sets the file of a file field, as the browser's picker does.
async function choose(w: VueWrapper, field: 'trips' | 'waypoints', file: File) {
  const input = w.find(`.portal-import .${field} input[type="file"]`)
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
}

async function submit(w: VueWrapper) {
  await w.find('.portal-form').trigger('submit')
  await flushPromises()
}

const status = (w: VueWrapper) => w.find('.portal-import [role="status"]').text()
const summary = (w: VueWrapper) =>
  w.findAll('.portal-import .error-summary li').map((li) => li.text())

describe('VolvoDataPortalImport', () => {
  it('imports the trips and the waypoints, shows the report, and reads again what changed', async () => {
    const { w, queryClient } = await mounted()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(w.find('.portal-import a').attributes()).toMatchObject({
      href: 'https://data.volvocars.com/',
      target: '_blank',
    })
    await choose(w, 'trips', trips)
    await choose(w, 'waypoints', waypoints)
    api.importVolvoDataPortal.mockResolvedValue(report)
    await submit(w)
    expect(api.importVolvoDataPortal).toHaveBeenCalledWith('v1', trips, waypoints)
    expect(status(w)).toBe('Import finished')
    const lines = w.findAll('.portal-import .import-report p').map((p) => p.text())
    expect(lines).toEqual([
      '2 trips imported.',
      "One left: it starts after the vehicle's first reading (Monday, 28 September 2026).",
      '2 rows left, unreadable or out of bounds.',
    ])
    const keys = invalidate.mock.calls.map(
      ([f]) => (f as { queryKey?: unknown } | undefined)?.queryKey,
    )
    expect(keys).toContainEqual(importKeys.list('v1'))
    expect(keys).toContainEqual(tripKeys.lists('v1'))
  })

  it('imports the trips alone', async () => {
    const { w } = await mounted()
    await choose(w, 'trips', trips)
    api.importVolvoDataPortal.mockResolvedValue({ ...report, skipped: 0, invalid: 0 })
    await submit(w)
    expect(api.importVolvoDataPortal).toHaveBeenCalledWith('v1', trips, undefined)
    expect(w.findAll('.portal-import .import-report p')).toHaveLength(1)
  })

  it('asks for the trips file first', async () => {
    const { w } = await mounted()
    await submit(w)
    expect(api.importVolvoDataPortal).not.toHaveBeenCalled()
    expect(summary(w)).toEqual(['Choose the trips file.'])
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
  })

  it.each([
    ['invalid_file', 400, 'This is not a file of the Volvo Data Portal'],
    ['invalid_body', 400, 'The files are too large'],
    ['network', 0, 'Runsten could not be reached'],
    ['internal', 500, 'The files could not be imported'],
  ] as const)('tells why the files were refused: %s', async (code, httpStatus, text) => {
    const { w } = await mounted()
    await choose(w, 'trips', trips)
    api.importVolvoDataPortal.mockRejectedValue(new ApiError(httpStatus, code, code))
    await submit(w)
    expect(summary(w)[0]).toContain(text)
    expect(w.find('.portal-import .trips').text()).toContain(text)
    expect(w.find('.portal-import .import-report').exists()).toBe(false)
    // Another file clears the error.
    await choose(w, 'trips', new File(['x'], 'other.csv'))
    await flushPromises()
    expect(summary(w)).toEqual([])
  })
})
