export const chargeKeys = {
  // Every list and every charge of every vehicle: what a change of the costs touches.
  all: () => ['charges'] as const,
  details: () => ['charge'] as const,
  // Every list of a vehicle, whatever the period: where a charge may already be.
  lists: (vehicle: string) => ['charges', vehicle] as const,
  list: (vehicle: string, from?: string, to?: string) =>
    ['charges', vehicle, from ?? null, to ?? null] as const,
  detail: (vehicle: string, id: string) => ['charge', vehicle, id] as const,
  // The entered costs no charge takes.
  orphans: () => ['charge-costs', 'orphans'] as const,
  // The charges an orphaned cost may be attached to: one page, apart from the lists,
  // whose pages the details read.
  allCandidates: () => ['charge-candidates'] as const,
  candidates: (vehicle: string, from: string, to: string) =>
    ['charge-candidates', vehicle, from, to] as const,
}
