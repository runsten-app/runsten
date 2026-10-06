import { checkView, expect, setEuro, signIn, test, vehicleId } from './support'

test('Stats: the totals and the charts of this month, each chart with its table', async ({
  page,
}, testInfo) => {
  await signIn(page)
  // With a currency, the costs' view has its charts.
  await setEuro(page)
  await page.goto(`/vehicles/${await vehicleId(page)}/stats`)
  // This month, by day, unless the URL says otherwise.
  await expect(page).toHaveURL(/\/stats\?from=\d{4}-\d{2}-01&to=\d{4}-\d{2}-\d{2}$/)
  await expect(page.getByRole('link', { name: 'Stats' })).toHaveAttribute('aria-current', 'page')
  // The period in one line: this month by name, with the months around it.
  const month = new Intl.DateTimeFormat('en-GB', { month: 'long', year: 'numeric' }).format(
    new Date(),
  )
  await expect(page.getByRole('button', { name: `Period: ${month}` })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Previous period' })).toBeVisible()

  // The errand: the way there, reconstructed, and back; a DC charge.
  await expect(page.locator('.tile.trips .value')).toHaveText('2')
  await expect(page.locator('.tile.trips .note')).toHaveText('including 1 reconstructed')
  await expect(page.locator('.tile.distance .value')).toHaveText(/^1\d(\.\d)?\s?km$/)
  await expect(page.locator('.tile.charges .value')).toHaveText('1')
  await expect(page.locator('.tile.charges .note').first()).toHaveText('0 AC, 1 DC')

  const distance = page.locator('.chart-frame.distance')
  await expect(distance.getByRole('img')).toHaveAttribute('aria-label', /^Distance: .+ in all/)
  // Its figures, for screen readers: one row per day of the month.
  await expect(distance.locator('tbody tr')).not.toHaveCount(0)
  // The driving's view by default: distance, consumption, time, and the trips by
  // distance with what each band consumed.
  await expect(page.locator('.chart-frame')).toHaveCount(5)
  await expect(page.locator('.tile.cost')).toBeVisible()
  await checkView(page, testInfo, 'stats')

  // The other views, in the URL; the focus stays on the view pressed.
  const charging = page.getByRole('button', { name: 'Charging', exact: true })
  await charging.click()
  await expect(page).toHaveURL(/view=charging/)
  await expect(charging).toBeFocused()
  await expect(page.locator('.chart-frame')).toHaveCount(4)
  await checkView(page, testInfo, 'stats-charging')
  await page.getByRole('button', { name: 'Costs', exact: true }).click()
  await expect(page).toHaveURL(/view=costs/)
  await expect(page.locator('.chart-frame')).toHaveCount(3)
  await checkView(page, testInfo, 'stats-costs')
  await page.getByRole('button', { name: 'Driving', exact: true }).click()
  await expect(page).toHaveURL(/view=driving/)

  // The focus stays on the control pressed: only the URL's query changes.
  const week = page.getByRole('button', { name: 'By week' })
  await week.click()
  await expect(page).toHaveURL(/bucket=week/)
  await expect(week).toHaveAttribute('aria-pressed', 'true')
  await expect(week).toBeFocused()
  await expect(distance.locator('thead th').first()).toHaveText('Week from')

  // An interval leads to its events: the whole column answers, so the canvas's middle
  // is an interval of the chart.
  await distance.getByRole('img').click()
  await expect(page).toHaveURL(/\/trips\?from=\d{4}-\d{2}-\d{2}&to=\d{4}-\d{2}-\d{2}$/)
  await expect(page.getByRole('link', { name: 'Trips' })).toHaveAttribute('aria-current', 'page')
})
