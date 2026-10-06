import { checkView, expect, expectAccessible, password, tabTo, test, user } from './support'

// A failed attempt counts against the login throttle, per peer: all of them come from
// runsten-web (10 in 15 minutes). One light and one dark project try, so that task e2e
// can run again at once.
const failing = ['desktop-light', 'mobile-dark']

test('signs in through the form, after a wrong password', async ({ page }, testInfo) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/login\?next=\/$/)
  await checkView(page, testInfo, 'login')

  const username = page.getByLabel('Username')
  const pass = page.getByLabel('Password', { exact: true })
  const signIn = page.getByRole('button', { name: 'Sign in' })
  if (failing.includes(testInfo.project.name)) {
    await username.fill(user)
    await pass.fill('wrong password, surely')
    await signIn.click()
    await expect(page.locator('.v-alert')).toHaveText('Wrong username or password.')
    await expectAccessible(page)
  }

  await username.fill(user)
  await pass.fill(password)
  await signIn.click()
  // The home page goes on to the only vehicle.
  await expect(page).toHaveURL(/\/vehicles\/[^/]+$/)
  await expect(page.getByRole('heading', { name: 'Battery' })).toBeVisible()
})

test('signs in and opens a trip with the keyboard alone', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name.startsWith('mobile'), 'a keyboard is a desktop matter')
  await page.goto('/login')
  const username = page.getByLabel('Username')
  await expect(username).toBeFocused()
  await page.keyboard.type(user)
  await page.keyboard.press('Tab')
  await expect(page.getByLabel('Password', { exact: true })).toBeFocused()
  await page.keyboard.type(password)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/vehicles\/[^/]+$/)

  await tabTo(page, page.getByRole('link', { name: 'Trips' }))
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/trips$/)
  await tabTo(page, page.locator('.trip-summary').first())
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/trips\/[^/]+$/)
  await expect(page.getByRole('heading', { name: 'Trip', exact: true })).toBeVisible()
})
