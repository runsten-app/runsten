import { afterEach, describe, expect, it, vi } from 'vitest'
import { deleteImport, fetchImports, importKeys } from '@/entities/vehicle-import'
import imports from '@fixtures/imports.json'
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
