import { checkView, expect, signIn, test, vehicleId } from './support'

// The health of a Polestar that gives its pressures, which the errand's Volvo does not:
// intercepted answers, as the API would give them. The Playwright image runs with web/
// alone mounted: the answers are written here, not read from a file.
const at = '2026-09-28T18:00:00Z'
const wheel = (w: string, kPa: number, warning: string) => ({
  wheel: w,
  pressure_kpa: kPa,
  warning,
  reported_at: at,
})
const polestar = {
  checked_at: '2026-09-28T19:00:00Z',
  tyres: {
    wheels: [
      wheel('front_left', 282, 'ok'),
      wheel('front_right', 280, 'ok'),
      wheel('rear_left', 248, 'low'),
      wheel('rear_right', 279, 'ok'),
    ],
    recommended_front_kpa: 280,
    recommended_rear_kpa: 280,
  },
  service: { state: 'ok', days: 300, months: null, distance_km: 4150, reported_at: at },
  checks: [
    { check: 'brake_fluid', warning: false, reported_at: at },
    { check: 'washer_fluid', warning: true, reported_at: at },
  ],
}
const reading = (iso: string, rearLeft: number) => ({
  at: iso,
  front_left_kpa: 282,
  front_right_kpa: 280,
  rear_left_kpa: rearLeft,
  rear_right_kpa: 279,
  recommended_front_kpa: 280,
  recommended_rear_kpa: 280,
})

// The errand's Volvo: a warning per tyre, none, and no pressure, so no chart.
test('Health: the Volvo’s tyres, service and warnings', async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto(`/vehicles/${vehicle}`)
  await page
    .getByRole('navigation', { name: 'Vehicle' })
    .getByRole('link', { name: 'Health' })
    .click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/health$`))
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Health')
  await expect(page.locator('[data-test="tyres"]')).toContainText(
    'a warning per tyre, not its pressure',
  )
  await expect(page.locator('[data-test="warnings"]')).toContainText('No warning')
  await expect(page.locator('.chart-frame')).toHaveCount(0)
  // Nothing needs attention: the tab has no dot.
  const tab = page
    .getByRole('navigation', { name: 'Vehicle' })
    .getByRole('link', { name: 'Health' })
  await expect(tab).toHaveAttribute('aria-current', 'page')
  await expect(tab.locator('.tab-dot')).toHaveCount(0)
  await expect(page.locator('.battery-summary')).toContainText('Not enough charges yet')
  await checkView(page, testInfo, 'health')
})

// The pressures over time, each axle against the pressure recommended; another window is
// asked when chosen, the focus kept on its button.
test('Health: the pressures of a Polestar over time', async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.route(`**/api/v1/vehicles/${vehicle}/health`, (route) =>
    route.fulfill({ json: { vehicle_id: vehicle, ...polestar } }),
  )
  await page.route(`**/api/v1/vehicles/${vehicle}/health/tyres*`, (route) => {
    const url = new URL(route.request().url())
    const to = url.searchParams.get('to') ?? new Date().toISOString()
    const end = Date.parse(to)
    const iso = (h: number) => new Date(end - h * 3600_000).toISOString()
    route.fulfill({
      json: {
        from: url.searchParams.get('from'),
        to,
        runs: [{ readings: [reading(iso(48), 262), reading(iso(24), 255), reading(iso(1), 248)] }],
      },
    })
  })
  await page.goto(`/vehicles/${vehicle}/health`)
  await expect(page.locator('[data-test="tyres"] .wheel').nth(2)).toContainText('2.48')
  await expect(page.locator('[data-test="warnings"] li')).toHaveText(['Washer fluid'])
  await expect(page.locator('.chart-frame')).toHaveCount(2)
  // A low tyre and a warning: the tab says the vehicle needs attention, its name unchanged.
  const tab = page
    .getByRole('navigation', { name: 'Vehicle' })
    .getByRole('link', { name: 'Health' })
  await expect(tab).toHaveAccessibleDescription('Needs attention')
  const group = page.getByRole('group', { name: 'Period' })
  const year = group.getByRole('button', { name: '12 months' })
  await year.click()
  await expect(year).toHaveAttribute('aria-pressed', 'true')
  await expect(year).toBeFocused()
  await checkView(page, testInfo, 'health-pressures')
})
