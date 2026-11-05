export const tripKeys = {
  // Every trip of every vehicle: what deleting an import touches.
  details: () => ['trip'] as const,
  // Every list of a vehicle, whatever the period: where a trip may already be.
  lists: (vehicle: string) => ['trips', vehicle] as const,
  list: (vehicle: string, from?: string, to?: string) =>
    ['trips', vehicle, from ?? null, to ?? null] as const,
  // The maps of a vehicle, apart from its lists: they hold no page of trips.
  maps: (vehicle: string) => ['trip-map', vehicle] as const,
  map: (vehicle: string, from?: string, to?: string) =>
    ['trip-map', vehicle, from ?? null, to ?? null] as const,
  detail: (vehicle: string, id: string) => ['trip', vehicle, id] as const,
  track: (vehicle: string, id: string) => ['trip', vehicle, id, 'track'] as const,
}
