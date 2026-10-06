import type { Position } from '@/shared/lib'

// A marker is a position the map shows, named in its tooltip.
export type MapMarker = { position: Position; label: string }
// A circle is a place: its center and radius.
export type MapCircle = { center: Position; radiusM: number }
