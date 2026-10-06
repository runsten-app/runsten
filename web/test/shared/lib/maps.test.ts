import { afterEach, describe, expect, it } from 'vitest'
import { mapsShown, mapTiles, showMaps } from '@/shared/lib'
import { withMapTiles, withoutMapTiles } from '@test/mapMeta'

afterEach(() => {
  withoutMapTiles()
  showMaps(true)
  localStorage.clear()
})

describe('mapTiles', () => {
  it('are none without the metas of runsten-web', () => {
    expect(mapTiles()).toBeNull()
  })

  it('are the metas of runsten-web', () => {
    withMapTiles()
    expect(mapTiles()).toEqual({
      url: 'https://tiles.example/{z}/{x}/{y}.png',
      attribution: '© <OpenStreetMap> contributors',
      vector: false,
    })
  })

  it('are vector tiles when they are a PMTiles file', () => {
    withMapTiles('https://maps.example/europe.pmtiles')
    expect(mapTiles()?.vector).toBe(true)
  })
})

describe('showMaps', () => {
  it('shows the maps by default, and keeps the choice', () => {
    expect(mapsShown.value).toBe(true)
    showMaps(false)
    expect(mapsShown.value).toBe(false)
    expect(localStorage.getItem('runsten.maps')).toBe('off')
  })
})
