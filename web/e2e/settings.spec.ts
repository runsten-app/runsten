import { readFile } from 'node:fs/promises'
import type { Page } from '@playwright/test'
import { checkView, exclusive, expect, signIn, test, user, vehicleId } from './support'

// The projects run at once against the same stack, and compose-test sets a currency and
// a place after them: each project works on a place of its own name, deletes it, and
// only ever chooses the euro (setting the same currency again is accepted, a change is
// not once a price exists). The place is at the vehicle's position, where the errand's
// DC charge ended: it holds that charge while it exists, so it lives under the costs
// spec's lock, which would take it for the charge's place otherwise.
const currency = 'Euro (EUR)'

async function chooseCurrency(page: Page) {
  // Vuetify's field covers its input: the field opens the list.
  await page.locator('.currency-form .v-field').click()
  await page.getByRole('option', { name: currency }).click()
  await page.getByRole('button', { name: 'Save the currency' }).click()
  await expect(page.locator('.currency-form [role="status"]')).toHaveText('Currency saved.')
}

test('Settings: the currency, then a place with a window across midnight, reopened, changed and deleted', async ({
  page,
}, testInfo) => {
  test.setTimeout(180_000)
  const name = `E2E ${testInfo.project.name} ${Date.now()}`
  await signIn(page)
  await page.goto('/')
  await expect(page).toHaveURL(/\/vehicles\/[^/]+$/)
  await page.getByRole('button', { name: `Account: ${user}` }).click()
  await page.locator('a.settings').click()
  await expect(page).toHaveURL(/\/settings$/)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Settings')
  await chooseCurrency(page)
  await checkView(page, testInfo, 'settings')

  await exclusive('costs', async () => {
    // A new place: an empty form, sent as is, tells every error in a summary that takes
    // the focus, each line leading to its field.
    await page.getByRole('link', { name: 'New place' }).click()
    await expect(page).toHaveURL(/\/settings\/places\/new/)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('New place')
    await checkView(page, testInfo, 'place-new')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    const summary = page.locator('.error-summary')
    await expect(summary).toBeFocused()
    await expect(summary.getByRole('link')).toHaveText([
      'Name: Required.',
      'Latitude: Required.',
      'Longitude: Required.',
      /Base price: Required\.$/,
    ])
    await checkView(page, testInfo, 'place-error')
    await summary.getByRole('link', { name: 'Name: Required.' }).click()
    await expect(page.getByLabel('Name')).toBeFocused()

    // Filled in: the vehicle's position, a base price, and off-peak hours across midnight.
    await page.getByLabel('Name').fill(name)
    await page.getByRole('button', { name: /^Current position of / }).click()
    await expect(page.getByLabel('Latitude')).toHaveValue(/^-?\d+\.\d{5}$/)
    await page.locator('#place-v0-price').fill('0,25')
    await page.getByRole('button', { name: 'Add a time window' }).click()
    await expect(page.locator('#place-v0-w0-days')).toBeFocused()
    await page.locator('#place-v0-w0-from').fill('22:00')
    await page.locator('#place-v0-w0-to').fill('06:00')
    await page.locator('#place-v0-w0-price').fill('0.15')
    await expect(page.getByText('Crosses midnight: ends the next day.')).toBeVisible()
    const save = page.getByRole('button', { name: 'Save', exact: true })
    await save.click()
    await expect(page.locator('.place-form [role="status"]')).toHaveText('Place saved.')
    await expect(page).toHaveURL(/\/settings\/places\/[0-9a-f-]{36}$/)
    await expect(save).toBeFocused()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(name)
    await checkView(page, testInfo, 'place-filled')

    // Listed with its price of today, and reopened as it was saved.
    await page.getByRole('link', { name: 'Settings' }).first().click()
    const row = page.getByRole('listitem').filter({ hasText: name })
    await expect(row).toContainText(/€0\.25\/kWh since .* · 1 time window/)
    await row.getByRole('link').click()
    await expect(page.getByLabel('Name')).toHaveValue(name)
    await expect(page.locator('#place-v0-w0-from')).toHaveValue('22:00')
    await expect(page.locator('#place-v0-w0-to')).toHaveValue('06:00')
    await expect(page.locator('#place-v0-price')).toHaveValue('0.25')

    // A new price from tomorrow, a copy of today's with its window.
    await page.getByRole('button', { name: 'New price from…' }).click()
    await expect(page.locator('#place-v1-from')).toBeFocused()
    await expect(page.locator('#place-v1-w0-from')).toHaveValue('22:00')
    await page.locator('#place-v1-price').fill('0.3')
    await save.click()
    await expect(page.locator('.place-form [role="status"]')).toHaveText('Place saved.')

    // Deleted once confirmed in a dialog, then gone from the list.
    await page.getByRole('button', { name: 'Delete', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: `Delete ${name}?` })
    await expect(dialog).toBeVisible()
    await dialog.getByRole('button', { name: 'Delete' }).click()
    await expect(page).toHaveURL(/\/settings$/)
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Settings')
    await expect(page.getByRole('listitem').filter({ hasText: name })).toHaveCount(0)
  })
})

