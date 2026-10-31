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
// locked (null when it does not say), what is open or ajar, and whether it told any
// opening at all (else "all closed" would mean nothing).
export type Openings = { locked: boolean | null; open: OpenItem[]; told: boolean }

// openings sums up the doors and windows; an opening the vehicle does not tell is left
// out, never taken as closed or open.
export function openings(doors: Doors | null, windows: Windows | null): Openings {
  const all: [OpeningName, Opening | null | undefined][] = [
    ['frontLeftDoor', doors?.front_left],
    ['frontRightDoor', doors?.front_right],
    ['rearLeftDoor', doors?.rear_left],
    ['rearRightDoor', doors?.rear_right],
    ['hood', doors?.hood],
    ['tailgate', doors?.tailgate],
    ['tankLid', doors?.tank_lid],
    ['frontLeftWindow', windows?.front_left],
    ['frontRightWindow', windows?.front_right],
    ['rearLeftWindow', windows?.rear_left],
    ['rearRightWindow', windows?.rear_right],
    ['sunroof', windows?.sunroof],
  ]
  const open: OpenItem[] = []
  let told = false
  for (const [name, state] of all) {
    if (!state) continue
    told = true
    if (state !== 'closed') open.push({ name, state })
  }
  return { locked: doors?.locked ?? null, open, told }
}
