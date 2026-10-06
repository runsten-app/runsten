import type { QueryClient } from '@tanstack/vue-query'
import { createRouter, type LocationQueryValue, type RouterHistory } from 'vue-router'
import { fetchSession, sessionKeys } from '@/entities/session'
import { ChargePage } from '@/pages/charge'
import { ChargesPage } from '@/pages/charges'
import { HomePage } from '@/pages/home'
import { LoginPage } from '@/pages/login'
import { TripPage } from '@/pages/trip'
import { TripsPage } from '@/pages/trips'
import { VehiclePage } from '@/pages/vehicle'
import { isApiError } from '@/shared/api'
import { parseDecimal } from '@/shared/lib'
import { extensionGuards, extensionRoutes } from './extensions'

declare module 'vue-router' {
  interface RouteMeta {
    // Reachable without a session.
    public?: boolean
  }
}

type QueryValue = LocationQueryValue | LocationQueryValue[] | undefined

const text = (v: QueryValue) => (typeof v === 'string' && v ? v : undefined)
const coordinate = (v: QueryValue) => {
  const s = text(v)
  return s === undefined ? undefined : (parseDecimal(s) ?? undefined)
}

export function createAppRouter(history: RouterHistory, queryClient: QueryClient) {
  const router = createRouter({
    history,
    routes: [
      { path: '/login', name: 'login', component: LoginPage, meta: { public: true } },
      { path: '/', name: 'home', component: HomePage },
      { path: '/vehicles/:vehicle', name: 'vehicle', component: VehiclePage, props: true },
      // A list and its details share a parent, so that the list's tab stays active on
      // the details. The router encodes the event ID in the path (its ':' included, as
      // encodeURI does) and gives it back decoded: an encodeURIComponent of ours would
      // be encoded twice.
      {
        path: '/vehicles/:vehicle/trips',
        children: [
          { path: '', name: 'trips', component: TripsPage, props: true },
          { path: ':id', name: 'trip', component: TripPage, props: true },
        ],
      },
      {
        path: '/vehicles/:vehicle/charges',
        children: [
          { path: '', name: 'charges', component: ChargesPage, props: true },
          { path: ':id', name: 'charge', component: ChargePage, props: true },
        ],
      },
      // Loaded on demand: the charts' library weighs on this page only.
      {
        path: '/vehicles/:vehicle/stats',
        name: 'stats',
        component: () => import('@/pages/stats').then((m) => m.StatsPage),
        props: true,
      },
      // The battery's estimated capacity, loaded on demand like the statistics.
      {
        path: '/vehicles/:vehicle/battery',
        name: 'battery',
        component: () => import('@/pages/battery').then((m) => m.BatteryPage),
        props: true,
      },
      // A vehicle's own settings (its model), on demand like the statistics.
      {
        path: '/vehicles/:vehicle/settings',
        name: 'vehicleSettings',
        component: () => import('@/pages/vehicle-settings').then((m) => m.VehicleSettingsPage),
        props: true,
      },
      // The account's settings, loaded on demand like the statistics: most visits never
      // open them. They belong to no vehicle: none is kept in the query.
      {
        path: '/settings',
        name: 'settings',
        component: () => import('@/pages/settings').then((m) => m.SettingsPage),
      },
      // The user's own preferences, kept in the browser: on demand, like the settings.
      {
        path: '/preferences',
        name: 'preferences',
        component: () => import('@/pages/preferences').then((m) => m.PreferencesPage),
      },
      // How the collector reads the vehicles, and the Volvo ID: on demand, like the
      // settings.
      {
        path: '/connection',
        name: 'connection',
        component: () => import('@/pages/connection').then((m) => m.ConnectionPage),
        props: (to) => ({ vehicle: text(to.query.vehicle), outcome: text(to.query.volvo) }),
      },
      // One record for a new place ("new") and an existing one: once created, the URL
      // takes its ID and the same form stays, with its focus. A new place may start at
      // lat and lon (from a charge).
      {
        path: '/settings/places/:place',
        name: 'place',
        component: () => import('@/pages/place').then((m) => m.PlacePage),
        props: (to) => ({
          place: to.params.place === 'new' ? undefined : String(to.params.place),
          lat: coordinate(to.query.lat),
          lon: coordinate(to.query.lon),
        }),
      },
      ...extensionRoutes,
      { path: '/:rest(.*)*', redirect: '/' },
    ],
  })

  // Every page but the sign-in needs a session: load it (once, then from the cache)
  // before the page, as the GED front end does with its project guard. The extensions'
  // guards come after it, with the session.
  router.beforeEach(async (to) => {
    if (to.meta.public) return true
    try {
      await queryClient.fetchQuery({
        queryKey: sessionKeys.current(),
        queryFn: fetchSession,
        staleTime: 5 * 60_000,
      })
    } catch (e) {
      if (isApiError(e, 'unauthorized')) return { name: 'login', query: { next: to.fullPath } }
      throw e
    }
    for (const guard of extensionGuards) {
      const elsewhere = await guard(to, queryClient)
      if (elsewhere !== true) return elsewhere
    }
    return true
  })
  return router
}
