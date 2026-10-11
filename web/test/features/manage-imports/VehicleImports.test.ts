import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { tripKeys } from '@/entities/trip'
import type { VehicleImport } from '@/entities/vehicle-import'
import { VehicleImports } from '@/features/manage-imports'
import { ApiError } from '@/shared/api'
import imports from '@fixtures/imports.json'
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

const aximote = imports.items[0] as VehicleImport
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted(list: VehicleImport[] | Error = [aximote]) {
  api.fetchImportJob.mockResolvedValue(null)
  if (list instanceof Error) api.fetchImports.mockRejectedValue(list)
  else api.fetchImports.mockResolvedValue(list)
  const queryClient = testQueryClient()
  const mounted = mountWith(VehicleImports, {
    queryClient,
    props: { vehicle: 'v1' },
    attachTo: document.body,
  })
  wrapper = mounted.wrapper
  await flushPromises()
  return { w: mounted.wrapper, queryClient }
}

const status = (w: VueWrapper) => w.find('.vehicle-imports > [role="status"]').text()

describe('VehicleImports', () => {
  it('offers the import from Aximote above the list, empty without an import', async () => {
    const { w } = await mounted([])
    expect(w.findAll('h3').map((h) => h.text())).toEqual(['Import from Aximote Beta', 'Imports'])
    expect(w.find('.none').text()).toBe('Nothing imported.')
    expect(api.fetchImports).toHaveBeenCalledWith('v1')
  })

  it('lists the imports by app, what they hold and when', async () => {
    const { w } = await mounted([
      aximote,
      { ...aximote, source: 'volvo_csv', trips: 0, charges: 1 },
    ])
    expect(w.find('h2').text()).toBe('Imported history')
    const rows = w.findAll('.import')
    expect(rows.map((r) => r.find('.imported').text())).toEqual([
      'Imported from Aximote',
      'Imported from the Volvo Cars app',
    ])
    expect(rows[0]?.find('.counts').text()).toBe('1 trip · 1 charge')
    expect(rows[1]?.find('.counts').text()).toBe('no trip · 1 charge')
    expect(rows[0]?.find('.span').text()).toBe(
      'From Sunday, 27 September 2026 to Sunday, 27 September 2026',
    )
    expect(rows[0]?.find('.delete').attributes('aria-label')).toBe('Imported from Aximote: delete')
  })

  it('tells a failure to list them', async () => {
    const { w } = await mounted(new ApiError(500, 'internal', ''))
    expect(w.find('.v-alert').text()).toBe(
      'The imports could not be loaded. Try again in a moment.',
    )
  })

  it('deletes an import once confirmed, and reads its events again', async () => {
    const { w, queryClient } = await mounted()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    await w.find('.delete').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Delete this import?')
    expect(document.body.textContent).toContain(
      'Imported from Aximote: 1 trip · 1 charge. They leave the lists and the statistics',
    )

    api.deleteImport.mockRejectedValueOnce(new ApiError(500, 'internal', ''))
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The import could not be deleted.')

    api.deleteImport.mockResolvedValue(undefined)
    api.fetchImports.mockResolvedValue([])
    document.querySelector<HTMLButtonElement>('.confirm-delete')?.click()
    await flushPromises()
    expect(api.deleteImport).toHaveBeenLastCalledWith('v1', 'aximote')
    expect(status(w)).toBe('Imported from Aximote: deleted.')
    expect(document.activeElement?.id).toBe('vehicle-imports-title')
    const keys = invalidate.mock.calls.map(
      ([f]) => (f as { queryKey?: unknown } | undefined)?.queryKey,
    )
    for (const k of [
      tripKeys.lists('v1'),
      tripKeys.details(),
      chargeKeys.all(),
      chargeKeys.details(),
      statsKeys.every(),
    ])
      expect(keys).toContainEqual(k)
    expect(w.findAll('.import')).toHaveLength(0)
    expect(w.find('.none').text()).toBe('Nothing imported.')
  })

  it('cancels a deletion', async () => {
    const { w } = await mounted()
    await w.find('.delete').trigger('click')
    await flushPromises()
    const cancel = [...document.querySelectorAll<HTMLButtonElement>('.v-dialog button')].find(
      (b) => b.textContent?.trim() === 'Cancel',
    )
    cancel?.click()
    await flushPromises()
    expect(api.deleteImport).not.toHaveBeenCalled()
  })
})
