import { checkView, expect, signIn, test, vehicleId } from './support'

// The keyboard: "?" lists the keys, a digit leads to its tab, Shift held shows them.
test('Shortcuts: the list, a tab by its digit, the keys shown', async ({ page }, testInfo) => {
  await signIn(page)
  await page.goto(`/vehicles/${await vehicleId(page)}`)
  await expect(page.getByRole('link', { name: 'State' })).toHaveAttribute('aria-current', 'page')

  await page.keyboard.press('?')
  const dialog = page.getByRole('dialog', { name: 'Keyboard shortcuts' })
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('dd').first()).toHaveText('State')
  await checkView(page, testInfo, 'shortcuts')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  await page.keyboard.press('2')
  await expect(page).toHaveURL(/\/battery$/)
  await expect(page.getByRole('link', { name: 'Battery' })).toHaveAttribute('aria-current', 'page')

  await page.keyboard.down('Shift')
  await expect(page.locator('.key-hint')).toHaveText(['1', '2', '3', '4', '5'])
  await checkView(page, testInfo, 'shortcuts-hints')
  await page.keyboard.up('Shift')
  await expect(page.locator('.key-hint')).toHaveCount(0)
})
