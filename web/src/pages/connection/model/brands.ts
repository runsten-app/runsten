// The brands of the Connection page, one tab each, in this order: a brand's query
// parameter carries the outcome of its sign-in back to the page (?volvo=connected).
export const brands = ['volvo', 'polestar', 'tesla'] as const
export type Brand = (typeof brands)[number]
