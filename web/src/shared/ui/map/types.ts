import type { Position } from '@/shared/lib'

// A marker is a position the map shows, named in its tooltip.
export type MapMarker = { position: Position; label: string }
// A circle is a place: its center and radius.
export type MapCircle = { center: Position; radiusM: number }
// A spot is where trips ended, drawn by how often: weight from 0 to 1.
export type MapSpot = { position: Position; label: string; weight: number }
// A link is the trips between two spots, a straight line drawn by how often.
export type MapLink = { from: Position; to: Position; label: string; weight: number }
