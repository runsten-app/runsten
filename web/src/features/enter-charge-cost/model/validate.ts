import type { Cost, EnteredCost } from '@/entities/charge'
import type { EnteredCostLimits } from '@/entities/settings'
import { amountInput, decimalPlaces, majorUnits, parseAmount, parseDecimal } from '@/shared/lib'

// Field names every issue by the element that shows it: the error summary leads there.
export const field = {
  amount: 'charge-cost-amount',
  energy: 'charge-cost-energy',
  note: 'charge-cost-note',
}

// The fields as typed: numbers stay text until checked (a point or a comma).
export type CostDraft = { amount: string; energy: string; note: string }

export type Rule = 'required' | 'notNumber' | 'range' | 'decimals' | 'aboveZero' | 'tooLong'
export type Issue = { field: string; rule: Rule; params?: Record<string, number> }
export type Checked = { issues: Issue[]; cost: EnteredCost | null }

// draftOf starts from the entered cost, if any; a tariff's is not what was paid.
export function draftOf(cost: Cost | null, digits: number): CostDraft {
  if (cost?.source !== 'entered') return { amount: '', energy: '', note: '' }
  return {
    amount: amountInput(cost.min_minor, digits),
    energy: cost.energy_kwh === null ? '' : String(cost.energy_kwh),
    note: cost.note ?? '',
  }
}

// validate checks a draft with the API's rules (EnteredCost, within its limits), in a
// currency of digits decimals, and gives the body to send when it breaks none.
export function validate(d: CostDraft, digits: number, limits: EnteredCostLimits): Checked {
  const issues: Issue[] = []

  let amount: number | null = null
  const maxAmount = majorUnits(limits.amount_minor_max, digits)
  const a = parseDecimal(d.amount)
  if (!d.amount.trim()) issues.push({ field: field.amount, rule: 'required' })
  else if (a === null) issues.push({ field: field.amount, rule: 'notNumber' })
  else if (a < 0 || a > maxAmount)
    issues.push({ field: field.amount, rule: 'range', params: { min: 0, max: maxAmount } })
  else if (decimalPlaces(d.amount) > digits)
    issues.push({ field: field.amount, rule: 'decimals', params: { max: digits } })
  else {
    // A sign or a bare fraction (".5") reads as a number, not as an amount.
    amount = parseAmount(d.amount, digits)
    if (amount === null) issues.push({ field: field.amount, rule: 'notNumber' })
  }

  let energy: number | null = null
  if (d.energy.trim()) {
    energy = parseDecimal(d.energy)
    if (energy === null) issues.push({ field: field.energy, rule: 'notNumber' })
    else if (energy <= 0 || energy > limits.energy_kwh_max)
      issues.push({
        field: field.energy,
        rule: 'aboveZero',
        params: { max: limits.energy_kwh_max },
      })
  }

  const note = d.note.trim()
  // In characters, as the API counts them, not UTF-16 units.
  if ([...note].length > limits.note_chars)
    issues.push({ field: field.note, rule: 'tooLong', params: { max: limits.note_chars } })

  if (issues.length || amount === null) return { issues, cost: null }
  return { issues, cost: { amount_minor: amount, energy_kwh: energy, note: note || null } }
}
