import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ImportSources } from '@/features/manage-imports'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ fetchImportJob: vi.fn() }))
vi.mock('@/entities/vehicle-import', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle-import')>()),
  ...api,
}))

let wrapper: VueWrapper | undefined
beforeEach(() => {
  api.fetchImportJob.mockReset()
  api.fetchImportJob.mockResolvedValue(null)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted(volvo: boolean) {
  wrapper = mountWith(ImportSources, { props: { vehicle: 'v1', volvo } }).wrapper
  await flushPromises()
  return wrapper
}

describe('ImportSources', () => {
  it("offers Aximote and the Volvo Data Portal under one heading, the portal's form behind a button", async () => {
    const w = await mounted(true)
    expect(w.find('h3').text()).toBe('Add history')
    expect(w.findAll('h4').map((h) => h.text())).toEqual([
      'Import from Aximote Beta',
      'Import from the Volvo Data Portal Beta',
    ])
    // Before a first import, Aximote's form is open; the portal's waits.
    expect(w.find('.import-form').exists()).toBe(true)
    expect(w.find('.portal-form').exists()).toBe(false)
  })

  it('offers Aximote alone to a vehicle that is not a Volvo', async () => {
    const w = await mounted(false)
    expect(w.findAll('h4').map((h) => h.text())).toEqual(['Import from Aximote Beta'])
    expect(w.find('.portal-import').exists()).toBe(false)
  })
})
