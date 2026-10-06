import type { QueryClient } from '@tanstack/vue-query'
import type { RouteLocationNormalized, RouteLocationRaw } from 'vue-router'

// ExtensionGuard is an extension's check of a page a signed-in user opens, once their
// session is loaded: true lets them in, a location sends them there instead.
export type ExtensionGuard = (
  to: RouteLocationNormalized,
  queryClient: QueryClient,
) => Promise<true | RouteLocationRaw>
