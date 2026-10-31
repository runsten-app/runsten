import type { components } from '@/shared/api'

export type Doors = components['schemas']['Doors']
export type Windows = components['schemas']['Windows']
export type Opening = NonNullable<Doors['front_left']>

// OpeningName names a door, a lid or a window, in the order a sentence lists them.
export type OpeningName =
  | 'frontLeftDoor'
  | 'frontRightDoor'
  | 'rearLeftDoor'
  | 'rearRightDoor'
  | 'hood'
  | 'tailgate'
  | 'tankLid'
  | 'frontLeftWindow'
  | 'frontRightWindow'
  | 'rearLeftWindow'
  | 'rearRightWindow'
  | 'sunroof'

export type OpenItem = { name: OpeningName; state: Exclude<Opening, 'closed'> }

// Openings is what the vehicle says of its doors and windows, in a line: whether it is
// locked (null when it does not say), what is open or ajar, and whether it told any door
// or lid, and any window: closed is said only of what it told.
export type Openings = {
  locked: boolean | null
  open: OpenItem[]
  doorsTold: boolean
  windowsTold: boolean
}

// openings sums up the doors and windows; an opening the vehicle does not tell is left
// out, never taken as closed or open.
export function openings(doors: Doors | null, windows: Windows | null): Openings {
  const all: [OpeningName, Opening | null | undefined, 'doors' | 'windows'][] = [
    ['frontLeftDoor', doors?.front_left, 'doors'],
    ['frontRightDoor', doors?.front_right, 'doors'],
    ['rearLeftDoor', doors?.rear_left, 'doors'],
    ['rearRightDoor', doors?.rear_right, 'doors'],
    ['hood', doors?.hood, 'doors'],
    ['tailgate', doors?.tailgate, 'doors'],
    ['tankLid', doors?.tank_lid, 'doors'],
    ['frontLeftWindow', windows?.front_left, 'windows'],
    ['frontRightWindow', windows?.front_right, 'windows'],
    ['rearLeftWindow', windows?.rear_left, 'windows'],
    ['rearRightWindow', windows?.rear_right, 'windows'],
    ['sunroof', windows?.sunroof, 'windows'],
  ]
  const open: OpenItem[] = []
  const told = { doors: false, windows: false }
  for (const [name, state, group] of all) {
    if (!state) continue
    told[group] = true
    if (state !== 'closed') open.push({ name, state })
  }
  return {
    locked: doors?.locked ?? null,
    open,
    doorsTold: told.doors,
    windowsTold: told.windows,
  }
}
