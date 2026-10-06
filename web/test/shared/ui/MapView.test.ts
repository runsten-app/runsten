import { flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { showMaps } from '@/shared/lib'
import { MapView } from '@/shared/ui'
import { withMapTiles, withoutMapTiles } from '@test/mapMeta'
import { mountWith } from '@test/utils'

const mountedViews: { unmount: () => void }[] = []

afterEach(() => {
  mountedViews.splice(0).forEach((w) => w.unmount())
  document.body.innerHTML = ''
  withoutMapTiles()
  vi.unstubAllGlobals()
  showMaps(true)
  localStorage.clear()
})

const lyon = { lat: 45.764, lon: 4.8357 }
const villeurbanne = { lat: 45.7797, lon: 4.927 }

async function mounted(props: Record<string, unknown> = {}) {
  const { wrapper } = mountWith(MapView, {
    props: {
      label: 'Map of the trip',
      markers: [
        { position: lyon, label: 'Home <sweet>' },
        { position: villeurbanne, label: 'End' },
      ],
      route: true,
      ...props,
    },
    attachTo: document.body.appendChild(document.createElement('div')),
  })
  mountedViews.push(wrapper)
  await flushPromises()
  return wrapper
}

describe('MapView', () => {
  it('is nothing where the instance has no tiles: nothing leaves for a third party', async () => {
    const w = await mounted()
    expect(w.find('.map-frame').exists()).toBe(false)
    expect(document.querySelector('img')).toBeNull()
  })

  it('is nothing when the reader hid the maps', async () => {
    withMapTiles()
    showMaps(false)
    const w = await mounted()
    expect(w.find('.map-frame').exists()).toBe(false)
  })

  it('shows the markers, named, over the tiles, with their attribution as text', async () => {
    withMapTiles()
    const w = await mounted({ hint: 'Press the map' })
    await vi.waitFor(() => expect(document.querySelector('.leaflet-tile')).not.toBeNull())
    const map = w.find('.map-frame')
    expect(map.attributes('role')).toBe('region')
    expect(map.attributes('aria-label')).toBe('Map of the trip')
    const tooltips = [...document.querySelectorAll('.leaflet-tooltip')].map((t) => t.textContent)
    expect(tooltips).toEqual(['Home <sweet>', 'End'])
    expect(document.querySelector('.leaflet-control-attribution')?.textContent).toBe(
      '© <OpenStreetMap> contributors',
    )
    const tile = document.querySelector<HTMLImageElement>('.leaflet-tile')
    expect(tile?.src).toMatch(/^https:\/\/tiles\.example\/\d+\/\d+\/\d+\.png$/)
    expect(tile?.referrerPolicy).toBe('strict-origin')
    expect(w.text()).toContain('Press the map')
  })

  it('draws vector tiles on canvases, without images nor inverting them', async () => {
    withMapTiles('https://maps.example/europe.pmtiles')
    // The file is read by range requests, here never answered.
    const fetch = vi.fn<typeof globalThis.fetch>(() => new Promise(() => {}))
    vi.stubGlobal('fetch', fetch)
    const w = await mounted()
    await vi.waitFor(() => expect(document.querySelector('canvas.leaflet-tile')).not.toBeNull())
    expect(String(fetch.mock.calls[0]?.[0])).toBe('https://maps.example/europe.pmtiles')
    expect(document.querySelector('img')).toBeNull()
    expect(w.find('.map-frame').classes()).not.toContain('dark')
    expect(document.querySelector('.leaflet-control-attribution')?.textContent).toBe(
      '© <OpenStreetMap> contributors',
    )
  })
})
