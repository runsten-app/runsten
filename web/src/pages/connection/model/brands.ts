// The brands of the Connection page, one tab each, in this order: a brand's query
// parameter carries the outcome of its sign-in back to the page (?volvo=connected).
// Hyundai, Kia and Genesis are read through the same API, Pleos's, each with its own
// key: the owner looks for their make, so each has its own tab.
export const brands = ['volvo', 'polestar', 'tesla', 'bmw', 'hyundai', 'kia', 'genesis'] as const
export const pleosProviders = ['pleos_hyundai', 'pleos_kia', 'pleos_genesis'] as const
export type Brand = (typeof brands)[number]
export type PleosTab = Extract<Brand, 'hyundai' | 'kia' | 'genesis'>
export const pleosTabs: readonly PleosTab[] = ['hyundai', 'kia', 'genesis']
export const isPleos = (b: Brand): b is PleosTab => pleosTabs.includes(b as PleosTab)