// The settings lead to each vehicle's own, as the vehicle's page does, from their
// Vehicles area, which the areas' navigation leads to.
test("Settings: the Vehicles area leads to a vehicle's settings", async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto('/settings')
  const areas = page.getByRole('navigation', { name: 'Settings areas' })
  await expect(areas.getByRole('link', { name: 'Costs' })).toHaveAttribute('aria-current', 'page')
  await areas.getByRole('link', { name: 'Vehicles' }).click()
  await expect(page).toHaveURL(/\/settings\/vehicles$/)
  await expect(areas.getByRole('link', { name: 'Vehicles' })).toHaveAttribute(
    'aria-current',
    'page',
  )
  await checkView(page, testInfo, 'settings-vehicles')
  const section = page.getByRole('region', { name: 'Vehicles' })
  await section.getByRole('link', { name: /EX-SIM · 2026/ }).click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/settings$`))
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Vehicle settings')
})

// The simulated vehicle reports a family the catalog does not have: its settings say so,
// with what the vehicle reports, and offer no variant (the choice is tested in Vitest).
test("Vehicle settings: a family the catalog lacks, from the bar's vehicle menu", async ({
  page,
}, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  await page.goto(`/vehicles/${vehicle}`)
  await page.locator('button.vehicle-menu').click()
  await page.getByRole('link', { name: 'Vehicle settings' }).click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/settings$`))
  // Two areas, each a link of the sub-navigation: the model first.
  const areas = page.getByRole('navigation', { name: 'Vehicle settings areas' })
  await expect(areas.getByRole('link')).toHaveText(['Vehicle', 'Imported history'])
  await expect(areas.getByRole('link', { name: 'Vehicle', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  )
  const section = page.getByRole('region', { name: 'Vehicle', exact: true })
  await expect(section.locator('dd')).toHaveText([/^[A-HJ-NPR-Z0-9]{17}$/, 'EX-SIM', '2026'])
  await expect(section.locator('.none')).toHaveText(
    "Runsten's catalog has no variant of the EX-SIM: it lists the electric Volvo, Polestar and Tesla models, and the plug-in hybrid Volvos and the Polestar 1, only.",
  )
  await expect(section.getByRole('radio')).toHaveCount(0)
  // The deletion last, on a tile of its own.
  await expect(section.getByRole('region', { name: 'Delete this vehicle' })).toBeVisible()
  await checkView(page, testInfo, 'vehicle-settings')

  await areas.getByRole('link', { name: 'Imported history' }).click()
  await expect(page).toHaveURL(new RegExp(`/vehicles/${vehicle}/settings/history$`))
  await expect(page.getByRole('region', { name: 'Imported history' })).toBeVisible()
  await expect(areas.getByRole('link', { name: 'Imported history' })).toHaveAttribute(
    'aria-current',
    'page',
  )
})

// A variant as GET /variants gives it (the catalog's EX30 figures, public sources).
function variant(id: string, name: string, option: number | null, candidate: boolean) {
  const level = 'secondary'
  return {
    id,
    name,
    gross_kwh: option ? 69 : 51,
    net_kwh: option ? 64 : 49,
    ac_max_kw: 11,
    ac_option_kw: option,
    dc_max_kw: option ? 153 : 134,
    family: 'EX30',
    model_years: { from: 2024, to: 2026 },
    candidate,
    levels: {
      gross_kwh: level,
      net_kwh: level,
      ac_max_kw: level,
      ac_option_kw: option ? level : null,
      dc_max_kw: level,
    },
  }
}

