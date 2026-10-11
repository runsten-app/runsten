import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  deleteImport,
  fetchImports,
  importKeys,
  importVolvoDataPortal,
} from '@/entities/vehicle-import'
import imports from '@fixtures/imports.json'
import portalReport from '@fixtures/import-volvo-data-portal.json'
import { api } from '@/shared/api'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const vehicle = 'v1'

describe('import API', () => {
  it("lists a vehicle's imports", async () => {
    stubFetch({ [`GET /vehicles/${vehicle}/imports`]: () => json(imports) })
    expect(await fetchImports(vehicle)).toEqual(imports.items)
  })

  it('deletes one source', async () => {
    const requests = stubFetch({
      [`DELETE /vehicles/${vehicle}/imports/aximote`]: () => new Response(null, { status: 204 }),
    })
    await deleteImport(vehicle, 'aximote')
    expect(requests).toHaveLength(1)
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ [`DELETE /vehicles/${vehicle}/imports/nope`]: () => apiError(404, 'not_found') })
    await expect(deleteImport(vehicle, 'nope')).rejects.toMatchObject({ code: 'not_found' })
  })

  it('has one key per vehicle', () => {
    expect(importKeys.list(vehicle)).toEqual(['imports', vehicle])
  })
})

// jsdom's FormData is not Node's, which its Request takes: the call is read as the
// client makes it, the form as it goes.
type PostCall = [
  string,
  { params: unknown; body: unknown; bodySerializer: (b: unknown) => unknown },
]

describe('import of the Volvo Data Portal', () => {
  const post = () =>
    vi.spyOn(api, 'POST').mockResolvedValue({ data: portalReport, response: json(portalReport) })
  afterEach(() => vi.restoreAllMocks())

  it('sends the files as a multipart form, and reads the report', async () => {
    const spy = post()
    const trips = new File(['Start time,End time\n'], 'trips.csv', { type: 'text/csv' })
    const waypoints = new File(['Timestamp\n'], 'waypoints.csv', { type: 'text/csv' })
    expect(await importVolvoDataPortal(vehicle, trips, waypoints)).toEqual(portalReport)
    const [path, init] = spy.mock.calls[0] as unknown as PostCall
    expect(path).toBe('/vehicles/{vehicle}/imports/volvo_data_portal')
    expect(init.params).toEqual({ path: { vehicle } })
    const form = init.bodySerializer(init.body) as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.get('trips')).toBe(trips)
    expect(form.get('waypoints')).toBe(waypoints)
  })

  it('leaves the waypoints out when there are none', async () => {
    const spy = post()
    await importVolvoDataPortal(vehicle, new File(['x'], 'trips.csv'))
    const form = (spy.mock.calls[0] as unknown as PostCall)[1].body as FormData
    expect(form.has('trips')).toBe(true)
    expect(form.has('waypoints')).toBe(false)
  })
})
