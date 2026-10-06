import { checkView, expect, signIn, test, user, vehicleId } from './support'

// The user's own preferences, from the menu under their name: neither the account's
// settings nor a vehicle's.
test('Preferences: from the user menu', async ({ page }, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}`)
  await page.getByRole('button', { name: `Account: ${user}` }).click()
  await page.locator('a.preferences').click()
  await expect(page).toHaveURL(/\/preferences$/)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Preferences')
  await expect(page.getByRole('heading', { level: 2 })).toHaveText([
    'Language',
    'Appearance',
    'Keyboard shortcuts',
  ])
  await checkView(page, testInfo, 'preferences')
})
