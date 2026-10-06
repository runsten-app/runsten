<script setup lang="ts">
import {
  mdiBatteryHeartOutline,
  mdiChartBoxOutline,
  mdiCogOutline,
  mdiConnection,
  mdiEvPlugType2,
  mdiGauge,
  mdiMapMarkerPath,
} from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useDisplay } from 'vuetify'
import { VAppBar, VBottomNavigation } from 'vuetify/components'
import { collectionState, needsAttention } from '@/entities/vehicle'
import { UserMenu } from '@/features/auth'
import { ShortcutsHelp, shortcutsEnabled, useShortcuts } from '@/features/keyboard-shortcuts'
import { periodQuery } from '@/features/filter-period'
import { VehicleSelect } from '@/features/select-vehicle'
import { useLimits } from '@/features/view-limits'
import { CollectionNotice, useCollections } from '@/features/watch-collection'
import { useNow } from '@/shared/lib'
import { AttentionDot } from '@/shared/ui'
import { extensionMenuItems, extensionNotices } from '../extensions'

// With a vehicle, the bar leads to its state (Verdandi) and its battery, its trips and its
// charges (Urd), and their statistics. Tabs on every screen: a single navigation stays at
// the same place from one width to another: under the bar, and at the bottom of a phone,
// within the thumb's reach. The icon goes above the label, and on a phone the tabs shrink
// below Vuetify's 90 px: five labels side by side do not fit 360 px in Swedish otherwise,
// and icons alone would say nothing. Every page tells first the extensions' notices, if
// any; every page of a vehicle then warns when nothing new is read of it. The digits lead
// to the tabs, in their order, "," to the settings; Shift held alone shows them, "?" lists
// them. The settings, the connection and the user's own pages share one menu: the bar
// keeps the vehicle. The connection is looked at when the reading fails: a dot on the
// menu and on it says so, from every page.
const props = defineProps<{ vehicle?: string }>()
const { t } = useI18n()
const { unread } = useLimits()
const route = useRoute()
const { xs } = useDisplay()

type Section = 'state' | 'battery' | 'trips' | 'charges' | 'stats'
const sections: Record<string, Section> = {
  vehicle: 'state',
  battery: 'battery',
  trips: 'trips',
  trip: 'trips',
  charges: 'charges',
  charge: 'charges',
  stats: 'stats',
}
const current = computed(() => sections[String(route.name)])
// The connection, opened from a vehicle, leads back to it; within it, the vehicle it was
// opened from stays. The settings are the account's: they belong to no vehicle.
const from = computed(() => {
  const vehicle = props.vehicle ?? route.query.vehicle
  return typeof vehicle === 'string' ? { vehicle } : {}
})
const settings = { name: 'settings' }
const connection = computed(() => ({ name: 'connection', query: from.value }))
const { vehicles } = useCollections()
// A vehicle the offer leaves unread is no failure: its notice tells it, on its pages.
const now = useNow()
const attention = computed(() =>
  vehicles.value?.some((v) => {
    const s = collectionState(v, now.value, unread(v.id))
    return s.kind !== 'unread' && needsAttention(s)
  })
    ? t('connection.attention')
    : undefined,
)
function sectionsOf(vehicle: string) {
  const params = { vehicle }
  // The lists keep the period from one to the other.
  const query = periodQuery(route.query)
  return [
    { value: 'state', icon: mdiGauge, label: t('nav.state'), to: { name: 'vehicle', params } },
    {
      value: 'battery',
      icon: mdiBatteryHeartOutline,
      label: t('nav.battery'),
      to: { name: 'battery', params },
    },
    {
      value: 'trips',
      icon: mdiMapMarkerPath,
      label: t('nav.trips'),
      to: { name: 'trips', params, query },
    },
    {
      value: 'charges',
      icon: mdiEvPlugType2,
      label: t('nav.charges'),
      to: { name: 'charges', params, query },
    },
    {
      value: 'stats',
      icon: mdiChartBoxOutline,
      label: t('nav.stats'),
      to: { name: 'stats', params, query },
    },
  ]
}
const tabs = computed(() => (props.vehicle ? sectionsOf(props.vehicle) : []))
// Beyond a vehicle's pages the digits still lead to its sections: the vehicle the page was
// opened from, else the first one, as the home page does. "1" is the way back.
const targets = computed(() => {
  const vehicle = props.vehicle ?? from.value.vehicle ?? vehicles.value?.[0]?.id
  return vehicle ? sectionsOf(vehicle) : []
})
const { help, hints } = useShortcuts(() => targets.value.map((s) => s.to))
const keys = (key: string) => (shortcutsEnabled.value ? key : undefined)
</script>

