import type { Locator } from '@playwright/test'
import { aximoteToken, checkView, expect, signIn, test, vehicleId } from './support'

// Once every project has read the imported history (imports.spec.ts), this one alone
// imports it from the screen, deletes it, and imports it again: the other projects would
// see it come and go.

async function importFromScreen(section: Locator) {
  // Behind a button: there is always an import's report here (imports.setup.ts).
  await section.getByRole('button', { name: 'Import again' }).click()
  await section.getByLabel('Aximote token').fill(aximoteToken)
  await section.getByRole('button', { name: 'Find my vehicles' }).click()
  await expect(section.getByRole('radio', { name: /My EX30/ })).toBeChecked()
  await section.getByRole('button', { name: 'Import', exact: true }).click()
  // The token leaves the page with the form; the report follows the progress.
  await expect(section.getByLabel('Aximote token')).toHaveCount(0)
  await expect(section.locator('.import-report')).toContainText('Import finished', {
    timeout: 30_000,
  })
}

test('Imports: from Aximote on the screen, deleted once confirmed, then imported again', async ({
  page,
}, testInfo) => {
  test.setTimeout(120_000)
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto(`/vehicles/${vehicle}/settings`)
  const section = page.getByRole('region', { name: 'Imported history' })
  const aximote = section.getByRole('listitem').filter({ hasText: 'Imported from Aximote' })

  // Again: the same events, replaced.
  await importFromScreen(section)
  await expect(section.locator('.import-report')).toContainText('3 trips and charges imported.')
  await expect(aximote).toContainText('2 trips · 1 charge')
  await checkView(page, testInfo, 'import-done')

  await aximote.getByRole('button', { name: 'Imported from Aximote: delete' }).click()
  const dialog = page.getByRole('dialog', { name: 'Delete this import?' })
  await dialog.getByRole('button', { name: 'Delete' }).click()
  await expect(section.locator(':scope > [role="status"]')).toHaveText(
    'Imported from Aximote: deleted.',
  )
  await expect(section.getByRole('heading', { name: 'Imported history' })).toBeFocused()
  await expect(aximote).toHaveCount(0)
  // Gone from the lists too.
  const res = await page.request.get(`/api/v1/vehicles/${vehicle}/trips/aximote:trip-1001`)
  expect(res.status()).toBe(404)

  await importFromScreen(section)
  await expect(aximote).toContainText('2 trips · 1 charge')
  const back = await page.request.get(`/api/v1/vehicles/${vehicle}/trips/aximote:trip-1001`)
  expect(back.status()).toBe(200)
})
