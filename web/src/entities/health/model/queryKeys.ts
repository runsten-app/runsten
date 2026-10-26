import type { TyresQuery } from '../api/healthApi'

export const healthKeys = {
  all: (vehicle: string) => ['health', vehicle] as const,
  current: (vehicle: string) => ['health', vehicle, 'current'] as const,
  tyres: (vehicle: string, q: TyresQuery) =>
    ['health', vehicle, 'tyres', q.from, q.to ?? null] as const,
}
