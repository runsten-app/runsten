import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { tripKeys } from '@/entities/trip'
import { importKeys, type AximoteVehicle, type ImportJob } from '@/entities/vehicle-import'
import { ImportSources } from '@/features/manage-imports'
import { ApiError } from '@/shared/api'
import vehiclesFixture from '@fixtures/aximote-vehicles.json'
import doneFixture from '@fixtures/import-job-done.json'
import pendingFixture from '@fixtures/import-job-pending.json'
import { mountWith, testQueryClient } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchImports: vi.fn(),
  deleteImport: vi.fn(),
  fetchImportJob: vi.fn(),
  fetchAximoteVehicles: vi.fn(),
  startAximoteImport: vi.fn(),
}))
vi.mock('@/entities/vehicle-import', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle-import')>()),
  ...api,
}))

const token = 'axm_pat_AAAAAAAAAAAAAAAAAAAAAAAAAA'
const vehicles = vehiclesFixture.items as AximoteVehicle[]
const pending = pendingFixture.job as ImportJob
const done = doneFixture.job as ImportJob
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  api.fetchImports.mockResolvedValue([])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted(job: ImportJob | null = null) {
  api.fetchImportJob.mockResolvedValue(job)
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

async function paste(w: VueWrapper, value: string) {
  await w.find('#aximote-token').setValue(value)
}

async function submit(w: VueWrapper) {
  await w.find('.import-form').trigger('submit')
  await flushPromises()
}

const status = (w: VueWrapper) => w.find('.aximote-import [role="status"]').text()
const summary = (w: VueWrapper) => w.findAll('.error-summary li').map((li) => li.text())

describe('AximoteImport', () => {
  it('lists the vehicles of the token, the one with the VIN chosen, then imports it', async () => {
    const { w, queryClient } = await mounted()
    expect(w.find('.import-report').exists()).toBe(false)
    await paste(w, `  ${token} `)
    api.fetchAximoteVehicles.mockResolvedValue(vehicles)
    await submit(w)
    expect(api.fetchAximoteVehicles).toHaveBeenCalledWith('v1', token)
    expect(status(w)).toBe('Aximote gives 3 vehicles.')
    const radios = w.findAll('.vehicle')
    expect(radios.map((r) => r.text())).toEqual([
      'Old V60 · Volvo V60 (2016)',
      'My EX30 · Volvo EX30 (2024)Same VIN as this vehicle',
      'veh-bare',
    ])
    expect(w.find<HTMLInputElement>('input[value="veh-ex30"]').element.checked).toBe(true)
    expect(w.find('.no-match').exists()).toBe(false)

    api.startAximoteImport.mockResolvedValue(pending)
    await submit(w)
    expect(api.startAximoteImport).toHaveBeenCalledWith('v1', token, 'veh-ex30')
    expect(status(w)).toBe('Import started: it goes on in the background.')
    // The job's progress replaces the form; the token left the page.
    expect(w.find('.import-form').exists()).toBe(false)
    expect(w.find('.import-progress').text()).toContain('Reading the trips. Nothing imported.')
    expect(queryClient.getQueryData(importKeys.job('v1'))).toEqual(pending)
  })

  it('never guesses the vehicle without a VIN in common', async () => {
    const { w } = await mounted()
    await paste(w, token)
    api.fetchAximoteVehicles.mockResolvedValue(vehicles.map((v) => ({ ...v, vin_matches: false })))
    await submit(w)
    expect(w.find('.no-match').text()).toContain("None has this vehicle's VIN in Aximote")
    expect(w.findAll<HTMLInputElement>('input[type="radio"]').some((r) => r.element.checked)).toBe(
      false,
    )
    await submit(w)
    expect(api.startAximoteImport).not.toHaveBeenCalled()
    expect(summary(w)).toEqual(['Choose the vehicle whose history to import.'])
    expect(w.find('.vehicle-error').text()).toBe('Choose the vehicle whose history to import.')
    expect(w.find('#aximote-vehicle').attributes('aria-describedby')).toBe('aximote-vehicle-error')

    // Another token: the vehicles listed go.
    await paste(w, token.replace('A', 'B'))
    expect(w.find('.vehicles').exists()).toBe(false)
  })

  it('checks the token before sending it', async () => {
    const { w } = await mounted()
    await submit(w)
    expect(summary(w)).toEqual(['Paste the token.'])
    expect(document.activeElement?.classList.contains('error-summary')).toBe(true)
    await paste(w, 'rst_abc')
    expect(summary(w)).toEqual([
      'A token is axm_pat_ followed by 26 letters and digits: copy it again.',
    ])
    expect(w.find('#aximote-token').attributes('aria-invalid')).toBe('true')
    expect(api.fetchAximoteVehicles).not.toHaveBeenCalled()
  })

  it.each([
    ['token_refused', 400, 'Aximote refused this token: check it, or create another one.'],
    ['source_unavailable', 502, 'Aximote did not answer. Try again in a moment.'],
    ['network', 0, 'Runsten could not be reached. Try again in a moment.'],
    ['internal', 500, 'The import could not start. Try again in a moment.'],
  ])('tells %s', async (code, httpStatus, text) => {
    const { w } = await mounted()
    await paste(w, token)
    api.fetchAximoteVehicles.mockRejectedValue(new ApiError(httpStatus, code as 'internal', ''))
    await submit(w)
    expect(summary(w)).toEqual([text])
  })

  it('tells an import already running', async () => {
    const { w } = await mounted()
    await paste(w, token)
    api.fetchAximoteVehicles.mockResolvedValue(vehicles)
    await submit(w)
    api.startAximoteImport.mockRejectedValue(new ApiError(409, 'import_active', ''))
    await submit(w)
    expect(summary(w)).toEqual(['An import of this vehicle is already in progress.'])
  })

  it('shows the progress, the wait asked for, then reads everything again once done', async () => {
    const running = { ...pending, status: 'running', step: 'charges', imported: 4 } as ImportJob
    const { w, queryClient } = await mounted({ ...pending, not_before: '2999-01-01T00:00:00Z' })
    expect(w.find('.waiting').text()).toContain('Aximote asked to wait: the import resumes')
    expect(w.find('.import-form').exists()).toBe(false)

    api.fetchImportJob.mockResolvedValue(running)
    await queryClient.refetchQueries({ queryKey: importKeys.job('v1') })
    await flushPromises()
    expect(w.find('.step').text()).toBe('Reading the charges. 4 trips and charges imported.')
    expect(w.find('.waiting').exists()).toBe(false)

    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    api.fetchImportJob.mockResolvedValue(done)
    await queryClient.refetchQueries({ queryKey: importKeys.job('v1') })
    await flushPromises()
    const keys = invalidate.mock.calls.map(
      ([f]) => (f as { queryKey?: unknown } | undefined)?.queryKey,
    )
    for (const k of [
      importKeys.list('v1'),
      tripKeys.lists('v1'),
      chargeKeys.all(),
      statsKeys.every(),
    ])
      expect(keys).toContainEqual(k)
    expect(status(w)).toBe('Import finished')
    expect(w.find('.import-form').exists()).toBe(false)
    expect(w.find('.toggle').text()).toBe('Import again')
  })

  it('opens the form again behind a button once an import is over, and closes it', async () => {
    const { w } = await mounted(done)
    expect(w.find('.import-form').exists()).toBe(false)
    await w.find('.toggle').trigger('click')
    await flushPromises()
    expect(document.activeElement?.id).toBe('aximote-token')
    await paste(w, token)
    api.fetchAximoteVehicles.mockResolvedValue(vehicles)
    await submit(w)
    await w.find('.cancel').trigger('click')
    await flushPromises()
    expect(w.find('.import-form').exists()).toBe(false)
    expect(document.activeElement?.id).toBe('aximote-import-toggle')

    // Opened again: empty, its vehicles gone.
    await w.find('.toggle').trigger('click')
    await flushPromises()
    expect(w.find<HTMLInputElement>('#aximote-token').element.value).toBe('')
    expect(w.find('.vehicles').exists()).toBe(false)
  })

  it('reports an import done: imported, left after the cutoff, invalid', async () => {
    const { w } = await mounted(done)
    const report = w.find('.import-report')
    expect(report.text()).toContain('Import finished')
    expect(report.text()).toContain('3 trips and charges imported.')
    expect(report.text()).toContain(
      "One left: it starts after the vehicle's first reading (Monday, 28 September 2026).",
    )
    expect(report.text()).toContain('2 left unfinished or out of bounds.')
  })

  it('reports an import failed, and why', async () => {
    const { w } = await mounted({
      ...done,
      status: 'failed',
      error: 'token_refused',
      imported: 0,
      skipped: 0,
      invalid: 0,
    })
    const report = w.find('.import-report')
    expect(report.text()).toContain('Import failed')
    expect(report.find('.failure').text()).toBe(
      'Aximote refused the token: check it, or create another one.',
    )
    expect(report.text()).toContain('Nothing imported.')
  })
})
