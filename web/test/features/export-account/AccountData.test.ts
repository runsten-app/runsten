import { h } from 'vue'
import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { accountExportKeys, type AccountExport } from '@/entities/session'
import { AccountData } from '@/features/export-account'
import { ApiError } from '@/shared/api'
import { mountWith, testQueryClient } from '@test/utils'

const api = vi.hoisted(() => ({ fetchAccountExport: vi.fn(), startAccountExport: vi.fn() }))
vi.mock('@/entities/session', async (original) => ({
  ...(await original<typeof import('@/entities/session')>()),
  ...api,
}))

const job = (status: AccountExport['status'], bytes = 0): AccountExport => ({
  id: '6f0e2d3e-0000-4000-8000-000000000001',
  status,
  error: status === 'failed' ? 'internal' : null,
  bytes,
  created_at: '2026-10-01T08:00:00Z',
  not_before: '2026-10-01T08:00:00Z',
  ended_at: status === 'done' || status === 'failed' ? '2026-10-01T08:05:00Z' : null,
})

let wrapper: VueWrapper | undefined

beforeEach(() => {
  api.fetchAccountExport.mockReset()
  api.startAccountExport.mockReset()
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mounted(exportJob: AccountExport | null = null) {
  api.fetchAccountExport.mockResolvedValue(exportJob)
  const queryClient = testQueryClient()
  const m = mountWith(AccountData, { queryClient })
  wrapper = m.wrapper
  await flushPromises()
  return m
}

async function build(w: VueWrapper) {
  await w.find('.archive-start').trigger('click')
  await flushPromises()
}

describe('AccountData', () => {
  it('downloads the export through a link, saved by the browser', async () => {
    const { wrapper } = await mounted()
    const link = wrapper.find('a.export')
    expect(link.attributes('href')).toBe('api/v1/account/export')
    expect(link.attributes()).toHaveProperty('download')
    expect(link.text()).toBe('Download my data')
  })

  it('shows what the slot adds after the download', async () => {
    const { wrapper } = mountWith({
      render: () => h(AccountData, null, () => h('button', { class: 'extension-action' }, 'More')),
    })
    const row = wrapper.find('a.export').element.parentElement
    expect(row?.lastElementChild?.className).toBe('extension-action')
  })

  it('builds a ZIP archive in the background, none held yet', async () => {
    const { wrapper, queryClient } = await mounted()
    expect(wrapper.find('.archive-start').text()).toBe('Build a ZIP archive')

    api.startAccountExport.mockResolvedValue(job('pending'))
    await build(wrapper)
    expect(api.startAccountExport).toHaveBeenCalledTimes(1)
    expect(queryClient.getQueryData(accountExportKeys.job())).toEqual(job('pending'))
  })

  it('shows the building of the archive while the worker works', async () => {
    const { wrapper } = await mounted(job('running'))
    expect(wrapper.find('.building').text()).toBe('Building the archive')
    expect(wrapper.find('a.archive-download').exists()).toBe(false)
  })

  it('holds the archive once built: downloaded as a link', async () => {
    const { wrapper } = await mounted(job('done', 2 * 1024 * 1024))
    const link = wrapper.find('a.archive-download')
    expect(link.attributes('href')).toBe('api/v1/account/export.zip')
    expect(link.attributes()).toHaveProperty('download')
    expect(link.text()).toBe('Download the ZIP (2.0 MB)')
    expect(wrapper.find('.held-at').text()).toContain('Built')
    // The user asks for another: the one held is replaced.
    api.startAccountExport.mockResolvedValue(job('pending'))
    await build(wrapper)
    expect(wrapper.find('.building').exists()).toBe(true)
  })

  it('tells of a failed build, and builds another one', async () => {
    const { wrapper } = await mounted(job('failed'))
    expect(wrapper.find('.failure').text()).toBe('The archive could not be built.')
    expect(wrapper.find('.archive-start').text()).toBe('Build another archive')
  })

  it('tells of a failure to start, and does not crash', async () => {
    const { wrapper } = await mounted()
    api.startAccountExport.mockRejectedValue(
      new ApiError(409, 'export_active', 'an export of the account is already active'),
    )
    await build(wrapper)
    expect(wrapper.find('.error-message').text()).toBe(
      'An export of this account is already being built.',
    )
  })
})
