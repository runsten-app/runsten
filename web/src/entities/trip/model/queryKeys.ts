export const tripKeys = {
  // Every list of a vehicle, whatever the period: where a trip may already be.
  lists: (vehicle: string) => ['trips', vehicle] as const,
  list: (vehicle: string, from?: string, to?: string) =>
    ['trips', vehicle, from ?? null, to ?? null] as const,
  detail: (vehicle: string, id: string) => ['trip', vehicle, id] as const,
}
