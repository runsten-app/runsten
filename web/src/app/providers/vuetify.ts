import { useI18n } from 'vue-i18n'
import { createVuetify } from 'vuetify'
import { createVueI18nAdapter } from 'vuetify/locale/adapters/vue-i18n'
import { aliases, mdi } from 'vuetify/iconsets/mdi-svg'
import 'vuetify/styles'
import '../styles/base.css'
import { i18n } from '@/shared/i18n'
import { defaultTheme, storedAppearance, vuetifyTheme } from '@/shared/theme'

// Vuetify writes its theme into a <style> element; the CSP of runsten-web only allows it
// with the nonce it put in this meta. Empty in development (no CSP).
type AdapterI18n = Parameters<typeof createVueI18nAdapter>[0]['i18n']

const nonce =
  document.querySelector<HTMLMetaElement>('meta[name="csp-nonce"]')?.content || undefined

// Tiles are filled surfaces, never raised nor outlined: no elevation anywhere (the radii
// are the theme's, in app/styles/base.css). Fields are filled, their label floating
// inside; on a tile they take the inset color, a step apart from it.
const field = { variant: 'filled', flat: true, bgColor: 'surface' } as const
const defaults = {
  global: { elevation: 0 },
  VCard: {
    flat: true,
    VTextField: { bgColor: 'surface-variant' },
    VSelect: { bgColor: 'surface-variant' },
    VAutocomplete: { bgColor: 'surface-variant' },
    VTextarea: { bgColor: 'surface-variant' },
  },
  VSheet: { elevation: 0 },
  VChip: { rounded: 'pill' },
  VTextField: field,
  VSelect: field,
  VAutocomplete: field,
  VTextarea: field,
  VMenu: { VList: { bgColor: 'surface' } },
  VDivider: { opacity: 1 },
}

export const vuetify = createVuetify({
  theme: {
    // The reader's choice (Preferences), else the system's scheme.
    defaultTheme: storedAppearance(),
    cspNonce: nonce,
    themes: {
      light: vuetifyTheme(defaultTheme, 'light'),
      dark: vuetifyTheme(defaultTheme, 'dark'),
    },
  },
  defaults,
  icons: { defaultSet: 'mdi', aliases, sets: { mdi } },
  // The adapter takes any I18n; ours is narrowed to its three locales and typed messages.
  locale: { adapter: createVueI18nAdapter({ i18n: i18n as unknown as AdapterI18n, useI18n }) },
})
