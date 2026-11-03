import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tripKeys } from '@/entities/trip'
import { importKeys, type FileImport } from '@/entities/vehicle-import'
import { ImportSources } from '@/features/manage-imports'
import { ApiError } from '@/shared/api'
import settingsFixture from '@fixtures/settings.json'
import reportFixture from '@fixtures/import-teslamate.json'
import { mountWith, testQueryClient } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchImports: vi.fn(),
  fetchImportJob: vi.fn(),
  importTeslaMate: vi.fn(),
}))
vi.mock('@/entities/vehicle-import', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle-import')>()),
  ...api,
}))
const settings = vi.hoisted(() => ({ fetchSettings: vi.fn() }))
vi.mock('@/entities/settings', async (original) => ({
  ...(await original<typeof import('@/entities/settings')>()),
  ...settings,
}))

const report = reportFixture as FileImport
const backup = new File(['COPY public.cars (id) FROM stdin;\n'], 'teslamate.bck')
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of [...Object.values(api), settings.fetchSettings]) f.mockReset()
  api.fetchImports.mockResolvedValue([])
  api.fetchImportJob.mockResolvedValue(null)
  settings.fetchSettings.mockResolvedValue(settingsFixture)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted() {
  const queryClient = testQueryClient()
  const m = mountWith(ImportSources, {
    queryClient,
    props: { vehicle: 'v1', volvo: false, tesla: true },
    attachTo: document.body,
  })
  wrapper = m.wrapper
  await flushPromises()
  await m.wrapper.find('.teslamate-import .toggle').trigger('click')
  await flushPromises()
  return { w: m.wrapper, queryClient }
}

async function choose(w: VueWrapper, file: File) {
  const input = w.find('.teslamate-import .backup input[type="file"]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
}

async function submit(w: VueWrapper) {
  await w.find('.teslamate-form').trigger('submit')
  await flushPromises()
}

const status = (w: VueWrapper) => w.find('.teslamate-import [role="status"]').text()
const summary = (w: VueWrapper) =>
  w.findAll('.teslamate-import .error-summary li').map((li) => li.text())

describe('TeslaMateImport', () => {
  it('comes first for a Tesla, tells how to make the backup, and waits behind a button', async () => {
    const { w } = await mounted()
    expect(w.findAll('h4').map((h) => h.text())).toEqual([
      'Import from TeslaMate Beta',
      'Import from Aximote Beta',
    ])
    expect(w.find('.teslamate-import .command').text()).toBe(
      'docker compose exec -T database pg_dump -U teslamate teslamate > teslamate.bck',
    )
    expect(document.activeElement?.id).toBe('teslamate-backup')
    await w.find('.teslamate-form .cancel').trigger('click')
    await flushPromises()
    expect(w.find('.teslamate-form').exists()).toBe(false)
    expect(document.activeElement?.id).toBe('teslamate-import-toggle')
  })

  it('imports the backup with its costs in the account currency, and reads again what changed', async () => {
    const { w, queryClient } = await mounted()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await choose(w, backup)
    const costs = w.find('.teslamate-import .costs')
    expect(costs.text()).toBe("Import TeslaMate's costs, in Euro")
    await costs.find('input').setValue(true)
    api.importTeslaMate.mockResolvedValue(report)
    await submit(w)
    expect(api.importTeslaMate).toHaveBeenCalledWith('v1', backup, 'EUR', expect.any(Function))
    expect(status(w)).toBe('Import finished')
    expect(document.activeElement?.id).toBe('teslamate-import-toggle')
    expect(w.findAll('.teslamate-import .import-report p').map((p) => p.text())).toEqual([
      '3 drives and charges imported.',
      'One left: unfinished, or a value out of bounds.',
    ])
    const keys = invalidate.mock.calls.map(
      ([f]) => (f as { queryKey?: unknown } | undefined)?.queryKey,
    )
    expect(keys).toContainEqual(importKeys.list('v1'))
    expect(keys).toContainEqual(tripKeys.lists('v1'))
  })

  it('leaves the costs out unless asked, and says how to have them without a currency', async () => {
    const { w } = await mounted()
    await choose(w, backup)
    api.importTeslaMate.mockResolvedValue({ ...report, invalid: 0, skipped: 2 })
    await submit(w)
    expect(api.importTeslaMate).toHaveBeenCalledWith('v1', backup, undefined, expect.any(Function))
    expect(w.findAll('.teslamate-import .import-report p')).toHaveLength(2)
    wrapper?.unmount()
    settings.fetchSettings.mockResolvedValue({ ...settingsFixture, currency: null })
    const { w: without } = await mounted()
    expect(without.find('.teslamate-import .costs').exists()).toBe(false)
    expect(without.find('.teslamate-import .no-currency').text()).toContain(
      'TeslaMate keeps no currency',
    )
  })

  it('tells whether the backup is compressed or sent', async () => {
    const { w } = await mounted()
    await choose(w, backup)
    let compressed: () => void = () => {}
    let finish: (r: FileImport) => void = () => {}
    api.importTeslaMate.mockImplementation((_v: string, _b: File, _c: string, done: () => void) => {
      compressed = done
      return new Promise<FileImport>((r) => (finish = r))
    })
    await w.find('.teslamate-form').trigger('submit')
    await flushPromises()
    expect(status(w)).toBe('Compressing the backup…')
    compressed()
    await flushPromises()
    expect(status(w)).toBe('Sending and reading the backup…')
    expect(w.find('.teslamate-form .cancel').attributes('disabled')).toBeDefined()
    finish(report)
    await flushPromises()
    expect(status(w)).toBe('Import finished')
  })

  it('asks for the backup first', async () => {
    const { w } = await mounted()
    await submit(w)
    expect(api.importTeslaMate).not.toHaveBeenCalled()
    expect(summary(w)).toEqual(['Choose the backup.'])
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
    await choose(w, backup)
    expect(w.find('.teslamate-import .backup [role="button"]').attributes('aria-label')).toBe(
      'Remove the backup',
    )
  })

  it.each([
    ['invalid_file', 400, 'This is not a TeslaMate backup Runsten can read'],
    ['no_matching_car', 409, "No car in this backup has this vehicle's VIN"],
    ['invalid_body', 400, 'larger than 4 GB'],
    ['import_active', 409, 'already being imported'],
    ['source_unavailable', 503, 'Runsten is reading other backups'],
    ['network', 0, 'Runsten could not be reached'],
    ['internal', 500, 'The backup could not be imported'],
  ] as const)('tells why the backup was refused: %s', async (code, httpStatus, text) => {
    const { w } = await mounted()
    await choose(w, backup)
    api.importTeslaMate.mockRejectedValue(new ApiError(httpStatus, code, code))
    await submit(w)
    expect(summary(w)[0]).toContain(text)
    expect(w.find('.teslamate-import .backup').text()).toContain(text)
    await choose(w, new File(['x'], 'other.bck'))
    await flushPromises()
    expect(summary(w)).toEqual([])
  })
})
