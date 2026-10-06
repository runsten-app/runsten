import type { Page } from '@playwright/test'
import {
  checkView,
  exclusive,
  expect,
  setEuro,
  signIn,
  test,
  vehicleId,
  yesterday,
} from './support'

type Charge = {
  id: string
  energy_soc_kwh: number | null
  place: { id: string; name: string } | null
  cost: { source: string } | null
}

// theCharge is the errand's only charge, a DC one, as the API gives it.
async function theCharge(page: Page, vehicle: string): Promise<Charge> {
  const res = await page.request.get(`/api/v1/vehicles/${vehicle}/charges`)
  expect(res.ok(), 'GET /charges').toBe(true)
  const [charge] = ((await res.json()) as { items: Charge[] }).items
  expect(charge, 'a charge').toBeTruthy()
  return charge as Charge
}

// The cost of the DC charge at 0.50 €/kWh: its energy from the state of charge over the
// assumed DC efficiency (0.95), as task compose-test computes it.
const price = 0.5
const tariffCost = (c: Charge) => {
  const minor = Math.round(((c.energy_soc_kwh ?? 0) / 0.95) * price * 100)
  return `€${(minor / 100).toFixed(2)}`
}

// The charge's cost is the account's: one project at a time. Each one gives the charge a
// place of its own if it has none (from the charge, "Create a place here"), reuses it
// otherwise (task compose-test's, or a place left by a killed run), and leaves the charge
// as it found it: its place deleted, its entered cost too.
test('Costs: a place from the charge, its cost in the details, the list and the statistics, then a cost entered and deleted', async ({
  page,
}, testInfo) => {
  test.setTimeout(180_000)
  await signIn(page)
  const vehicle = await vehicleId(page)
  await exclusive('costs', async () => {
    await setEuro(page)
    let charge = await theCharge(page, vehicle)
    if (charge.cost?.source === 'entered')
      await page.request.delete(`/api/v1/vehicles/${vehicle}/charges/${charge.id}/cost`)

    await page.goto(`/vehicles/${vehicle}/charges`)
    await page.locator('.charge-summary').click()
    await expect(page).toHaveURL(/\/charges\/[^/?]+$/)
    const details = page.url()
    const card = page.locator('.cost')

    let created: string | undefined
    if (!charge.place) {
      // A place at the charge's position, from the charge itself.
      const name = `E2E costs ${testInfo.project.name}`
      await card.getByRole('link', { name: 'Create a place here' }).click()
      await expect(page).toHaveURL(/\/settings\/places\/new\?lat=[\d.-]+&lon=[\d.-]+$/)
      await expect(page.getByLabel('Latitude')).toHaveValue(/^-?\d+\.\d{5}$/)
      await page.getByLabel('Name').fill(name)
      // From the day before: the charge happened today, or yesterday for an older stack.
      await page.locator('#place-v0-from').fill(yesterday())
      await page.locator('#place-v0-price').fill('0,50')
      await page.getByRole('button', { name: 'Save', exact: true }).click()
      await expect(page.locator('.place-form [role="status"]')).toHaveText('Place saved.')
      created = /\/places\/([0-9a-f-]{36})/.exec(page.url())?.[1]
      charge = await theCharge(page, vehicle)
      await page.goto(details)
    }
    const place = charge.place?.name ?? ''
    const estimated = tariffCost(charge)

    // The cost of the place's tariff, estimated: one price over the charge, one amount.
    const cost = card.locator('.fact').filter({ hasText: /^Cost/ })
    await expect(card.locator('.fact').first().locator('dd')).toContainText(place)
    if (created) await expect(cost.locator('.value')).toHaveText(estimated)
    await expect(cost).toContainText(`Estimated from the tariff of ${place}`)
    await expect(cost).toContainText(/kWh billed \(estimated\), assuming an efficiency of 95\s?%/)
    await checkView(page, testInfo, 'charge-cost')

    // In the list, on the charge's line and in the totals.
    await page.getByRole('link', { name: 'Charges' }).first().click()
    await expect(page).toHaveURL(/\/charges$/)
    const line = page.locator('.charge-summary .cost')
    await expect(line).toHaveText(/^€\d+\.\d{2}$/)
    if (created) await expect(line).toHaveText(estimated)
    await expect(page.locator('.period-totals')).toContainText(/ · €\d+\.\d{2}$/)
    await checkView(page, testInfo, 'charges-cost')

    // Entered: an empty form tells what is missing in a summary that takes the focus.
    await page.goto(details)
    const toggle = card.getByRole('button', { name: 'Enter the cost' })
    await toggle.click()
    await expect(page.getByLabel('Amount paid')).toBeFocused()
    await page.getByRole('button', { name: 'Save the cost' }).click()
    const summary = page.locator('.error-summary')
    await expect(summary).toBeFocused()
    await expect(summary.getByRole('link')).toHaveText(['Amount paid: Required.'])
    await checkView(page, testInfo, 'charge-cost-error')
    await page.getByLabel('Amount paid').fill('12,34')
    await page.getByLabel('Note (optional)').fill('E2E, public charger')
    await page.getByRole('button', { name: 'Save the cost' }).click()
    await expect(card.locator('[role="status"]')).toHaveText('Cost saved.')
    const change = card.getByRole('button', { name: 'Change the entered cost' })
    await expect(change).toBeFocused()
    await expect(cost.locator('.value')).toHaveText('€12.34')
    await expect(cost).toContainText('Entered')
    await expect(cost).toContainText('Note: E2E, public charger')
    await checkView(page, testInfo, 'charge-cost-entered')

    // In the statistics: the tile and, in the costs' view, the chart, with its table.
    await page.getByRole('link', { name: 'Stats' }).click()
    await expect(page).toHaveURL(/\/stats\?/)
    await page.getByRole('button', { name: 'Costs', exact: true }).click()
    await expect(page).toHaveURL(/view=costs/)
    await expect(page.locator('.tile.cost .value')).toHaveText('€12.34')
    await expect(page.locator('.tile.cost .note')).toContainText(['1 cost entered'])
    const chart = page.locator('.chart-frame.cost')
    await expect(chart.getByRole('img')).toHaveAttribute(
      'aria-label',
      /^Cost of charges: €12\.34 in all/,
    )
    await expect(chart.locator('thead th')).toHaveText(['Day', 'Lowest cost', 'Highest cost'])
    await expect(chart.locator('tbody tr')).not.toHaveCount(0)
    await checkView(page, testInfo, 'stats-cost')

    // Deleted once confirmed: the tariff's estimate comes back.
    await page.goto(details)
    await card.getByRole('button', { name: 'Delete the entered cost' }).click()
    const dialog = page.getByRole('dialog', { name: 'Delete the entered cost?' })
    await expect(dialog).toBeVisible()
    await checkView(page, testInfo, 'charge-cost-delete')
    await dialog.getByRole('button', { name: 'Delete' }).click()
    await expect(card.locator('[role="status"]')).toHaveText('Entered cost deleted.')
    await expect(card.getByRole('button', { name: 'Enter the cost' })).toBeFocused()
    await expect(cost).toContainText(`Estimated from the tariff of ${place}`)
    if (created) await expect(cost.locator('.value')).toHaveText(estimated)

    if (created) {
      const res = await page.request.delete(`/api/v1/places/${created}`)
      expect(res.status(), 'DELETE /places').toBe(204)
    }
  })
})