<template>
  <v-app-bar flat color="background" height="64" class="app-bar">
    <div class="bar d-flex align-center ga-3 ga-sm-4">
      <!-- On a phone with a vehicle, the mark alone: the picker needs the width. -->
      <router-link :to="{ name: 'home' }" class="brand">
        <span class="mark" aria-hidden="true"><span /><span /><span /><span /></span>
        <span class="wordmark" :class="{ 'd-sr-only': xs && vehicle }">Runsten</span>
      </router-link>
      <VehicleSelect v-if="vehicle" :vehicle class="picker" />
      <v-spacer />
      <UserMenu :attention>
        <v-list-item
          :to="settings"
          :prepend-icon="mdiCogOutline"
          :title="t('settings.open')"
          :aria-keyshortcuts="keys(',')"
          class="settings"
        >
          <template v-if="shortcutsEnabled" #append>
            <kbd class="menu-key" aria-hidden="true">,</kbd>
          </template>
        </v-list-item>
        <v-list-item
          :to="connection"
          :prepend-icon="mdiConnection"
          :title="t('connection.open')"
          class="connection"
        >
          <template v-if="attention" #append>
            <AttentionDot :label="attention" />
          </template>
        </v-list-item>
        <component :is="item" v-for="(item, i) in extensionMenuItems" :key="i" />
      </UserMenu>
    </div>
  </v-app-bar>
  <component
    :is="xs ? VBottomNavigation : VAppBar"
    v-if="tabs.length"
    flat
    color="background"
    :height="xs ? 64 : 68"
    :class="xs ? 'section-bar bottom' : 'section-bar top'"
    v-bind="xs ? { grow: true, active: true } : {}"
  >
    <!-- Links to pages, not tabs over panels: v-tabs would make them a tablist with a
         roving tabindex (arrow keys only). Its look stays, the semantics of a <nav>
         come back: plain links, each reached with Tab, the page shown aria-current. -->
    <nav class="w-100" :aria-label="t('nav.label')">
      <v-tabs
        :model-value="current"
        :mandatory="false"
        stacked
        :grow="xs"
        :height="xs ? 64 : 68"
        slider-color="primary"
        class="sections"
        :class="{ narrow: xs }"
        role="none"
      >
        <v-tab
          v-for="(tab, i) in tabs"
          :key="tab.value"
          :value="tab.value"
          :to="tab.to"
          :prepend-icon="tab.icon"
          :role="undefined"
          :tabindex="undefined"
          :aria-selected="undefined"
          :aria-current="tab.value === current ? 'page' : undefined"
          :aria-keyshortcuts="keys(String(i + 1))"
        >
          {{ tab.label }}
          <kbd v-if="hints" class="key-hint" aria-hidden="true">{{ i + 1 }}</kbd>
        </v-tab>
      </v-tabs>
    </nav>
  </component>
  <v-main>
    <div class="shell">
      <component :is="notice" v-for="(notice, i) in extensionNotices" :key="i" />
      <CollectionNotice v-if="vehicle" :vehicle :unread="unread(vehicle)" />
      <slot />
    </div>
  </v-main>
  <ShortcutsHelp v-model="help" :sections="targets.map((s) => s.label)" />
</template>

<style scoped>
.bar,
.sections,
.shell {
  inline-size: 100%;
  max-inline-size: 1280px;
  margin-inline: auto;
}
.bar {
  padding-inline: 24px;
}
.shell {
  padding: 16px 24px 28px;
}
.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: inherit;
  text-decoration: none;
}
/* The mark: four tiles, one in the accent. */
.mark {
  display: grid;
  grid-template-columns: repeat(2, 9px);
  gap: 2px;
}
.mark > span {
  block-size: 9px;
  border-radius: 2px;
  background: rgb(var(--v-theme-on-surface));
}
.mark > span:nth-child(2) {
  background: rgb(var(--v-theme-primary));
}
.wordmark {
  font-size: 19px;
  font-weight: 600;
  letter-spacing: -0.01em;
  text-transform: lowercase;
}
.picker {
  flex: 0 1 26rem;
  min-inline-size: 0;
}
@media (max-width: 599.98px) {
  /* The picker takes the width the spacer would. */
  .picker {
    flex: 100 1 auto;
  }
}
.section-bar.top {
  border-block-end: 1px solid rgb(var(--v-theme-line));
}
.section-bar.bottom {
  border-block-start: 1px solid rgb(var(--v-theme-line));
}
.sections {
  padding-inline: 24px;
}
.sections :deep(.v-tab) {
  min-inline-size: 116px;
  font-size: 0.8125rem;
  letter-spacing: 0;
  color: rgb(var(--v-theme-text-secondary));
}
.sections :deep(.v-tab[aria-current='page']) {
  color: rgb(var(--v-theme-on-surface));
  font-weight: 500;
}
.sections.narrow {
  padding-inline: 0;
}
.sections.narrow :deep(.v-tab) {
  min-inline-size: 0;
  padding-inline: 2px;
  font-size: 0.75rem;
}
/* The keys, while Shift is held: over the corner of what they lead to. */
.key-hint {
  inset-block-start: auto;
  inset-block-end: -12px;
  inset-inline-end: 50%;
  translate: 50% 0;
}
.key-hint {
  position: absolute;
  inset-block-start: 4px;
  inset-inline-end: 4px;
  min-inline-size: 1.25rem;
  padding: 0 4px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-on-surface));
  color: rgb(var(--v-theme-surface));
  font-family: inherit;
  font-size: 0.75rem;
  font-weight: 600;
  line-height: 1.25rem;
  text-align: center;
  pointer-events: none;
}
/* The settings' key, as a menu tells its shortcuts. */
.menu-key {
  font-family: inherit;
  font-size: 0.75rem;
  color: rgb(var(--v-theme-text-secondary));
}
@media (max-width: 599.98px) {
  .bar {
    padding-inline: 10px;
  }
  .shell {
    padding: 10px 10px 16px;
  }
}
</style>
