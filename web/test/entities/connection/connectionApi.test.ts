import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  connectionKeys,
  deleteApiKey,
  deletePolestarCredential,
  fetchConnection,
  setApiKey,
  setPolestarCredential,
} from '@/entities/connection'
import connection from '@fixtures/connection.json'
import polestar from '@fixtures/connection-polestar.json'
import { json, stubFetch } from '@test/http'

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

  it('names its query', () => {
    expect(connectionKeys.current()).toEqual(['connection'])
  })
})
