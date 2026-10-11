import { aximoteToken, checkView, expect, signIn, test, vehicleId } from './support'

// The history imported from the simulator's Aximote before these tests (imports.setup.ts):
// two trips and a charge, a month before the first reading. These tests only read it,
// in every project at once; imports-run.spec.ts imports and deletes it once they are
// done.

test('Imports: an imported trip, marked with its app, in the list and in its details', async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}/trips`)

  const imported = page
    .locator('.trip-summary')
    .filter({ has: page.getByText('Imported from Aximote', { exact: true }) })
  await expect(imported).toHaveCount(2)
  // To Villeurbanne: its start may be in a place another test made (costs.spec.ts).
  const morning = imported.filter({ hasText: /→ Villeurbanne, France/ })
  // Its times are the app's: one instant at each end, never a range.
  await expect(morning.locator('.event-when')).toHaveText(/^\d{2}:\d{2} → \d{2}:\d{2}$/)
  await checkView(page, testInfo, 'trips-imported')

  await morning.click()
  await expect(page).toHaveURL(/\/trips\/aximote:trip-1001$/)
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
  await expect(page).toHaveURL(/\/charges\/aximote:charge-501$/)
  await expect(page.locator('.imported-note')).toBeVisible()
  await expect(page.locator('.energies')).toContainText(/Average power\s*8\.4\s?kW/)
  await expect(page.getByRole('button', { name: 'Enter the cost' })).toHaveCount(0)
  await checkView(page, testInfo, 'charge-imported')
})

test("Imports: a vehicle's settings tell the last import, list Aximote's vehicles, and ask before deleting", async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}/settings`)

  const section = page.getByRole('region', { name: 'Imported history' })
  const report = section.locator('.import-report')
  await expect(report).toContainText('Import finished')
  await expect(report).toContainText('3 trips and charges imported.')
  await expect(report).toContainText('2 left unfinished or out of bounds.')
  const aximote = section.getByRole('listitem').filter({ hasText: 'Imported from Aximote' })
  await expect(aximote).toContainText('2 trips · 1 charge')
  await checkView(page, testInfo, 'vehicle-settings-imports')

  // Behind a button once an import is over.
  await section.getByRole('button', { name: 'Import again' }).click()
  // A token mistyped: told before anything is sent.
  const token = section.getByLabel('Aximote token')
  await token.fill('axm_pat_short')
  await section.getByRole('button', { name: 'Find my vehicles' }).click()
  const summary = section.locator('.error-summary')
  await expect(summary).toBeFocused()
  await expect(summary.getByRole('link')).toHaveText([
    'A token is axm_pat_ followed by 26 letters and digits: copy it again.',
  ])
  await checkView(page, testInfo, 'import-token-error')

  // The token's vehicles: the one with this vehicle's VIN is chosen. Nothing imported.
  await token.fill(aximoteToken)
  await section.getByRole('button', { name: 'Find my vehicles' }).click()
  await expect(section.locator('.aximote-import [role="status"]')).toHaveText(
    'Aximote gives 2 vehicles.',
  )
  const vehicles = section.getByRole('group', { name: 'Vehicle in Aximote' })
  await expect(vehicles.getByRole('radio', { name: /My EX30/ })).toBeChecked()
  await expect(vehicles.getByRole('radio', { name: /Old V60/ })).not.toBeChecked()
  await expect(vehicles).toContainText('Same VIN as this vehicle')
  await expect(section.getByRole('button', { name: 'Import', exact: true })).toBeVisible()
  await checkView(page, testInfo, 'import-vehicles')

  await section.getByRole('button', { name: 'Cancel' }).click()
  await expect(token).toHaveCount(0)
  await aximote.getByRole('button', { name: 'Imported from Aximote: delete' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete this import?' })
  await expect(dialog).toBeVisible()
  await checkView(page, testInfo, 'import-delete')
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toBeHidden()
  await expect(aximote).toHaveCount(1)
})
