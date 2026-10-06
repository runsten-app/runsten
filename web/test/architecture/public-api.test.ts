import { existsSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// Every slice of a sliced layer exposes its public API in index.ts: other layers import
// only that (enforced by ESLint's no-restricted-imports).
const src = join(import.meta.dirname, '../../src')
const sliced = ['pages', 'widgets', 'features', 'entities']

describe('slices', () => {
  const slices = sliced.flatMap((layer) =>
    existsSync(join(src, layer))
      ? readdirSync(join(src, layer), { withFileTypes: true })
          .filter((d) => d.isDirectory())
          .map((d) => `${layer}/${d.name}`)
      : [],
  )

  it('exist', () => expect(slices.length).toBeGreaterThan(0))
  it.each(slices)('%s has an index.ts', (slice) => {
    expect(existsSync(join(src, slice, 'index.ts'))).toBe(true)
  })
})
