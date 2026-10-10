import { VueQueryPlugin } from '@tanstack/vue-query'
import { createApp } from 'vue'
import { createWebHistory } from 'vue-router'
import { i18n } from '@/shared/i18n'
import { onUnauthorized } from '@/shared/api'
import App from './App.vue'
import { beforeStart } from './extensions'
import { createQueryClient } from './providers/query'
import { vuetify } from './providers/vuetify'
import { createAppRouter } from './routes'

// Before anything reads the API or the router starts: a downstream build's preparations.
await beforeStart()

const queryClient = createQueryClient()
// The base of the document: runsten-web writes the reverse proxy's prefix into it.
const router = createAppRouter(createWebHistory(new URL(document.baseURI).pathname), queryClient)

// The session ended while the app was open: back to the sign-in, then to this page.
onUnauthorized(() => {
  const here = router.currentRoute.value
  if (here.meta.public) return
  queryClient.clear()
  void router.push({ name: 'login', query: { next: here.fullPath } })
})

document.documentElement.lang = i18n.global.locale.value
createApp(App).use(i18n).use(vuetify).use(VueQueryPlugin, { queryClient }).use(router).mount('#app')
