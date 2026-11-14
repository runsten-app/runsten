import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tripKeys } from '@/entities/trip'
import { importKeys, type FileImport } from '@/entities/vehicle-import'
import { ImportSources } from '@/features/manage-imports'
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
const trips = new File(['PK'], '00-a-null-01_trip_2026-07-13T02_00_00Z.zip', {
  type: 'application/zip',
})
const waypoints = new File(['PK'], '00-a-null-01_waypoint_2026-07-13T02_00_00Z.zip', {
  type: 'application/zip',
})
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
  const m = mountWith(ImportSources, {
    queryClient,
    props: { vehicle: 'v1' },
    attachTo: document.body,
  })
  wrapper = m.wrapper
  await flushPromises()
  return { w: m.wrapper, queryClient }
}

// choose picks the files, as the browser's picker does once the button opened it.
async function choose(w: VueWrapper, ...files: File[]) {
  const input = w.find('.portal-import input.picker')
  Object.defineProperty(input.element, 'files', { value: files, configurable: true })
  await input.trigger('change')
  await flushPromises()
}

const status = (w: VueWrapper) => w.find('.portal-import [role="status"]').text()

describe('VolvoDataPortalImport', () => {
  it('opens the browser’s picker for many files from its button', async () => {
    const { w } = await mounted()
    const input = w.find('.portal-import input.picker')
    expect(input.attributes('multiple')).toBeDefined()
    expect(input.attributes('accept')).toContain('.zip')
    const click = vi.spyOn(input.element as HTMLInputElement, 'click')
    const button = w.find('.portal-import .toggle')
    expect(button.text()).toBe('Choose the files')
    expect(button.attributes('aria-describedby')).toBe('portal-files-hint')
    await button.trigger('click')
    expect(click).toHaveBeenCalledOnce()
  })

  it('imports the files as soon as they are chosen, shows the report, and reads again what changed', async () => {
    const { w, queryClient } = await mounted()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    expect(w.find('.portal-import a').attributes()).toMatchObject({
      href: 'https://data.volvocars.com/',
      target: '_blank',
    })
    api.importVolvoDataPortal.mockResolvedValue(report)
    await choose(w, trips, waypoints)
    expect(api.importVolvoDataPortal).toHaveBeenCalledWith('v1', [trips, waypoints])
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
    // Cleared, so that the same files may be chosen again.
    expect((w.find('.portal-import input.picker').element as HTMLInputElement).value).toBe('')
  })

  it('shows the button busy while the files are sent', async () => {
    const { w } = await mounted()
    let finish: (r: FileImport) => void = () => {}
    api.importVolvoDataPortal.mockReturnValue(new Promise<FileImport>((r) => (finish = r)))
    await choose(w, trips)
    expect(w.find('.portal-import .toggle').classes()).toContain('v-btn--loading')
    finish(report)
    await flushPromises()
    expect(status(w)).toBe('Import finished')
  })

  it('reports the trips alone when nothing was left', async () => {
    const { w } = await mounted()
    api.importVolvoDataPortal.mockResolvedValue({ ...report, skipped: 0, invalid: 0 })
    await choose(w, trips)
    expect(w.findAll('.portal-import .import-report p')).toHaveLength(1)
  })

  it('does nothing when the picker is closed without a file', async () => {
    const { w } = await mounted()
    await choose(w)
    expect(api.importVolvoDataPortal).not.toHaveBeenCalled()
    expect(w.find('.portal-import .import-error').exists()).toBe(false)
  })

  it.each([
    ['invalid_file', 400, 'These are not the files of the Volvo Data Portal'],
    ['invalid_body', 400, 'Too many files or too large'],
    ['network', 0, 'Runsten could not be reached'],
    ['internal', 500, 'The files could not be imported'],
  ] as const)('tells why the files were refused: %s', async (code, httpStatus, text) => {
    const { w } = await mounted()
    api.importVolvoDataPortal.mockRejectedValue(new ApiError(httpStatus, code, code))
    await choose(w, trips)
    expect(w.find('.portal-import .import-error').text()).toContain(text)
    expect(w.find('.portal-import .import-report').exists()).toBe(false)
    // Other files clear the error.
    api.importVolvoDataPortal.mockResolvedValue(report)
    await choose(w, waypoints, trips)
    expect(w.find('.portal-import .import-error').exists()).toBe(false)
    expect(status(w)).toBe('Import finished')
  })
})
