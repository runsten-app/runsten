export const vehicleKeys = {
  list: () => ['vehicles'] as const,
  detail: (vehicle: string) => ['vehicle', vehicle] as const,
  // details is every vehicle's detail: a change of the account's connection shows in each.
  details: () => ['vehicle'] as const,
  state: (vehicle: string) => ['state', vehicle] as const,
  variants: (vehicle: string) => ['variants', vehicle] as const,
}
