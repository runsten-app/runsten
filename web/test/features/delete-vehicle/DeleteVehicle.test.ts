import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { vehicleKeys, type Vehicle } from '@/entities/vehicle'
import { DeleteVehicle } from '@/features/delete-vehicle'
import { ApiError } from '@/shared/api'
import vehicles from '@fixtures/vehicles.json'
import { mountWith, testQueryClient } from '@test/utils'

const api = vi.hoisted(() => ({ deleteVehicle: vi.fn() }))
vi.mock('@/entities/vehicle', async (original) => ({
  ...(await original<typeof import('@/entities/vehicle')>()),
  ...api,
}))

const list = vehicles.items as Vehicle[]
const [deleted, kept] = [list[0], list[1]]
let wrapper: VueWrapper | undefined

beforeEach(() => {
  api.deleteVehicle.mockReset()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted() {
  const queryClient = testQueryClient()
  queryClient.setQueryData(vehicleKeys.list(), list)
  queryClient.setQueryData(vehicleKeys.detail(deleted?.id ?? ''), deleted)
  queryClient.setQueryData(vehicleKeys.detail(kept?.id ?? ''), kept)
  const m = mountWith(DeleteVehicle, {
    queryClient,
    props: { vehicle: deleted?.id, label: 'EX30' },
    attachTo: document.body,
  })
  await m.router.push({ name: 'vehicleSettings', params: { vehicle: deleted?.id } })
  wrapper = m.wrapper
  await flushPromises()
  return { w: m.wrapper, queryClient, router: m.router }
}

const button = (text: string) =>
  [...document.querySelectorAll<HTMLButtonElement>('.v-dialog button')].find(
    (b) => b.textContent?.trim() === text,
  )

describe('DeleteVehicle', () => {
  it('names the vehicle, and says what goes with it once asked', async () => {
    const { w } = await mounted()
    expect(w.find('h3').text()).toBe('Delete this vehicle')
    expect(w.find('.delete').text()).toBe('Delete EX30')
    await w.find('.delete').trigger('click')
    await flushPromises()
    expect(document.querySelector('#delete-vehicle-dialog-title')?.textContent?.trim()).toBe(
      'Delete EX30?',
    )
    expect(document.body.textContent).toContain('are deleted for good')
    expect(document.body.textContent).toContain('adds it as a new vehicle, without this history')
    // The section behind the dialog is inert; the dialog is outside it.
    expect(w.find('.delete-vehicle').attributes('inert')).toBeDefined()
    expect(document.querySelector('.confirm-delete')?.closest('[inert]')).toBeNull()
  })

  it('cancels', async () => {
    const { w } = await mounted()
    await w.find('.delete').trigger('click')
    await flushPromises()
    button('Cancel')?.click()
    await flushPromises()
    expect(api.deleteVehicle).not.toHaveBeenCalled()
    expect(w.find('.delete-vehicle').attributes('inert')).toBeUndefined()
  })

  it('stays open while the deletion runs', async () => {
    const { w } = await mounted()
    await w.find('.delete').trigger('click')
    await flushPromises()
    let settle: (v?: unknown) => void = () => undefined
    api.deleteVehicle.mockReturnValue(new Promise((resolve) => (settle = resolve)))
    button('Delete')?.click()
    await flushPromises()
    expect(button('Cancel')?.disabled).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await flushPromises()
    expect(document.querySelector('.confirm-delete')).not.toBeNull()
    expect(w.find('.delete-vehicle').attributes('inert')).toBeDefined()
    settle()
    await flushPromises()
  })

  it('tells a failure, then deletes, goes home and forgets the vehicle', async () => {
    const { w, queryClient, router } = await mounted()
    await w.find('.delete').trigger('click')
    await flushPromises()

    api.deleteVehicle.mockRejectedValueOnce(new ApiError(500, 'internal', ''))
    button('Delete')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The vehicle could not be deleted.')
    expect(router.currentRoute.value.name).toBe('vehicleSettings')

    api.deleteVehicle.mockResolvedValue(undefined)
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    button('Delete')?.click()
    await flushPromises()
    expect(api.deleteVehicle).toHaveBeenLastCalledWith(deleted?.id)
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('home'))
    expect(queryClient.getQueryData<Vehicle[]>(vehicleKeys.list())?.map((v) => v.id)).not.toContain(
      deleted?.id,
    )
    expect(queryClient.getQueryData(vehicleKeys.detail(deleted?.id ?? ''))).toBeUndefined()
    expect(queryClient.getQueryData(vehicleKeys.detail(kept?.id ?? ''))).toEqual(kept)
    expect(invalidate).toHaveBeenCalled()
  })
})
