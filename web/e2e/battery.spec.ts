import { checkView, expect, signIn, test, vehicleId } from './support'

// A full history the errand scenario does not have: an intercepted answer, as the API
// would give it (the errand's own page is read for real, above). The Playwright image
// runs with web/ alone mounted: the answer is written here, not read from a file.
const full = {
  time_zone: 'UTC',
  reference: { capacity_kwh: 75, source: 'catalog_net' },
  current: { capacity_kwh: 73.5, q1_kwh: 73, q3_kwh: 73.75, estimates: 20 },
  deviation_pct: -2,
  change: { since: '2025-08-05T12:00:00Z', initial_kwh: 75, change_pct: -2 },
  cycles: 14.4,
  estimates: [
    {
      charge: '2025-08-05T11:59:00.000000Z',
      at: '2025-08-05T12:00:00Z',
      odometer_km: 20040,
      source: 'power',
      type: 'AC',
      span_soc: 50,
      capacity_kwh: 76,
    },
    {
      charge: '2025-08-12T11:59:00.000000Z',
      at: '2025-08-12T12:00:00Z',
      odometer_km: null,
      source: 'power',
      type: 'AC',
      span_soc: 25,
      capacity_kwh: 76,
    },
    {
      charge: '2025-08-20T11:59:00.000000Z',
      at: '2025-08-20T12:00:00Z',
      odometer_km: 20160,
      source: 'power',
      type: 'DC',
      span_soc: 25,
      capacity_kwh: 76,
    },
    {
      charge: '2025-08-20T11:59:00.000000Z',
      at: '2025-08-20T12:00:00Z',
      odometer_km: 20160,
      source: 'billed',
      type: 'DC',
      span_soc: 25,
      capacity_kwh: 77,
    },
    {
      charge: '2025-09-06T11:59:00.000000Z',
      at: '2025-09-06T12:00:00Z',
      odometer_km: 20548,
      source: 'power',
      type: 'AC',
      span_soc: 40,
      capacity_kwh: 75.75,
    },
    {
      charge: '2025-09-13T11:59:00.000000Z',
      at: '2025-09-13T12:00:00Z',
      odometer_km: 20604,
      source: 'power',
      type: 'AC',
      span_soc: 50,
      capacity_kwh: 75.75,
    },
  ],
  excluded: {
    reconstructed: 1,
    no_power: 0,
    high_soc: 0,
    span_soc: 1,
    power_gap: 0,
    low_power: 0,
    implausible: 0,
  },
  months: [
    {
      start: '2025-08-01T00:00:00Z',
      end: '2025-09-01T00:00:00Z',
      capacity: { capacity_kwh: 76, q1_kwh: 76, q3_kwh: 76.25, estimates: 4 },
      range_at_full: { median_km: 402, readings: 2 },
    },
    {
      start: '2025-09-01T00:00:00Z',
      end: '2025-10-01T00:00:00Z',
      capacity: { capacity_kwh: 75.75, q1_kwh: 75.75, q3_kwh: 75.75, estimates: 2 },
      range_at_full: { median_km: 402, readings: 2 },
    },
    {
      start: '2025-10-01T00:00:00Z',
      end: '2025-11-01T00:00:00Z',
      capacity: null,
      range_at_full: null,
    },
  ],
}

// The errand's charge gives no estimate in Compose, whose collector runs in real time (the
// energy from the power is 60 times too small): the 5 the aggregate waits for are all still
// needed. The page says so, without promising a measurement, and shows the charts all the
// same.
test('Battery: the errand so far, without promising a measurement', async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto(`/vehicles/${vehicle}/battery`)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Battery')
  await expect(page.locator('.tile.capacity .value')).toHaveText('Not enough charges yet')
  await expect(page.locator('.tile.capacity .note')).toHaveText('5 more usable charges needed')
  await expect(page.locator('.v-tab[aria-current="page"]')).toHaveText('Battery')
  await expect(page.locator('.chart-frame')).toHaveCount(2)
  await checkView(page, testInfo, 'battery')
})

// A press of the axis toggle changes the URL's query only: the focus stays on the button,
// and the summary tells what the mileage axis leaves out.
test('Battery: the capacity chart by date or by mileage', async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.route(`**/api/v1/vehicles/${vehicle}/battery*`, (route) =>
    route.fulfill({ json: full }),
  )
  await page.goto(`/vehicles/${vehicle}/battery`)
  await expect(page.locator('.chart-frame.capacity')).toBeVisible()
  const group = page.getByRole('group', { name: 'Axis' })
  await expect(group.getByRole('button', { name: 'Date' })).toHaveAttribute('aria-pressed', 'true')
  // Intl puts a no-break space between a number and its unit, as on every view.
  await expect(page.locator('.capacity canvas')).toHaveAttribute(
    'aria-label',
    'Estimated capacity from 76\u00a0kWh in Aug 2025 to 75.8\u00a0kWh in Sept 2025.',
  )

  const mileage = group.getByRole('button', { name: 'Mileage' })
  await mileage.click()
  await expect(page).toHaveURL(/\/battery\?axis=odometer$/)
  await expect(mileage).toHaveAttribute('aria-pressed', 'true')
  await expect(mileage).toBeFocused()
  await expect(page.locator('.capacity canvas')).toHaveAttribute(
    'aria-label',
    'Estimated capacity from 76\u00a0kWh at 20,040 km to 75.8\u00a0kWh at 20,604 km. 1 estimate without an odometer is not shown.',
  )
  await checkView(page, testInfo, 'battery-axis')
})

test('the state tells the estimated capacity, and leads to the page', async ({
  page,
}, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto(`/vehicles/${vehicle}`)
  await expect(page.locator('.estimated-capacity')).toContainText('Not enough charges yet')
  await checkView(page, testInfo, 'state-battery')
  await page.getByRole('link', { name: 'See the battery page' }).click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/battery$`))
})

test('the vehicle tabs lead to the battery', async ({ page }) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto(`/vehicles/${vehicle}`)
  await page
    .getByRole('navigation', { name: 'Vehicle' })
    .getByRole('link', { name: 'Battery' })
    .click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/battery$`))
})
