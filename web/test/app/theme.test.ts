import { describe, expect, it } from 'vitest'
import { vuetify } from '@/app/providers/vuetify'
import { themes, type Palette } from '@/shared/theme'

// Every theme reads at WCAG AA in both schemes: axe checks the pages, but not what a
// canvas draws, nor the schemes and themes the tests do not show.
function luminance(hex: string): number {
  const h = hex.replace('#', '')
  const [r, g, b] = [0, 2, 4].map((i) => {
    const c = parseInt(h.slice(i, i + 2), 16) / 255
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * (r ?? 0) + 0.7152 * (g ?? 0) + 0.0722 * (b ?? 0)
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return ((hi ?? 0) + 0.05) / ((lo ?? 0) + 0.05)
}

const schemes = Object.values(themes).flatMap((theme) =>
  (['light', 'dark'] as const).map((scheme) => ({
    name: `${theme.id} ${scheme}`,
    p: theme[scheme],
  })),
)

function check(p: Palette, pairs: [keyof Palette, keyof Palette][], min: number) {
  for (const [fg, bg] of pairs) {
    const ratio = contrast(p[fg], p[bg])
    expect(ratio, `${fg} on ${bg}: ${ratio.toFixed(2)}`).toBeGreaterThanOrEqual(min)
  }
}

const grounds = ['background', 'surface', 'surface-variant'] as const

describe.each(schemes)('$name', ({ p }) => {
  // Text, secondary text included, and links (the accent) at 4.5:1 on every ground.
  it('reads its text at 4.5:1', () => {
    const texts = [
      'on-surface',
      'text-secondary',
      'primary',
      'success',
      'warning',
      'error',
      'info',
    ] as const
    check(
      p,
      texts.flatMap((t) => grounds.map((g) => [t, g] as [keyof Palette, keyof Palette])),
      4.5,
    )
    check(
      p,
      [
        ['on-primary', 'primary'],
        ['success', 'success-container'],
        ['warning', 'warning-container'],
        ['error', 'error-container'],
        ['background', 'on-surface'],
      ],
      4.5,
    )
  })

  // WCAG 1.4.11: a series of a chart at 3:1 against what it is drawn on.
  it('draws its series at 3:1', () => {
    const series = ['chart-1', 'chart-2', 'chart-3', 'chart-4'] as const
    check(
      p,
      series.flatMap((s) =>
        (['background', 'surface'] as const).map((g) => [s, g] as [keyof Palette, keyof Palette]),
      ),
      3,
    )
  })
})

// The theme in use is Vuetify's light and dark: what the system picks between.
describe('Vuetify', () => {
  it('has the default theme in both schemes', () => {
    const t = vuetify.theme.themes.value
    expect(t.light?.colors.primary).toBe(themes.tiles?.light.primary)
    expect(t.dark?.colors.primary).toBe(themes.tiles?.dark.primary)
    expect(t.dark?.dark).toBe(true)
    expect(t.light?.variables['font-body']).toContain('Hanken Grotesk')
  })
})
