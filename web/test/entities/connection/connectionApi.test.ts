import { afterEach, describe, expect, it, vi } from 'vitest'
import { connectionKeys, deleteApiKey, fetchConnection, setApiKey } from '@/entities/connection'
import connection from '@fixtures/connection.json'
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

  it('names its query', () => {
    expect(connectionKeys.current()).toEqual(['connection'])
  })
})
