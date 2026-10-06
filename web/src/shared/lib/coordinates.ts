export type Position = { lat: number; lon: number }

// Five decimals: about a meter. Coordinates keep the decimal point whatever the locale:
// "45,76400, 4,83570" would be ambiguous, and they are meant to be copied.
const fixed = (n: number) => n.toFixed(5)

// formatCoordinate is one of them, as a field shows it.
export const formatCoordinate = fixed

export function formatCoordinates(p: Position): string {
  return `${fixed(p.lat)}, ${fixed(p.lon)}`
}

// openStreetMapUrl is the position on openstreetmap.org, opened only by an explicit click:
// nothing leaves for a third party otherwise.
export function openStreetMapUrl(p: Position): string {
  const lat = fixed(p.lat)
  const lon = fixed(p.lon)
  return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lon}#map=16/${lat}/${lon}`
}
