import { checkView, expect, signIn, test } from './support'

test('Urd: the DC charge and its two energies', async ({ page }, testInfo) => {
  await signIn(page)
  await page.goto('/')
  await expect(page).toHaveURL(/\/vehicles\/[^/]+$/)
  await page.getByRole('link', { name: 'Charges' }).click()
  await expect(page).toHaveURL(/\/charges$/)

  const charge = page.locator('.charge-summary')
  await expect(charge).toHaveCount(1)
  await expect(charge.locator('.figures')).toHaveText(/^DC · \d+\s?% → (79|80)\s?% · /)
  await checkView(page, testInfo, 'charges')

  await charge.click()
  await expect(page).toHaveURL(/\/charges\/[^/?]+$/)
  const energies = page.locator('.energies dd')
  await expect(energies).toHaveCount(2)
  for (const energy of await energies.all()) await expect(energy).toHaveText(/^\d+(\.\d+)?\s?kWh$/)
  await checkView(page, testInfo, 'charge')
})
