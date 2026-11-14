<script setup lang="ts">
import { mdiCarOff } from '@mdi/js'
import { computed, defineAsyncComponent, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, type RouteLocationRaw } from 'vue-router'
import { useConnectionSettings } from '@/features/manage-api-key'
import { useFlag } from '@/features/view-flags'
import { useVehicles } from '@/features/select-vehicle'
import { AppShell } from '@/widgets/app-shell'
import { extensionChoices, extensionMakeMentions, type HomeMake } from '../extensions'

// Home leads to the vehicle of an account that has one; with several, it is the Garage,
// which compares them (loaded on demand: Chart.js comes with it). Without one, it asks which
// car, one tile per make, of equal weight: Volvo, Polestar, Tesla, BMW, MINI, Hyundai,
// Kia, Genesis, Škoda. How each is read (an ID, a credential, a key) is told by its brand's tab: the
// owner picks their make and is guided there, never told that two makes share an API. A
// Volvo needs the account's key first: without it, runsten-api refuses to start the flow
// (an instance without a key of its own). On an instance without a Volvo application, the
// Volvo is left out; the Tesla, while the flag source.tesla is off for the account; the BMW
// and the MINI, while source.bmw is; Hyundai, Kia and Genesis, while source.pleos is; Škoda,
// while source.skoda is; Renault, while source.renault is (it also reads Dacia and Alpine,
// told on its tab). A
// downstream build may add its own choices under the makes, and a mention on a make's
// tile (extensions.ts).
const { t } = useI18n()
const router = useRouter()
const { vehicles, isPending, error } = useVehicles()
const { connection } = useConnectionSettings()
const noVolvo = computed(() => connection.value?.volvo === false)
const teslaOn = useFlag('source.tesla')
const bmwOn = useFlag('source.bmw')
const pleosOn = useFlag('source.pleos')
const skodaOn = useFlag('source.skoda')
const renaultOn = useFlag('source.renault')
const keyMissing = computed(
  () => !!connection.value && !connection.value.instance_key && !connection.value.api_key,
)
const mentions = extensionMakeMentions?.() ?? ref<Partial<Record<HomeMake, string>>>({})

// The makes offered, in the public site's order, each a tile leading to where it is
// connected. A Volvo leads to the Volvo ID flow, a full page load (runsten-api serves it),
// or to the Connection page while the account's key is missing; a MINI to BMW's tab, the
// same access; any other make to its own tab. Its note under it: the extension's mention,
// else Beta for a make read in beta.
type Tile = { make: HomeMake; href?: string; to?: RouteLocationRaw; beta?: boolean }
const tiles = computed<Tile[]>(() => [
  ...(noVolvo.value
    ? []
    : [
        keyMissing.value
          ? { make: 'volvo' as const, to: { name: 'connection' } }
          : { make: 'volvo' as const, href: 'auth/volvo/start' },
      ]),
  { make: 'polestar', to: { name: 'connection', hash: '#polestar' } },
  ...(teslaOn.value
    ? [{ make: 'tesla' as const, to: { name: 'connection', hash: '#tesla' } }]
    : []),
  ...(bmwOn.value
    ? (['bmw', 'mini'] as const).map((make) => ({
        make,
        to: { name: 'connection', hash: '#bmw' },
      }))
    : []),
  ...(pleosOn.value
    ? (['hyundai', 'kia', 'genesis'] as const).map((make) => ({
        make,
        to: { name: 'connection', hash: `#${make}` },
        beta: true,
      }))
    : []),
  ...(skodaOn.value
    ? [{ make: 'skoda' as const, to: { name: 'connection', hash: '#skoda' }, beta: true }]
    : []),
  ...(renaultOn.value
    ? [{ make: 'renault' as const, to: { name: 'connection', hash: '#renault' }, beta: true }]
    : []),
])
// The makes, written out: an interpolated key is invisible to the unused-key rule.
const names = computed<Record<HomeMake, string>>(() => ({
  volvo: t('home.make.volvo'),
  polestar: t('home.make.polestar'),
  tesla: t('home.make.tesla'),
  bmw: t('home.make.bmw'),
  mini: t('home.make.mini'),
  hyundai: t('home.make.hyundai'),
  kia: t('home.make.kia'),
  genesis: t('home.make.genesis'),
  skoda: t('home.make.skoda'),
  renault: t('home.make.renault'),
}))
const note = (tile: Tile) => mentions.value[tile.make] ?? (tile.beta ? t('home.beta') : undefined)

const text = computed(() =>
  [
    t('home.empty.ask'),
    t('home.empty.guided'),
    ...(noVolvo.value ? [t('home.empty.noVolvo')] : []),
  ].join(' '),
)

const GaragePage = defineAsyncComponent(() => import('./GaragePage.vue'))
const garage = computed(() => (vehicles.value?.length ?? 0) >= 2)

watchEffect(() => {
  const only = vehicles.value?.length === 1 ? vehicles.value[0] : undefined
  if (only) void router.replace({ name: 'vehicle', params: { vehicle: only.id } })
})
</script>

<template>
  <AppShell :garage>
    <v-alert v-if="error" type="error" variant="tonal">{{ t('home.error') }}</v-alert>
    <GaragePage v-else-if="garage && vehicles" :vehicles />
    <v-progress-linear v-else-if="isPending || vehicles?.length" indeterminate />
    <v-empty-state v-else :icon="mdiCarOff" :headline="t('home.empty.title')" :text>
      <template #actions>
        <ul class="makes" :aria-label="t('home.makes')">
          <li v-for="tile in tiles" :key="tile.make">
            <v-card :href="tile.href" :to="tile.to" :class="['make', tile.make]">
              <span class="name text-title-medium">{{ names[tile.make] }}</span>
              <!-- Heard between the make and its note, which would run into each other. -->
              <span v-if="note(tile)" class="d-sr-only">, </span>
              <span
                v-if="note(tile)"
                :class="['note', 'text-label-small', { mention: !!mentions[tile.make] }]"
                >{{ note(tile) }}</span
              >
            </v-card>
          </li>
        </ul>
        <component :is="choice" v-for="(choice, i) in extensionChoices" :key="i" />
      </template>
    </v-empty-state>
  </AppShell>
</template>

<style scoped>
/* The makes as tiles of equal weight, the app's filled surfaces: four a row, two on a
   phone. An extension's choice comes under them. */
.v-empty-state :deep(.v-empty-state__actions) {
  flex-direction: column;
  align-items: center;
  width: 100%;
}
.makes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
  width: 100%;
  max-width: 38rem;
  margin: 0;
  padding: 0;
  list-style: none;
}
@media (min-width: 600px) {
  .makes {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
.make {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 0.25rem;
  height: 100%;
  min-height: 4.25rem;
  padding: 0.75rem 0.5rem;
  text-align: center;
}
.name {
  font-weight: 600;
}
.note {
  color: rgba(var(--v-theme-on-surface), var(--v-medium-emphasis-opacity));
}
.note.mention {
  color: rgb(var(--v-theme-primary));
}
</style>
