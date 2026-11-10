import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  accountExportKeys,
  exportZipPath,
  fetchAccountExport,
  startAccountExport,
} from '@/entities/session'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const pending = {
  id: '6f0e2d3e-0000-4000-8000-000000000001',
  status: 'pending',
  error: null,
  bytes: 0,
  created_at: '2026-10-01T08:00:00Z',
  not_before: '2026-10-01T08:00:00Z',
  ended_at: null,
}

describe('account export API', () => {
  it('reads the account’s export, null when it has none', async () => {
    stubFetch({ 'GET /account/export-job': () => json({ export: null }) })
    expect(await fetchAccountExport()).toBeNull()
  })

  it('reads a pending export', async () => {
    stubFetch({ 'GET /account/export-job': () => json({ export: pending }) })
    expect(await fetchAccountExport()).toEqual(pending)
  })

  it('starts one, written in the background', async () => {
    const requests = stubFetch({ 'POST /account/export-job': () => json(pending, 202) })
    expect(await startAccountExport()).toEqual(pending)
    expect(requests.map((r) => r.method)).toEqual(['POST'])
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ 'POST /account/export-job': () => apiError(409, 'export_active') })
    await expect(startAccountExport()).rejects.toMatchObject({ code: 'export_active' })
  })

  it('has one key for the account’s export, and the archive is a link', () => {
    expect(accountExportKeys.job()).toEqual(['account-export'])
    expect(exportZipPath).toBe('api/v1/account/export.zip')
  })
})
