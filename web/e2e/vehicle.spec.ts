import type { Page } from '@playwright/test'
import { checkView, expect, signIn, test } from './support'

// reading is a value of the state, by its label.
const reading = (page: Page, label: string) =>
  page.locator('.reading').filter({ has: page.getByText(label, { exact: true }) })

test('Verdandi: the state of charge, the range and how fresh they are', async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto('/')
  await expect(page).toHaveURL(/\/vehicles\/[^/]+$/)
  // The scenario's family is not in the catalog: the family and year, no variant.
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('EX-SIM · 2026')
  await expect(page.getByText(/^YV1[A-Z0-9]{14}$/)).toBeVisible()

  const soc = reading(page, 'State of charge')
  await expect(soc.locator('.value')).toHaveText(/^\d+(\.\d+)?\s?%$/)
  // Said once for the battery's tile: its values share it.
  await expect(page.locator('.battery .under-bar .details')).toHaveText(
    /^checked (now|.+ ago) · since /,
  )
  await expect(reading(page, 'Range').locator('.value')).toHaveText(/^\d[\d,]*\s?km$/)
  await expect(page.getByText(/^Last checked (now|.+ ago)$/)).toBeVisible()
  await expect(page.getByRole('link', { name: 'State' })).toHaveAttribute('aria-current', 'page')
  await checkView(page, testInfo, 'state')
})
