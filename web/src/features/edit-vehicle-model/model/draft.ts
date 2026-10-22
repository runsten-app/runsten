import type { Model, ModelChoice, VariantOption } from '@/entities/vehicle'

// automatic is the choice of no variant, and of no charger: the recognition, and the more
// powerful charger, apply.
export const automatic = ''

// ModelDraft is the form's: the radio buttons' values, strings.
export type ModelDraft = {
  variant: string // a variant's ID, or automatic
  charger: string // a charger's kW, or automatic
}

// draftOf starts from what the user wrote, as the API gives it back: a recognized variant
// is not a choice.
export function draftOf(m: Model): ModelDraft {
  return {
    variant: m.variant_source === 'chosen' && m.variant ? m.variant.id : automatic,
    charger: m.ac_max_kw === null ? automatic : String(m.ac_max_kw),
  }
}

// recognizedOf is the variant the recognition found: the only candidate.
export function recognizedOf(options: readonly VariantOption[]): VariantOption | null {
  const candidates = options.filter((o) => o.candidate)
  return candidates.length === 1 ? (candidates[0] ?? null) : null
}

// effectiveOf is the variant the draft would put in effect: the chosen one, else the
// recognized one.
export function effectiveOf(
  d: ModelDraft,
  options: readonly VariantOption[],
): VariantOption | null {
  if (d.variant === automatic) return recognizedOf(options)
  return options.find((o) => o.id === d.variant) ?? null
}

// chargersOf lists the onboard chargers a variant may have: the standard one, and the
// optional one if any.
export function chargersOf(v: VariantOption): number[] {
  return v.ac_option_kw === null ? [v.ac_max_kw] : [v.ac_max_kw, v.ac_option_kw]
}

// withVariant chooses a variant, and forgets a charger the new one does not have: the API
// would refuse it.
export function withVariant(
  d: ModelDraft,
  variant: string,
  options: readonly VariantOption[],
): ModelDraft {
  const next = { ...d, variant }
  const v = effectiveOf(next, options)
  if (!v || !chargersOf(v).map(String).includes(d.charger)) next.charger = automatic
  return next
}

export type Rule = 'unknownVariant' | 'notACharger'

// field names the element each issue belongs to, by vehicle: the error summary leads there.
export const field = {
  variant: (vehicle: string) => `vehicle-${vehicle}-variant`,
  charger: (vehicle: string) => `vehicle-${vehicle}-charger`,
}

export type Issue = { field: string; rule: Rule }

// validate checks a draft with the API's rules, and gives the body to send when it breaks
// none: a variant among those offered, and a charger of the variant in effect.
export function validate(
  vehicle: string,
  d: ModelDraft,
  options: readonly VariantOption[],
): { issues: Issue[]; choice: ModelChoice | null } {
  const issues: Issue[] = []
  if (d.variant !== automatic && !options.some((o) => o.id === d.variant))
    issues.push({ field: field.variant(vehicle), rule: 'unknownVariant' })
  const v = effectiveOf(d, options)
  if (d.charger !== automatic && (!v || !chargersOf(v).map(String).includes(d.charger)))
    issues.push({ field: field.charger(vehicle), rule: 'notACharger' })
  if (issues.length) return { issues, choice: null }
  return {
    issues,
    choice: {
      variant_id: d.variant === automatic ? null : d.variant,
      ac_max_kw: d.charger === automatic ? null : Number(d.charger),
    },
  }
}

// modelsOf lists the models the variants are of, in a natural order: a Volvo is offered
// its own model's, a Polestar of the Data Portal its brand's.
export function modelsOf(options: readonly VariantOption[]): string[] {
  return [...new Set(options.map((o) => o.family))].sort((a, b) =>
    a.localeCompare(b, 'en', { numeric: true }),
  )
}

// modelOf is the model shown first: that of the variant chosen, else of the recognized
// one, else the vehicle's, else the one the candidates share, else the only one.
export function modelOf(
  d: ModelDraft,
  options: readonly VariantOption[],
  family: string | null,
): string | null {
  const models = modelsOf(options)
  const v = effectiveOf(d, options)
  if (v) return v.family
  if (family !== null && models.includes(family)) return family
  const candidates = new Set(options.filter((o) => o.candidate).map((o) => o.family))
  if (candidates.size === 1) return [...candidates][0] ?? null
  return models.length === 1 ? (models[0] ?? null) : null
}

// fitsYear tells whether a variant was made in a model year.
export function fitsYear(o: VariantOption, year: number): boolean {
  return o.model_years.from <= year && (o.model_years.to === null || year <= o.model_years.to)
}

// byYear splits a model's variants: those of the vehicle's model year, in the API's order
// (the candidates first), and the others. All fit when the year is unknown, or fits none.
export function byYear(
  options: readonly VariantOption[],
  model: string | null,
  year: number | null,
): { fitting: VariantOption[]; others: VariantOption[] } {
  const of = options.filter((o) => o.family === model)
  const fitting = year === null ? of : of.filter((o) => fitsYear(o, year))
  if (fitting.length === 0) return { fitting: of, others: [] }
  return { fitting, others: of.filter((o) => !fitting.includes(o)) }
}
