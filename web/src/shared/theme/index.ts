import { tiles } from './themes/tiles'
import type { Palette, Theme } from './types'

export {
  appearances,
  isAppearance,
  storeAppearance,
  storedAppearance,
  type Appearance,
} from './appearance'
export type { Palette, Theme } from './types'

// The themes of the interface. A new one is a file of themes/ added here: its palettes,
// its typeface and its radii; nothing else changes.
export const themes: Record<string, Theme> = { [tiles.id]: tiles }
export const defaultTheme = tiles

// VuetifyTheme is what createVuetify takes for one color scheme.
export type VuetifyTheme = {
  dark: boolean
  colors: Record<string, string>
  variables: Record<string, string | number>
}

// vuetifyTheme turns a theme's scheme into Vuetify's colors and CSS variables. Vuetify
// picks between the themes named light and dark by the system's scheme: the theme in use
// takes both names, and choosing another replaces them.
export function vuetifyTheme(theme: Theme, scheme: 'light' | 'dark'): VuetifyTheme {
  const p: Palette = theme[scheme]
  return {
    dark: scheme === 'dark',
    colors: {
      ...p,
      'on-background': p['on-surface'],
      'on-surface-variant': p['on-surface'],
      // Vuetify's own light surface (a slider's track, a toggle's ground).
      'surface-light': p['surface-variant'],
      'on-surface-light': p['on-surface'],
    },
    variables: {
      // Vuetify reads the typeface from these.
      'font-body': theme.font.body,
      'font-heading': theme.font.heading,
      'tile-radius': theme.radius.tile,
      'tile-radius-phone': theme.radius.tilePhone,
      'control-radius': theme.radius.control,
      // Rules are the line color, whole; secondary text is text-secondary, never the text
      // color faded (app/styles/base.css).
      'border-color': p.line,
      'border-opacity': 1,
      'high-emphasis-opacity': 1,
      'medium-emphasis-opacity': 1,
      'disabled-opacity': 0.45,
    },
  }
}
