import { defineConfig, devices } from '@playwright/test'

// Browser tests of the interface against a Compose stack on the simulator's errand
// scenario: scripts/e2e.sh runs them in the Playwright image, in the stack's network
// (make compose-test, make e2e). Not part of make web-test: Vitest only takes test/.
//
// Every view in four combinations, a phone and a desktop, light and dark. The locale and
// the time zone are fixed: the assertions never depend on the machine's.
const desktop = { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } }
const mobile = devices['Pixel 7']
const views = [
  { name: 'desktop-light', use: { ...desktop, colorScheme: 'light' as const } },
  { name: 'desktop-dark', use: { ...desktop, colorScheme: 'dark' as const } },
  { name: 'mobile-light', use: { ...mobile, colorScheme: 'light' as const } },
  { name: 'mobile-dark', use: { ...mobile, colorScheme: 'dark' as const } },
]

export default defineConfig({
  testDir: 'e2e',
  outputDir: 'e2e-results/artifacts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  // The simulated data is deterministic: a flaky test is a bug, not a retry.
  retries: 0,
  reporter: [['list'], ['html', { outputFolder: 'e2e-results/report', open: 'never' }]],
  use: {
    baseURL: process.env.RUNSTEN_E2E_URL ?? 'http://127.0.0.1:8082',
    locale: 'en-GB',
    timezoneId: 'UTC',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  // The history imported from the simulator's Aximote comes first (setup); the four
  // projects read it in parallel; one project alone then imports and deletes it from the
  // screen, once they are done.
  projects: [
    { name: 'setup', testMatch: /\.setup\.ts$/ },
    ...views.map(({ name, use }) => ({
      name,
      use,
      dependencies: ['setup'],
      testIgnore: /imports-run\.spec\.ts$/,
    })),
    {
      name: 'imports',
      testMatch: /imports-run\.spec\.ts$/,
      use: { ...desktop, colorScheme: 'light' },
      dependencies: views.map((v) => v.name),
    },
  ],
})
