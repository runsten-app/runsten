import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SoftwareHistory } from '@/widgets/software-history'
import versions from '@fixtures/software-versions.json'
import { mountWith } from '@test/utils'

const { fetchSoftwareVersions } = vi.hoisted(() => ({ fetchSoftwareVersions: vi.fn() }))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  fetchSoftwareVersions,
}))

beforeEach(() => {
  fetchSoftwareVersions.mockReset()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()

async function mounted() {
  const { wrapper } = mountWith(SoftwareHistory, { props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

describe('SoftwareHistory', () => {
  it('lists the versions newest first, each with when the vehicle took it', async () => {
    fetchSoftwareVersions.mockResolvedValue(versions)
    const w = await mounted()
    const items = w.findAll('.version').map((v) => plain(v.text()))
    expect(items).toHaveLength(3)
    // The newest runs; its bounds are those of an event.
    expect(items[0]).toBe('2026.38.3.2 Running Installed 28 Sept 2026, 04:00–05:00')
    // The first Runsten read was installed at any time before.
    expect(items[2]).toBe('2026.32.1 First read 28 Sept 2026, 01:00')
    expect(w.find('.more').exists()).toBe(false)
  })

  it('shows the latest three, the others on demand', async () => {
    const more = [...versions.items, ...versions.items].map((v, i) => ({ ...v, id: `${i}` }))
    fetchSoftwareVersions.mockResolvedValue({ items: more, next_cursor: null })
    const w = await mounted()
    expect(w.findAll('.version')).toHaveLength(3)
    await w.find('.all').trigger('click')
    expect(w.findAll('.version')).toHaveLength(6)
    expect(w.find('.all').exists()).toBe(false)
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

  it('shows the next page read, past the latest three', async () => {
    const page = (n: number, from: number) =>
      [...versions.items, ...versions.items]
        .slice(0, n)
        .map((v, i) => ({ ...v, id: `${from + i}` }))
    fetchSoftwareVersions
      .mockResolvedValueOnce({ items: page(3, 0), next_cursor: 'next' })
      .mockResolvedValueOnce({ items: page(3, 3), next_cursor: null })
    const w = await mounted()
    await w.find('.more').trigger('click')
    await flushPromises()
    expect(w.findAll('.version')).toHaveLength(6)
  })

  it('says when the next page fails, keeping the versions read', async () => {
    const [first, second] = versions.items
    fetchSoftwareVersions
      .mockResolvedValueOnce({ items: [first, second], next_cursor: 'next' })
      .mockRejectedValue(new Error('down'))
    const w = await mounted()
    await w.find('.more').trigger('click')
    await vi.waitFor(() => expect(w.find('.v-alert').exists()).toBe(true))
    expect(w.findAll('.version')).toHaveLength(2)
  })

  it('shows nothing for a vehicle that tells no version, and says when they fail', async () => {
    fetchSoftwareVersions.mockResolvedValue({ items: [], next_cursor: null })
    expect((await mounted()).find('.software-history').exists()).toBe(false)
    fetchSoftwareVersions.mockRejectedValue(new Error('down'))
    const w = await mounted()
    await vi.waitFor(() => expect(w.find('.v-alert').exists()).toBe(true))
    expect(w.find('.v-alert').text()).toBe("The software's versions could not be loaded.")
  })
})
