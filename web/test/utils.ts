import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount, type ComponentMountingOptions } from '@vue/test-utils'
import { h, type Component } from 'vue'
import { createMemoryHistory, createRouter, type Router, type RouteRecordRaw } from 'vue-router'
import { VApp } from 'vuetify/components'
import { vuetify } from '@/app/providers/vuetify'
import { i18n } from '@/shared/i18n'

export function testQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

// testRouter has the app's routes, with empty pages; routes of the same name replace them.
export function testRouter(routes: RouteRecordRaw[] = []): Router {
  const Empty = { render: () => null }
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', name: 'home', component: Empty },
      { path: '/login', name: 'login', component: Empty },
      { path: '/vehicles/:vehicle', name: 'vehicle', component: Empty },
      { path: '/vehicles/:vehicle/trips', name: 'trips', component: Empty },
      { path: '/vehicles/:vehicle/trips/:id', name: 'trip', component: Empty },
      { path: '/vehicles/:vehicle/charges', name: 'charges', component: Empty },
      { path: '/vehicles/:vehicle/charges/:id', name: 'charge', component: Empty },
      { path: '/vehicles/:vehicle/stats', name: 'stats', component: Empty },
      { path: '/vehicles/:vehicle/battery', name: 'battery', component: Empty },
      { path: '/vehicles/:vehicle/settings', name: 'vehicleSettings', component: Empty },
      { path: '/settings', name: 'settings', component: Empty },
      { path: '/preferences', name: 'preferences', component: Empty },
      { path: '/connection', name: 'connection', component: Empty },
      { path: '/settings/places/:place', name: 'place', component: Empty },
      ...routes,
    ],
  })
}

type Options = {
  queryClient?: QueryClient
  router?: Router
  props?: Record<string, unknown>
  // In the document, for the focus.
  attachTo?: HTMLElement
}

// mountWith mounts a component inside a v-app, with the app's plugins, a query client
// and a router.
export function mountWith(c: Component, options: Options = {}) {
  const queryClient = options.queryClient ?? testQueryClient()
  const router = options.router ?? testRouter()
  const wrapper = mount({ render: () => h(VApp, () => h(c, options.props)) }, {
    global: { plugins: [i18n, vuetify, [VueQueryPlugin, { queryClient }], router] },
    attachTo: options.attachTo,
  } as ComponentMountingOptions<Component>)
  return { wrapper, queryClient, router }
}

// withSetup runs a composable in the setup of a host component, mounted like mountWith,
// and returns what it returned.
export function withSetup<T>(
  composable: () => T,
  options: Omit<Options, 'props' | 'attachTo'> = {},
) {
  let result!: T
  const Host = {
    setup() {
      result = composable()
      return () => null
    },
  }
  const mounted = mountWith(Host, options)
  return { result, ...mounted }
}

const plainText = (s: string | null) => (s ?? '').replace(/\s+/g, ' ').trim()

// seen is an element's text as it is shown, heard as a screen reader reads it: SpokenText
// gives each its own words (a range of amounts is read "from … to …").
export function seen(el: Element): string {
  const c = el.cloneNode(true) as Element
  c.querySelectorAll('.d-sr-only').forEach((n) => n.remove())
  return plainText(c.textContent)
}

export function heard(el: Element): string {
  const c = el.cloneNode(true) as Element
  c.querySelectorAll('[aria-hidden="true"]').forEach((n) => n.remove())
  return plainText(c.textContent)
}
