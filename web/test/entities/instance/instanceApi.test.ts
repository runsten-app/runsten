import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchInstance, instanceKeys } from '@/entities/instance'
import instance from '@fixtures/instance-update.json'
import { json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

describe('instance API', () => {
  it('reads the release running', async () => {
    stubFetch({ 'GET /instance': () => json(instance) })
    expect(await fetchInstance()).toEqual(instance)
  })

  it('names its query', () => {
    expect(instanceKeys.current()).toEqual(['instance'])
  })
})
