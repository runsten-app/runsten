import { Blob as NodeBlob } from 'node:buffer'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  deleteImport,
  fetchImports,
  gzipped,
  importAAOSLogging,
  importKeys,
  importTeslaMate,
  importVolvoDataPortal,
} from '@/entities/vehicle-import'
import imports from '@fixtures/imports.json'
import portalReport from '@fixtures/import-volvo-data-portal.json'
import aaosReport from '@fixtures/import-aaos-logging.json'
import teslaMateReport from '@fixtures/import-teslamate.json'
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

  it('sends the files as a multipart form, all in one field, and reads the report', async () => {
    const spy = post()
    const trips = new File(['PK'], 'trip.zip', { type: 'application/zip' })
    const waypoints = new File(['Timestamp\n'], 'data.csv', { type: 'text/csv' })
    expect(await importVolvoDataPortal(vehicle, [trips, waypoints])).toEqual(portalReport)
    const [path, init] = spy.mock.calls[0] as unknown as PostCall
    expect(path).toBe('/vehicles/{vehicle}/imports/volvo_data_portal')
    expect(init.params).toEqual({ path: { vehicle } })
    const form = init.bodySerializer(init.body) as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.getAll('files')).toEqual([trips, waypoints])
  })
})

describe('import of AAOS Logging', () => {
  afterEach(() => vi.restoreAllMocks())

  it('sends the export as a multipart form, and reads the report', async () => {
    const spy = vi
      .spyOn(api, 'POST')
      .mockResolvedValue({ data: aaosReport, response: json(aaosReport) })
    const exported = new File(['PK'], 'AAOSLogging.zip', { type: 'application/zip' })
    expect(await importAAOSLogging(vehicle, exported)).toEqual(aaosReport)
    const [path, init] = spy.mock.calls[0] as unknown as PostCall
    expect(path).toBe('/vehicles/{vehicle}/imports/aaos_logging')
    expect(init.params).toEqual({ path: { vehicle } })
    const form = init.bodySerializer(init.body) as FormData
    expect(form).toBeInstanceOf(FormData)
    expect(form.get('export')).toBe(exported)
    expect([...form.keys()]).toEqual(['export'])
  })
})

type BodyCall = [
  string,
  {
    params: unknown
    body: unknown
    bodySerializer: (b: unknown) => unknown
    headers: Record<string, string>
  },
]

// jsdom has no CompressionStream: a stand-in that tells what went through it.
class FakeCompression extends TransformStream<Uint8Array, Uint8Array> {
  constructor(format: string) {
    super({
      transform: (chunk, controller) => {
        controller.enqueue(new Uint8Array([0x1f, 0x8b]))
        controller.enqueue(chunk)
      },
    })
    expect(format).toBe('gzip')
  }
}

const bytes = async (b: Blob) => [...new Uint8Array(await b.arrayBuffer())]

describe('import of a TeslaMate backup', () => {
  afterEach(() => vi.restoreAllMocks())
  const plain = new File(['COPY'], 'teslamate.bck')

  it('compresses a plain backup with gzip', async () => {
    vi.stubGlobal('CompressionStream', FakeCompression)
    // jsdom's Blob has no stream(): Node's, as a browser's.
    const { body, type } = await gzipped(new NodeBlob(['COPY']) as unknown as Blob)
    expect(type).toBe('application/gzip')
    expect(await bytes(body)).toEqual([0x1f, 0x8b, ...new TextEncoder().encode('COPY')])
  })

  it('sends a gzipped backup as it is', async () => {
    vi.stubGlobal('CompressionStream', FakeCompression)
    const gz = new File([new Uint8Array([0x1f, 0x8b, 8])], 'teslamate.bck.gz')
    expect(await gzipped(gz)).toEqual({ body: gz, type: 'application/gzip' })
  })

  it('sends it plain where the browser cannot compress', async () => {
    vi.stubGlobal('CompressionStream', undefined)
    expect(await gzipped(plain)).toEqual({ body: plain, type: 'application/octet-stream' })
  })

  it('sends the backup as the body, its costs in the currency given, and reads the report', async () => {
    vi.stubGlobal('CompressionStream', undefined)
    const spy = vi
      .spyOn(api, 'POST')
      .mockResolvedValue({ data: teslaMateReport, response: json(teslaMateReport) })
    const compressed = vi.fn()
    expect(await importTeslaMate(vehicle, plain, 'EUR', compressed)).toEqual(teslaMateReport)
    expect(compressed).toHaveBeenCalledOnce()
    const [path, init] = spy.mock.calls[0] as unknown as BodyCall
    expect(path).toBe('/vehicles/{vehicle}/imports/teslamate')
    expect(init.params).toEqual({ path: { vehicle }, query: { currency: 'EUR' } })
    expect(init.headers).toEqual({ 'Content-Type': 'application/octet-stream' })
    expect(init.bodySerializer(init.body)).toBe(plain)
    await importTeslaMate(vehicle, plain)
    expect((spy.mock.calls[1] as unknown as BodyCall)[1].params).toEqual({
      path: { vehicle },
      query: {},
    })
  })
})
