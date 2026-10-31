import { describe, expect, it } from 'vitest'
import { openings, type Doors, type Windows } from '@/entities/vehicle'

const doors: Doors = {
  locked: true,
  front_left: 'closed',
  front_right: 'closed',
  rear_left: 'closed',
  rear_right: 'closed',
  hood: 'closed',
  tailgate: 'closed',
  tank_lid: null,
  sentry: null,
}
const windows: Windows = {
  front_left: 'closed',
  front_right: 'closed',
  rear_left: 'closed',
  rear_right: 'closed',
  sunroof: null,
}

describe('openings', () => {
  it('names what is open or ajar, doors first, and leaves out what is not told', () => {
    expect(openings(doors, windows)).toEqual({
      locked: true,
      open: [],
      doorsTold: true,
      windowsTold: true,
    })
    expect(
      openings({ ...doors, locked: false, tailgate: 'open' }, { ...windows, sunroof: 'ajar' }),
    ).toEqual({
      locked: false,
      open: [
        { name: 'tailgate', state: 'open' },
        { name: 'sunroof', state: 'ajar' },
      ],
      doorsTold: true,
      windowsTold: true,
    })
  })

  it('tells nothing of what the vehicle does not tell', () => {
    expect(openings(null, null)).toEqual({
      locked: null,
      open: [],
      doorsTold: false,
      windowsTold: false,
    })
    // The windows alone: nothing told of the doors.
    expect(openings(null, windows)).toMatchObject({ doorsTold: false, windowsTold: true })
    // The lock alone: nothing told of the openings, so nothing is "all closed".
    const lockOnly: Doors = {
      ...doors,
      front_left: null,
      front_right: null,
      rear_left: null,
      rear_right: null,
      hood: null,
      tailgate: null,
    }
    expect(openings(lockOnly, null)).toEqual({
      locked: true,
      open: [],
      doorsTold: false,
      windowsTold: false,
    })
  })
})
