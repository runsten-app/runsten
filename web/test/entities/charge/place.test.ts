import { describe, expect, it } from 'vitest'
import { chargeAt, type Charge } from '@/entities/charge'
import charges from '@fixtures/charges.json'

const [atHome, noPosition] = charges.items as Charge[]

describe('chargeAt', () => {
  it('tells the group of the statistics a charge counts in', () => {
    const home = atHome?.place?.id ?? ''
    const outside = { ...atHome, place: null } as Charge
    expect([atHome, outside, noPosition].map((c) => c && chargeAt(c, home))).toEqual([
      true,
      false,
      false,
    ])
    expect([atHome, outside, noPosition].map((c) => c && chargeAt(c, 'outside'))).toEqual([
      false,
      true,
      false,
    ])
    expect([atHome, outside, noPosition].map((c) => c && chargeAt(c, 'no_position'))).toEqual([
      false,
      false,
      true,
    ])
  })
})
