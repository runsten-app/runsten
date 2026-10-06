export type CurrencyUnit = { code: string; minor_digits: number }

// A price per kWh has more decimals than an amount (0.1356 €), up to the API's 5.
const priceDigits = 5

// formatPricePerKwh shows a price per kWh in the account's currency, with at least the
// currency's own decimals ("0.20 €/kWh", "0.2142 €/kWh"). Without a currency, a number.
export function formatPricePerKwh(
  price: number,
  currency: CurrencyUnit | null,
  locale: string,
): string {
  const n = new Intl.NumberFormat(locale, {
    ...(currency ? { style: 'currency', currency: currency.code } : {}),
    minimumFractionDigits: currency?.minor_digits ?? 0,
    maximumFractionDigits: priceDigits,
  }).format(price)
  return `${n}/kWh`
}

// currencySymbol is the currency as Intl writes it next to an amount ("€", "SEK").
export function currencySymbol(code: string, locale: string): string {
  const parts = new Intl.NumberFormat(locale, { style: 'currency', currency: code })
  return parts.formatToParts(0).find((p) => p.type === 'currency')?.value ?? code
}

// currencyName names a currency in the language ("euro", "Euro", "euro").
export function currencyName(code: string, locale: string): string {
  return new Intl.DisplayNames([locale], { type: 'currency' }).of(code) ?? code
}
