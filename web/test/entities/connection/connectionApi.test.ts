import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  connectionKeys,
  deleteApiKey,
  deleteBmw,
  deletePolestarCredential,
  fetchConnection,
  pollBmwToken,
  setApiKey,
  setPolestarCredential,
  startBmwAuthorization,
} from '@/entities/connection'
import connection from '@fixtures/connection.json'
import bmwDevice from '@fixtures/bmw-authorization.json'
import bmw from '@fixtures/connection-bmw.json'
import polestar from '@fixtures/connection-polestar.json'
import { apiError, json, stubFetch } from '@test/http'
import { ApiError } from '@/shared/api'

afterEach(() => vi.unstubAllGlobals())

describe('connection API', () => {
  it('reads the connection, gives the key and removes it', async () => {
    const requests = stubFetch({
      'GET /connection': () => json(connection),
      'PUT /connection/api-key': () => json(connection),
      'DELETE /connection/api-key': () => new Response(null, { status: 204 }),
    })
    expect(await fetchConnection()).toEqual(connection)
    expect(await setApiKey('0123456789abcdef0123456789abcdef')).toEqual(connection)
    await deleteApiKey()
    expect(await requests[1]?.json()).toEqual({ key: '0123456789abcdef0123456789abcdef' })
    expect(requests.map((r) => r.method)).toEqual(['GET', 'PUT', 'DELETE'])
  })

  it('gives the Polestar credential in the body, and removes it', async () => {
    const requests = stubFetch({
      'PUT /connection/polestar': () => json(polestar),
      'DELETE /connection/polestar': () => new Response(null, { status: 204 }),
    })
    const body = { client_id: 'runsten-dev', client_secret: 'secret-1234', account_id: 'acct-5678' }
    expect(await setPolestarCredential(body)).toEqual(polestar)
    await deletePolestarCredential()
    expect(await requests[0]?.json()).toEqual(body)
    expect(requests.map((r) => [r.method, new URL(r.url).search])).toEqual([
      ['PUT', ''],
      ['DELETE', ''],
    ])
  })

  it("starts BMW's flow with the client ID, asks for its token, and removes the connection", async () => {
    const requests = stubFetch({
      'POST /connection/bmw/authorization': () => json(bmwDevice),
      'POST /connection/bmw/token': () => json(bmw),
      'DELETE /connection/bmw': () => new Response(null, { status: 204 }),
    })
    expect(await startBmwAuthorization('4c1cb0a0-1b0e-4d1e')).toEqual(bmwDevice)
    expect(await pollBmwToken()).toEqual(bmw)
    await deleteBmw()
    expect(await requests[0]?.json()).toEqual({ client_id: '4c1cb0a0-1b0e-4d1e' })
    expect(requests[1]?.method).toBe('POST')
    expect(await requests[1]?.text()).toBe('')
    expect(requests.map((r) => r.method)).toEqual(['POST', 'POST', 'DELETE'])
  })

  it('tells how long to wait for BMW, from the Retry-After of a code not approved yet', async () => {
    stubFetch({
      'POST /connection/bmw/token': () =>
        apiError(409, 'authorization_pending', { 'Retry-After': '7' }),
    })
    const e: unknown = await pollBmwToken().catch((x: unknown) => x)
    expect(e).toBeInstanceOf(ApiError)
    expect(e).toMatchObject({ status: 409, code: 'authorization_pending', retryAfter: 7 })
  })

  it('names its query', () => {
    expect(connectionKeys.current()).toEqual(['connection'])
  })
})
