// The brands of the Connection page, one tab each, in this order: a brand's query
// parameter carries the outcome of its sign-in back to the page (?volvo=connected). The
// Pleos tab holds the three brands its API reads — Hyundai, Kia, Genesis —, each with
// its own key: the tab name stays one.
export const brands = ['volvo', 'polestar', 'tesla', 'bmw', 'pleos'] as const
export const pleosProviders = ['pleos_hyundai', 'pleos_kia', 'pleos_genesis'] as const
export type Brand = (typeof brands)[number]
