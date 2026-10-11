import { checkView, expect, signIn, test, vehicleId } from './support'

// The imports of scripts/e2e-imports.sql, written before the browser tests as an importer
// would: a trip and a charge of Aximote, a month before the first reading, and a trip
// of each project's own source, which that project deletes.

test('Imports: an imported trip, marked with its app, in the list and in its details', async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}/trips`)

  const imported = page
    .locator('.trip-summary')
    .filter({ has: page.getByText('Imported from Aximote', { exact: true }) })
  await expect(imported).toHaveCount(1)
  // Its times are the app's: one instant at each end, never a range.
  await expect(imported.locator('.event-when')).toHaveText(/^\d{2}:\d{2} → \d{2}:\d{2}$/)
  await expect(imported.locator('.route')).toContainText('Place Bellecour, Lyon → Part-Dieu, Lyon')
  await checkView(page, testInfo, 'trips-imported')

  await imported.click()
  await expect(page).toHaveURL(/\/trips\/aximote:e2e-trip$/)
  const note = page.locator('.imported-note')
  await expect(note).toContainText('Imported from Aximote')
  await expect(page.locator('.when')).toContainText('no range')
  await checkView(page, testInfo, 'trip-imported')
})

test('Imports: an imported charge, its average power, no cost to enter', async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}/charges`)
  const imported = page.locator('.charge-summary').filter({ has: page.locator('.imported') })
  await expect(imported).toHaveCount(1)
  await expect(imported.locator('.imported')).toHaveText('Imported from Aximote')

  await imported.click()
  await expect(page).toHaveURL(/\/charges\/aximote:e2e-charge$/)
  await expect(page.locator('.imported-note')).toBeVisible()
  await expect(page.locator('.energies')).toContainText(/Average power\s*6\.5\s?kW/)
  await expect(page.getByRole('button', { name: 'Enter the cost' })).toHaveCount(0)
  await checkView(page, testInfo, 'charge-imported')
})

test("Imports: a vehicle's settings list them, and delete one once confirmed", async ({
  page,
}, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  const own = `e2e_${testInfo.project.name.replace('-', '_')}`
  await page.goto(`/vehicles/${vehicle}/settings`)

  const section = page.getByRole('region', { name: 'Imported history' })
  const aximote = section.getByRole('listitem').filter({ hasText: 'Imported from Aximote' })
  await expect(aximote).toContainText('1 trip · 1 charge')
  const mine = section.getByRole('listitem').filter({ hasText: `Imported from ${own}` })
  await expect(mine).toContainText('1 trip · no charge')
  await checkView(page, testInfo, 'vehicle-settings-imports')

  await mine.getByRole('button', { name: `Imported from ${own}: delete` }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete this import?' })
  await expect(dialog).toBeVisible()
  await checkView(page, testInfo, 'import-delete')
  await dialog.getByRole('button', { name: 'Delete' }).click()
  await expect(section.locator('[role="status"]')).toHaveText(`Imported from ${own}: deleted.`)
  await expect(section.getByRole('heading', { name: 'Imported history' })).toBeFocused()
  await expect(mine).toHaveCount(0)
  await expect(aximote).toHaveCount(1)

  // Gone from the lists too.
  const res = await page.request.get(`/api/v1/vehicles/${vehicle}/trips/${own}:e2e-trip`)
  expect(res.status()).toBe(404)
})
