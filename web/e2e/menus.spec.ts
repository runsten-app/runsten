import { checkView, expect, signIn, test, user, vehicleId } from './support'

// The bar's two menus, open: the vehicle's, which names it on every page and leads to its
// settings, and the user's, grouped under who is signed in. Each a navigation of links,
// out of the page's landmarks: axe checks them open.
test('Menus: the vehicle and the user, open', async ({ page }, testInfo) => {
  await signIn(page)
  const id = await vehicleId(page)
  await page.goto(`/vehicles/${id}/trips`)

  const vehicle = page.locator('button.vehicle-menu')
  await expect(vehicle).toHaveAccessibleName(/^Vehicle: .+/)
  await vehicle.click()
  const vehicles = page.getByRole('navigation', { name: /^Vehicle: / })
  // The vehicle shown leads to the same section.
  await expect(vehicles.locator('a[aria-current="true"]')).toHaveAttribute(
    'href',
    new RegExp(`/vehicles/${id}/trips$`),
  )
  await checkView(page, testInfo, 'menu-vehicle')
  await vehicles.getByRole('link', { name: 'Vehicle settings' }).click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${id}/settings$`))

  // The account's pages keep the vehicle: the way back to it.
  await page.getByRole('button', { name: `Account: ${user}` }).click()
  const account = page.getByRole('navigation', { name: `Account: ${user}` })
  await expect(account.getByRole('listitem').first()).toHaveText(`Signed in as ${user}`)
  await checkView(page, testInfo, 'menu-user')
  await account.getByRole('link', { name: 'Settings' }).click()
  await expect(page).toHaveURL(/\/settings$/)
  await vehicle.click()
  // The menu of this page: the previous page's may still be leaving.
  await page
    .locator('.v-overlay--active')
    .getByRole('navigation', { name: /^Vehicle: / })
    .locator('a[aria-current="true"]')
    .click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${id}$`))

  // The keys are listed from the menu, where there is a keyboard.
  await page.getByRole('button', { name: `Account: ${user}` }).click()
  const shortcuts = account.getByRole('button', { name: 'Keyboard shortcuts' })
  if (testInfo.project.name.startsWith('mobile')) {
    await expect(shortcuts).toHaveCount(0)
  } else {
    await shortcuts.click()
    await expect(page.getByRole('dialog', { name: 'Keyboard shortcuts' })).toBeVisible()
  }
})
