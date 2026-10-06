import { checkView, expect, signIn, test, vehicleId, yesterday } from './support'

// A time, or the two bounds of an instant ("10:05–10:06"): the collector's clock is real
// time, so the values themselves are never asserted.
const bounds = String.raw`\d{2}:\d{2}(\s?–\s?\d{2}:\d{2})?`

test('Urd: a trip seen driving, its details, and back to the list', async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  const from = yesterday()
  await page.goto(`/vehicles/${vehicle}/trips?from=${from}`)
  await expect(page.getByRole('link', { name: 'Trips' })).toHaveAttribute('aria-current', 'page')

  const seen = page.locator('.trip-summary').filter({ hasNot: page.locator('.reconstructed') })
  await expect(seen).toHaveCount(1)
  await expect(seen.locator('.event-when')).toHaveText(new RegExp(`^${bounds} → ${bounds}$`))
  // Distance, then the duration: a range of minutes.
  await expect(seen.locator('.figures')).toHaveText(/^1\d(\.\d)?\s?km · [\d–]+\s?mins?\b/)
  await checkView(page, testInfo, 'trips')

  await seen.click()
  await expect(page).toHaveURL(new RegExp(`/trips/[^/?]+\\?from=${from}$`))
  const when = page.locator('.when')
  await expect(when).toBeVisible()
  await expect(when.getByText('Duration', { exact: true })).toBeVisible()
  await expect(page.locator('.reconstructed-note')).toHaveCount(0)
  await checkView(page, testInfo, 'trip')

  await page.locator('a.back').click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/trips\\?from=${from}$`))
  await expect(page.getByRole('button', { name: /^Period: Since / })).toBeVisible()
})

test('Urd: the reconstructed way back, in the list and in its details', async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}/trips`)

  const reconstructed = page
    .locator('.trip-summary')
    .filter({ has: page.locator('.reconstructed') })
  await expect(reconstructed).toHaveCount(1)
  await expect(reconstructed.locator('.event-when')).toHaveText(
    /^Happened between \d{2}:\d{2} and \d{2}:\d{2}$/,
  )
  await expect(reconstructed.locator('.reconstructed')).toContainText('Reconstructed')
  // No duration: it lies somewhere in its interval.
  await expect(reconstructed.locator('.figures')).not.toContainText('min')

  await reconstructed.click()
  await expect(page).toHaveURL(/\/trips\/[^/?]+$/)
  const note = page.locator('.reconstructed-note')
  await expect(note).toBeVisible()
  await expect(note.locator('.event-when')).toHaveText(/^Happened between /)
  await expect(page.locator('.when')).toHaveCount(0)
  await checkView(page, testInfo, 'trip-reconstructed')
})