// The stack's vehicle is of no family of the catalog: to see the form in a browser, with
// axe, the API is made to answer as for an EX30 whose details fit two variants. The rules
// of the choice are tested against the API in Go, the form in Vitest.
test('Vehicle settings: the variant and the charger of an EX30, as its driver chooses them', async ({
  page,
}, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  const ex30 = {
    family: 'EX30',
    model_year: 2024,
    variant: null,
    variant_source: null,
    ac_max_kw: null,
    powertrain: 'electric',
  }
  await page.route('**/api/v1/vehicles', async (route) => {
    const res = await route.fetch()
    const body = (await res.json()) as { items: { model: unknown }[] }
    await route.fulfill({
      response: res,
      json: { items: body.items.map((v) => ({ ...v, model: ex30 })) },
    })
  })
  await page.route(`**/api/v1/vehicles/${vehicle}`, async (route) => {
    const res = await route.fetch()
    const body = (await res.json()) as { model: unknown }
    await route.fulfill({ response: res, json: { ...body, model: ex30 } })
  })
  const er = variant('ex30-er-2024', 'EX30 Single Motor Extended Range', 22, true)
  await page.route('**/api/v1/vehicles/*/variants', (route) =>
    route.fulfill({
      json: {
        items: [
          er,
          variant('ex30-twin-2024', 'EX30 Twin Motor Performance / Cross Country', 22, true),
          variant('ex30-lfp-2024', 'EX30 Single Motor', null, false),
        ],
      },
    }),
  )
  let sent: unknown
  await page.route('**/api/v1/vehicles/*/model', async (route) => {
    sent = route.request().postDataJSON()
    const current = await (await page.request.get(`/api/v1/vehicles/${vehicle}`)).json()
    const { id, name, gross_kwh, net_kwh, ac_max_kw, ac_option_kw, dc_max_kw } = er
    const chosen = { id, name, gross_kwh, net_kwh, ac_max_kw, ac_option_kw, dc_max_kw }
    await route.fulfill({
      json: {
        ...current,
        model: { ...ex30, variant: chosen, variant_source: 'chosen', ac_max_kw: 11 },
      },
    })
  })

  await page.goto(`/vehicles/${vehicle}/settings`)
  const section = page.locator('.vehicle-model')
  const variants = section.getByRole('group', { name: 'Variant' })
  await expect(variants.getByRole('radio')).toHaveCount(4)
  await expect(variants.getByRole('radio', { name: /^Automatic recognition/ })).toBeChecked()
  // Two candidates: nothing recognized, no variant in effect, no charger asked.
  await expect(section.getByRole('group', { name: 'Onboard charger' })).toHaveCount(0)
  await checkView(page, testInfo, 'vehicle-settings-model')

  await variants.getByRole('radio', { name: /^EX30 Single Motor Extended Range/ }).check()
  const chargers = section.getByRole('group', { name: 'Onboard charger' })
  await expect(chargers.getByRole('radio', { name: /^Not stated: 22 kW assumed/ })).toBeChecked()
  await chargers.getByRole('radio', { name: '11 kW, standard' }).check()
  const save = section.getByRole('button', { name: /^Save the variant of / })
  await save.click()
  await expect(section.getByRole('status')).toHaveText('Variant saved.')
  await expect(save).toBeFocused()
  expect(sent).toEqual({ variant_id: 'ex30-er-2024', ac_max_kw: 11 })
  await checkView(page, testInfo, 'vehicle-settings-model-chosen')

  // A variant without the option: the charger is no longer asked.
  await variants.getByRole('radio', { name: /^EX30 Single Motor,/ }).check()
  await expect(chargers).toHaveCount(0)
})

// A Polestar of the Data Portal tells its brand, not its family: it is offered the brand's
// variants, of several models and years, which the form asks for the model first. The
// figures are placeholders: the form shows them, the rules are tested in Go.
function polestar(id: string, name: string, family: string, from: number, to: number) {
  const level = 'secondary'
  return {
    ...variant(id, name, null, false),
    gross_kwh: 82,
    net_kwh: 79,
    dc_max_kw: 205,
    family,
    model_years: { from, to },
    levels: {
      gross_kwh: level,
      net_kwh: level,
      ac_max_kw: level,
      ac_option_kw: null,
      dc_max_kw: level,
    },
  }
}

