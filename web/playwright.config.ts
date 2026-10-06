import { defineConfig, devices } from '@playwright/test'

// Browser tests of the interface against a Compose stack on the simulator's errand
// scenario: scripts/e2e.sh runs them in the Playwright image, in the stack's network
// (make compose-test, make e2e). Not part of make web-test: Vitest only takes test/.
//
// Every view in four combinations, a phone and a desktop, light and dark. The locale and
// the time zone are fixed: the assertions never depend on the machine's.
const desktop = { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } }
const mobile = devices['Pixel 7']

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
  projects: [
    { name: 'desktop-light', use: { ...desktop, colorScheme: 'light' } },
    { name: 'desktop-dark', use: { ...desktop, colorScheme: 'dark' } },
    { name: 'mobile-light', use: { ...mobile, colorScheme: 'light' } },
    { name: 'mobile-dark', use: { ...mobile, colorScheme: 'dark' } },
  ],
})
