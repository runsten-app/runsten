import { describe, expect, it } from 'vitest'
import { versionRuns, type SoftwareVersion } from '@/entities/health'
import versions from '@fixtures/software-versions.json'

const items = versions.items as SoftwareVersion[]
const min = 60_000
const now = Date.parse('2026-10-10T05:00:00Z')

describe('versionRuns', () => {
  it('gives each version how long it ran, until the next one or now', () => {
    const [current, previous, first] = versionRuns(items, now)
    // The newest runs until now, from its installation between 04:00 and 05:00.
    expect(current?.current).toBe(true)
    expect(current?.ran).toEqual({
      min: now - Date.parse('2026-09-28T05:00:00Z'),
      max: now - Date.parse('2026-09-28T04:00:00Z'),
    })
    // Installed between 02:30 and 03:00, until the next one, between 04:00 and 05:00.
    expect(previous?.current).toBe(false)
    expect(previous?.ran).toEqual({ min: 60 * min, max: 150 * min })
    // The first read was installed at any time before 01:00: no max.
    expect(first?.ran).toEqual({ min: 90 * min, max: null })
  })

  it('bounds the installation when a reading saw it, none otherwise', () => {
    const [current, previous, first] = versionRuns(items, now)
    expect(current?.install).toEqual({
      start: { after: '2026-09-28T03:30:00Z', before: '2026-09-28T03:31:00Z' },
      end: { after: '2026-09-28T04:00:00Z', before: '2026-09-28T05:00:00Z' },
    })
    expect(previous?.install).toBeNull()
    expect(first?.install).toBeNull()
  })

  it('has nothing to give without versions', () => {
    expect(versionRuns([], now)).toEqual([])
  })
})
