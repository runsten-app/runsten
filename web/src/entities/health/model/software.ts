import { honestDuration, type Bounds } from '@/shared/lib'
import type { SoftwareVersion } from './Health'

// VersionRun is a version of the software as the history shows it: how long the vehicle
// ran it, and how long its installation took.
export type VersionRun = {
  version: SoftwareVersion
  // current: the version running, the newest.
  current: boolean
  // ran lasts from its installation to the next one's, or to now for the current; no max
  // for the first version read, installed at any time before.
  ran: { min: number; max: number | null }
  // install bounds its installation: from its start to the version's own bounds, which
  // its end lies within; null when no reading saw it installing.
  install: { start: Bounds; end: Bounds } | null
}

// versionRuns gives each version of versions (newest first, as the API lists them) how
// long the vehicle ran it, until the next one, newer, was installed, or until now.
export function versionRuns(versions: SoftwareVersion[], now: number): VersionRun[] {
  const at = new Date(now).toISOString()
  return versions.map((v, i) => {
    const next = versions[i - 1]
    const end: Bounds = next
      ? { after: next.installed.after ?? next.installed.before, before: next.installed.before }
      : { after: at, before: at }
    const { after, before } = v.installed
    const ran = after
      ? honestDuration({ after, before }, end)
      : { min: honestDuration({ after: before, before }, end).min, max: null }
    const s = v.install_start
    return {
      version: v,
      current: i === 0,
      ran,
      install: s && after ? { start: s, end: { after, before } } : null,
    }
  })
}
