import '@fontsource/hanken-grotesk/300.css'
import '@fontsource/hanken-grotesk/400.css'
import '@fontsource/hanken-grotesk/500.css'
import '@fontsource/hanken-grotesk/600.css'
import type { Theme } from '../types'

// Tiles: filled tiles a step lighter (or darker) than the ground, large light figures,
// one orange accent. Hanken Grotesk (OFL) has tabular figures, which every figure uses.
// Every text color reads at 4.5:1 or better on the ground, the tiles and the inset
// strips; every chart series at 3:1 (test/app/theme.test.ts).
const stack = "'Hanken Grotesk', system-ui, sans-serif"

export const tiles: Theme = {
  id: 'tiles',
  name: 'Tiles',
  font: { body: stack, heading: stack },
  radius: { tile: '14px', tilePhone: '12px', control: '10px' },
  light: {
    background: '#EEF0F2',
    surface: '#FFFFFF',
    'surface-variant': '#F2F3F5',
    'on-surface': '#16171A',
    'text-secondary': '#565A61',
    primary: '#B34E0B',
    'on-primary': '#FFFFFF',
    secondary: '#3E5A8E',
    success: '#28724A',
    warning: '#8F5B00',
    error: '#B0304A',
    info: '#2D6597',
    'success-container': '#E3F2E8',
    'warning-container': '#FBEFD9',
    'error-container': '#F9E4E8',
    line: '#DDE0E4',
    track: '#E4E6E9',
    'chart-1': '#B34E0B',
    'chart-2': '#3E5A8E',
    'chart-3': '#565A61',
    'chart-4': '#C4692C',
    'chart-band': '#F3D9C8',
    'chart-grid': '#E6E8EB',
    boundary: '#9A9DA3',
  },
  dark: {
    background: '#0C0D0F',
    surface: '#1A1B1E',
    'surface-variant': '#25262A',
    'on-surface': '#F2F2F0',
    'text-secondary': '#A3A6AC',
    primary: '#FF8A3D',
    'on-primary': '#0C0D0F',
    secondary: '#8FA8D8',
    success: '#5CC98A',
    warning: '#F0B340',
    error: '#FF6B6B',
    info: '#6FA8E8',
    'success-container': '#173322',
    'warning-container': '#35290F',
    'error-container': '#3A1A1E',
    line: '#2A2C30',
    track: '#2E3034',
    'chart-1': '#FF8A3D',
    'chart-2': '#8FA8D8',
    'chart-3': '#A3A6AC',
    'chart-4': '#AE5F2A',
    'chart-band': '#4A2E1C',
    'chart-grid': '#26282C',
    boundary: '#5E6168',
  },
}
