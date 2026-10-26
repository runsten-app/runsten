import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ReleaseInfo } from '@/features/view-release'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import instance from '@fixtures/instance.json'
import withUpdate from '@fixtures/instance-update.json'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({ fetchInstance: vi.fn() }))
vi.mock('@/entities/instance', async (original) => ({
  ...(await original<typeof import('@/entities/instance')>()),
  ...api,
}))

beforeEach(() => {
  api.fetchInstance.mockReset()
  i18n.global.locale.value = 'en'
})

async function info() {
  const { wrapper } = mountWith(ReleaseInfo)
  await flushPromises()
  return wrapper
}

describe('ReleaseInfo', () => {
  it('tells a newer release, its notes and how to update', async () => {
    api.fetchInstance.mockResolvedValue(withUpdate)
    const w = await info()
    expect(w.find('.version').text()).toBe('Version 0.11.0')
    expect(w.find('.update').text()).toContain('Runsten 0.12.0 is out.')
    expect(w.find('.update a.notes').attributes('href')).toBe(
      'https://github.com/runsten-app/runsten/releases/tag/v0.12.0',
    )
    expect(w.find('.update a.how-to').attributes('href')).toBe(
      'https://runsten.app/docs/self-hosting/updating',
    )
    expect(w.find('.latest').exists()).toBe(false)
  })

  it('says no newer release is out, when the instance checks', async () => {
    api.fetchInstance.mockResolvedValue(instance)
    const w = await info()
    expect(w.find('.update').exists()).toBe(false)
    expect(w.find('.latest').text()).toBe('No newer release.')
    expect(w.find('a.releases').exists()).toBe(false)
  })

  // Without the check, nothing is asked of GitHub: the releases' page, for whoever opens it.
  it('leads to the releases, when the instance does not check', async () => {
    api.fetchInstance.mockResolvedValue({ ...instance, update_check: false })
    const w = await info()
    expect(w.find('.latest').exists()).toBe(false)
    expect(w.find('a.releases .d-sr-only').text()).toBe('(opens in a new tab)')
    expect(w.find('a.releases').attributes('href')).toBe(
      'https://github.com/runsten-app/runsten/releases',
    )
  })

  it('tells a failure', async () => {
    api.fetchInstance.mockRejectedValue(new ApiError(500, 'internal', ''))
    const w = await info()
    expect(w.find('.v-alert').text()).toBe(
      'The version could not be loaded. Try again in a moment.',
    )
  })
})
