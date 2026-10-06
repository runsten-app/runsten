// withMapTiles writes the metas runsten-web writes for an instance with map tiles,
// raster tiles unless told otherwise.
export function withMapTiles(url = 'https://tiles.example/{z}/{x}/{y}.png') {
  for (const [name, content] of [
    ['map-tiles', url],
    ['map-attribution', '© <OpenStreetMap> contributors'],
  ] as const) {
    const m = document.createElement('meta')
    m.name = name
    m.content = content
    document.head.append(m)
  }
}

export function withoutMapTiles() {
  document.head.querySelectorAll('meta[name^="map-"]').forEach((m) => m.remove())
}
