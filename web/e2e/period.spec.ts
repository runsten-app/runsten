import { expect, signIn, test, vehicleId, yesterday } from './support'

test('the period is in the URL, and survives a reload', async ({ page }) => {
  await signIn(page)
  const trips = `/vehicles/${await vehicleId(page)}/trips`
  await page.goto(trips)
  const from = yesterday()
  // The days are in the period's menu.
  await page.getByRole('button', { name: /^Period:/ }).click()
  await page.getByLabel('From').fill(from)
  await expect(page).toHaveURL(new RegExp(`${trips}\\?from=${from}$`))

  await page.reload()
  await expect(page.getByRole('button', { name: /^Period: Since / })).toBeVisible()
  await page.getByRole('button', { name: /^Period:/ }).click()
  await expect(page.getByLabel('From')).toHaveValue(from)
  await page.keyboard.press('Escape')
  await expect(page.locator('.trip-summary')).toHaveCount(2)
})

test('an inverted period is told, and never asked for', async ({ page }) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  const calls: string[] = []
  page.on('request', (r) => {
    const path = new URL(r.url()).pathname
    if (path.startsWith('/api/') && path.endsWith('/trips')) calls.push(r.url())
  })
  await page.goto(`/vehicles/${vehicle}/trips?from=2026-09-20&to=2026-09-10`)
  await expect(page.getByText('The end comes before the start.')).toBeVisible()
  await page.waitForLoadState('networkidle')
  expect(calls).toEqual([])
})
