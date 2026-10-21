import { describe, expect, it } from 'vitest'
import { useCollectionText } from '@/features/watch-collection'
import { withSetup } from '@test/utils'

describe('useCollectionText', () => {
  it("words a failure by the vehicle's vendor", () => {
    const { result: text } = withSetup(useCollectionText)
    expect(text.failure('unavailable')).toBe('Volvo unreachable')
    expect(text.failure('unavailable', 'polestar')).toBe('Polestar unreachable')
    expect(text.failure('token', 'polestar')).toBe('No token from Polestar')
    expect(text.failure('not_found', 'polestar')).toBe('Vehicle unknown to Polestar')
    expect(text.failure('credential_refused', 'polestar')).toBe('Polestar credential refused')
    expect(text.failure('unshared', 'polestar')).toBe('Vehicle no longer shared')
    expect(text.failure('not_granted', 'polestar')).toBe('Domain not granted')
  })

  it("names the Data Portal's API", () => {
    const { result: text } = withSetup(useCollectionText)
    expect(text.api('polestar')).toBe('Polestar Data Portal')
    expect(text.api('elsewhere')).toBe('elsewhere')
  })
})
