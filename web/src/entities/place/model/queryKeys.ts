export const placeKeys = {
  list: () => ['places'] as const,
  detail: (place: string) => ['place', place] as const,
  // The charges without a price change with the places, the charges and their entered
  // costs: every place's are refreshed at once.
  unpricedAll: () => ['unpriced-charges'] as const,
  unpriced: (place: string) => ['unpriced-charges', place] as const,
}
