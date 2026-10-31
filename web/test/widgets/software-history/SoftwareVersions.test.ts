import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SoftwareVersions } from '@/widgets/software-history'
import versions from '@fixtures/software-versions.json'
import { heard, mountWith, seen } from '@test/utils'

const { fetchSoftwareVersions } = vi.hoisted(() => ({ fetchSoftwareVersions: vi.fn() }))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchSoftwareVersions,
}))

beforeEach(() => {
  fetchSoftwareVersions.mockReset()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-10-10T05:00:00Z'))
})

afterEach(() => {
  vi.useRealTimers()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted() {
  const { wrapper } = mountWith(SoftwareVersions, { props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

describe('SoftwareVersions', () => {
  it('lists the versions newest first: when installed, how long run, how long installing', async () => {
    fetchSoftwareVersions.mockResolvedValue(versions)
    const w = await mounted()
    expect(w.find('h2').text()).toBe('Versions')
    expect(w.findAll('thead th').map((h) => h.text())).toEqual([
      'Version',
      'Ran for',
      'Installation',
    ])
    const rows = w.findAll('.version')
    expect(rows.map((r) => plain(r.find('th').text()))).toEqual([
      '2026.38.3.2 Running Installed 28 Sept 2026, 04:00–05:00',
      '2026.38.3.1 Installed 28 Sept 2026, 02:30–03:00',
      // The first Runsten read was installed at any time before.
      '2026.32.1 First read 28 Sept 2026, 01:00',
    ])
    expect(rows.map((r) => plain(r.find('.ran').text()))).toEqual([
      '12 days so far',
      '1 hr – 2 hrs, 30 mins',
      'at least 1 hr, 30 mins',
    ])
    // Seen installing from 03:30–03:31 to the first reading of the version.
    expect(plain(rows[0]?.find('.install').text())).toBe('29 mins – 1 hr, 30 mins')
    // Not seen: a dash, read as such, and said under the list; never 0.
    const unseen = w.findAll('.install').at(1)
    expect(unseen && seen(unseen.element)).toBe('—')
    expect(unseen && heard(unseen.element)).toBe('Installation not seen')
    expect(w.find('.note').text()).toMatch(/^—: no reading saw the installation/)
    expect(w.find('.more').exists()).toBe(false)
  })

  it('reads the next page on demand', async () => {
    const [first, second, third] = versions.items
    fetchSoftwareVersions
      .mockResolvedValueOnce({ items: [first, second], next_cursor: 'next' })
      .mockResolvedValueOnce({ items: [third], next_cursor: null })
    const w = await mounted()
    expect(w.findAll('.version')).toHaveLength(2)
    await w.find('.more').trigger('click')
    await flushPromises()
    expect(fetchSoftwareVersions).toHaveBeenLastCalledWith('v1', 'next')
    expect(w.findAll('.version')).toHaveLength(3)
    expect(w.find('.more').exists()).toBe(false)
  })

  it('says when the next page fails, keeping the versions read', async () => {
    const [first, second] = versions.items
    fetchSoftwareVersions
      .mockResolvedValueOnce({ items: [first, second], next_cursor: 'next' })
      .mockRejectedValue(new Error('down'))
    const w = await mounted()
    await w.find('.more').trigger('click')
    await flushPromises()
    expect(w.findAll('.version')).toHaveLength(2)
    expect(w.find('.v-alert').text()).toBe("The software's versions could not be loaded.")
  })

  it('says when the vehicle tells no version, or the versions fail', async () => {
    fetchSoftwareVersions.mockResolvedValue({ items: [], next_cursor: null })
    let w = await mounted()
    expect(w.find('table').exists()).toBe(false)
    expect(w.find('.none').text()).toBe('This vehicle does not tell the version of its software.')
    w.unmount()
    fetchSoftwareVersions.mockRejectedValue(new Error('down'))
    w = await mounted()
    expect(w.find('.v-alert').text()).toBe("The software's versions could not be loaded.")
    expect(w.find('.none').exists()).toBe(false)
  })
})