test('Vehicle settings: the model, then the variant of a Polestar', async ({ page }, testInfo) => {
  await signIn(page)
  const vehicle = await vehicleId(page)
  const model = {
    family: null,
    model_year: 2024,
    variant: null,
    variant_source: null,
    ac_max_kw: null,
    powertrain: 'electric',
  }
  await page.route('**/api/v1/vehicles', async (route) => {
    const res = await route.fetch()
    const body = (await res.json()) as { items: { model: unknown }[] }
    await route.fulfill({
      response: res,
      json: { items: body.items.map((v) => ({ ...v, model })) },
    })
  })
  await page.route(`**/api/v1/vehicles/${vehicle}`, async (route) => {
    const res = await route.fetch()
    const body = (await res.json()) as { model: unknown }
    await route.fulfill({ response: res, json: { ...body, model } })
  })
  await page.route('**/api/v1/vehicles/*/variants', (route) =>
    route.fulfill({
      json: {
        items: [
          polestar(
            'ps2-lr-sm-2024',
            'Polestar 2 Long Range Single Motor',
            'Polestar 2',
            2024,
            2026,
          ),
          polestar('ps2-lr-dm-2024', 'Polestar 2 Long Range Dual Motor', 'Polestar 2', 2024, 2026),
          polestar(
            'ps2-lr-sm-2021',
            'Polestar 2 Long Range Single Motor',
            'Polestar 2',
            2021,
            2023,
          ),
          polestar('ps3-lr-dm-2025', 'Polestar 3 Long Range Dual Motor', 'Polestar 3', 2025, 2026),
        ],
      },
    }),
  )

  await page.goto(`/vehicles/${vehicle}/settings`)
  const section = page.locator('.vehicle-model')
  const models = section.getByRole('group', { name: 'Model', exact: true })
  const variants = section.getByRole('group', { name: 'Variant' })
  // No family and no candidate: no model pressed, automatic recognition alone.
  await expect(models.getByRole('button')).toHaveCount(2)
  await expect(models.getByRole('button', { pressed: true })).toHaveCount(0)
  await expect(variants.getByRole('radio')).toHaveCount(1)
  await expect(section.getByText('Choose the model to see its variants.')).toBeVisible()
  await checkView(page, testInfo, 'vehicle-settings-polestar')

  // The model: the variants of the vehicle's model year, the others folded.
  const ps2 = models.getByRole('button', { name: 'Polestar 2' })
  await ps2.click()
  await expect(ps2).toHaveAttribute('aria-pressed', 'true')
  await expect(variants.getByRole('radio')).toHaveCount(3)
  const others = section.getByRole('button', { name: 'Other model years (1)' })
  await expect(others).toHaveAttribute('aria-expanded', 'false')
  await others.click()
  await expect(others).toHaveAttribute('aria-expanded', 'true')
  await expect(variants.getByRole('radio')).toHaveCount(4)
  const older = section.getByRole('radiogroup', { name: 'Other model years (1)' })
  await older.getByRole('radio').check()
  await expect(older.getByRole('radio')).toBeChecked()
  await checkView(page, testInfo, 'vehicle-settings-polestar-other-years')

  // Another model forgets the variant of the previous one.
  await models.getByRole('button', { name: 'Polestar 3' }).click()
  await expect(variants.getByRole('radio', { name: /^Automatic recognition/ })).toBeChecked()
  await expect(variants.getByRole('radio')).toHaveCount(2)
})

// The account's data: the export is downloaded whole.
test("Settings: the account's data, downloaded", async ({ page }, testInfo) => {
  await signIn(page)
  await page.goto('/settings/account')
  await expect(page.getByRole('heading', { level: 2 })).toHaveText('Account')
  await checkView(page, testInfo, 'settings-account')
  const section = page.getByRole('region', { name: 'Your data' })
  const download = page.waitForEvent('download')
  await section.getByRole('link', { name: 'Download my data' }).click()
  const file = await download
  expect(file.suggestedFilename()).toMatch(/^runsten-account-\d{4}-\d{2}-\d{2}\.json$/)
  const path = await file.path()
  const exported = JSON.parse(await readFile(path, 'utf8')) as {
    format: string
    tables: { name: string; rows: unknown[] }[]
  }
  expect(exported.format).toBe('runsten-account')
  const rows = Object.fromEntries(exported.tables.map((t) => [t.name, t.rows.length]))
  expect(rows.vehicles).toBeGreaterThan(0)
  expect(rows.snapshots).toBeGreaterThan(0)
})
