// A theme is a palette for each color scheme, a typeface and a few measures: everything
// the interface draws with. Components never name a color, a font or a radius: they read
// these tokens (rgb(var(--v-theme-…)), var(--v-…)), so that a new theme is a new file.

// Palette holds the colors of one scheme, light or dark, as #rrggbb.
export type Palette = {
  // The page's ground, the tiles on it, and the inset strips and pressed segments of a
  // tile: a step of color apart, never a border or a shadow.
  background: string
  surface: string
  'surface-variant': string
  // Text and figures; text-secondary for labels, units and notes (never an opacity).
  'on-surface': string
  'text-secondary': string
  // The one accent, and what reads on it.
  primary: string
  'on-primary': string
  secondary: string
  success: string
  warning: string
  error: string
  info: string
  // Status pills: a container fill under the status's own color.
  'success-container': string
  'warning-container': string
  'error-container': string
  // Rules between rows and under the bar; the track of a bar or a gauge.
  line: string
  track: string
  // The series of the charts, 3:1 at least against the ground and the tiles: chart-4 is
  // the uncertainty of chart-1, the same hue fainter, drawn as the edge of a band whose
  // fill is chart-band. chart-grid for the horizontal rules, boundary for a measured zero.
  'chart-1': string
  'chart-2': string
  'chart-3': string
  'chart-4': string
  'chart-band': string
  'chart-grid': string
  boundary: string
}

export type Theme = {
  // Kept in the browser once a theme can be chosen: never renamed.
  id: string
  name: string
  // CSS font stacks. The theme's file imports its typeface's @font-face rules, bundled
  // (the CSP allows fonts from the origin only): a browser fetches the font files of the
  // theme in use only.
  font: { body: string; heading: string }
  // Corner radii: tiles, and controls (buttons, fields, chips, inset strips).
  radius: { tile: string; tilePhone: string; control: string }
  light: Palette
  dark: Palette
}
