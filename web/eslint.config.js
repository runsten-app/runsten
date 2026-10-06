import js from '@eslint/js'
import vueI18n from '@intlify/eslint-plugin-vue-i18n'
import prettier from 'eslint-config-prettier'
import vue from 'eslint-plugin-vue'
import globals from 'globals'
import ts from 'typescript-eslint'

// Feature-Sliced Design: app → pages → widgets → features → entities → shared, imports
// only go down, and only through a slice's public API (its index.ts). Inside a slice,
// segments import each other relatively (../model/Trip); leaving it takes the @/ alias.
// A flat config does not merge the options of one rule across blocks: each layer repeats
// the common patterns.
const layers = ['app', 'pages', 'widgets', 'features', 'entities']
const common = [
  {
    group: ['../../*', '**/../../*'],
    message: 'Leave a slice through the @/ alias, not a relative import.',
  },
  {
    group: layers.filter((l) => l !== 'app').map((l) => `@/${l}/*/*`),
    message: 'Import a slice through its public API (@/<layer>/<slice>), not its internals.',
  },
]
const deny = (forbidden, message) => ({ group: forbidden, message })
const boundaries = (files, ...patterns) => ({
  files,
  rules: { 'no-restricted-imports': ['error', { patterns: [...common, ...patterns] }] },
})
// Tests may reach inside a slice (its model, its messages), never up with ../../.
const tests = {
  files: ['test/**'],
  rules: { 'no-restricted-imports': ['error', { patterns: [common[0]] }] },
}
const query = deny(['@tanstack/vue-query'], 'TanStack Query lives in features/*/composables only.')

export default ts.config(
  { ignores: ['dist/', 'coverage/', 'e2e-results/', 'src/shared/api/schema.d.ts'] },
  js.configs.recommended,
  ts.configs.strict,
  vue.configs['flat/recommended'],
  {
    files: ['**/*.vue'],
    languageOptions: { parserOptions: { parser: ts.parser } },
  },
  {
    languageOptions: { globals: { ...globals.browser } },
  },
  // The browser tests and their configuration run in Node (Playwright's runner).
  {
    files: ['e2e/**', 'playwright.config.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
  ...vueI18n.configs.recommended,
  {
    settings: {
      'vue-i18n': {
        localeDir: './src/shared/i18n/locales/*.json',
        messageSyntaxVersion: '^11.0.0',
      },
    },
    rules: {
      // Every visible text goes through vue-i18n: English, French and Swedish.
      '@intlify/vue-i18n/no-raw-text': [
        'error',
        { ignorePattern: '^[-–—·:/()%\\s\\d.,]+$', ignoreText: ['Runsten'] },
      ],
      '@intlify/vue-i18n/no-missing-keys': 'error',
      '@intlify/vue-i18n/no-unused-keys': ['error', { extensions: ['.ts', '.vue'] }],
    },
  },
  boundaries(['src/app/**']),
  tests,
  boundaries(
    ['src/pages/**'],
    deny(['@/app', '@/app/**'], 'Pages do not import app.'),
    query,
    deny(['@/entities/*/api', '@/entities/*/api/**'], 'Pages go through features for data.'),
  ),
  boundaries(
    ['src/widgets/**'],
    deny(
      ['@/app', '@/app/**', '@/pages', '@/pages/**'],
      'Widgets import features, entities and shared only.',
    ),
  ),
  boundaries(
    ['src/features/**'],
    deny(
      ['@/app', '@/app/**', '@/pages', '@/pages/**', '@/widgets', '@/widgets/**'],
      'Features import entities and shared only.',
    ),
  ),
  boundaries(
    ['src/entities/**'],
    deny(
      [
        '@/app',
        '@/app/**',
        '@/pages',
        '@/pages/**',
        '@/widgets',
        '@/widgets/**',
        '@/features',
        '@/features/**',
      ],
      'Entities import shared only.',
    ),
    query,
  ),
  boundaries(
    ['src/shared/**'],
    deny(
      layers.flatMap((l) => [`@/${l}`, `@/${l}/**`]),
      'Shared imports no business layer.',
    ),
    query,
  ),
  prettier,
)
