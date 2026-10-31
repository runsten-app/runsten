<script setup lang="ts">
import {
  mdiChartBoxOutline,
  mdiCogOutline,
  mdiConnection,
  mdiEvPlugType2,
  mdiGauge,
  mdiHeartPulse,
  mdiKeyboardOutline,
  mdiMapMarkerPath,
} from '@mdi/js'
import { computed, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { useDisplay } from 'vuetify'
import { VAppBar, VBottomNavigation } from 'vuetify/components'
import { needsAttention as healthNeedsAttention } from '@/entities/health'
import { collectionState, needsAttention } from '@/entities/vehicle'
import { UserMenu } from '@/features/auth'
import { ShortcutsHelp, shortcutsEnabled, useShortcuts } from '@/features/keyboard-shortcuts'
import { periodQuery } from '@/features/filter-period'
import { placeQuery } from '@/features/filter-place'
import { useConnectionSettings } from '@/features/manage-api-key'
import { renewalDue } from '@/features/manage-polestar'
import { rememberedVehicle, rememberVehicle, VehicleMenu } from '@/features/select-vehicle'
import { useHealth } from '@/features/view-health'
import { useLimits } from '@/features/view-limits'
import { CollectionNotice, useCollections } from '@/features/watch-collection'
import { useNow } from '@/shared/lib'
import { AttentionDot } from '@/shared/ui'
import { extensionMenuItems, extensionNotices } from '../extensions'

// With a vehicle, the bar leads to its state (Verdandi) and its health, its battery's
// included, its trips and its charges (Urd), and their statistics. Tabs on every screen: a single navigation stays at
// the same place from one width to another: under the bar, and at the bottom of a phone,
// within the thumb's reach. The icon goes above the label, and on a phone the tabs shrink
// below Vuetify's 90 px: five labels side by side do not fit 360 px in Swedish otherwise,
// and icons alone would say nothing. Every page tells first the extensions' notices, if
// any; every page of a vehicle then warns when nothing new is read of it. The digits lead
// to the tabs, in their order, "," to the settings; Shift held alone shows them, "?" lists
// them. The settings, the connection and the user's own pages share one menu. The bar
// names the vehicle on every page, the account's included: the one seen last, else the
// first; its menu leads to the account's other vehicles and to its settings. The connection is looked at when the reading fails, or the Polestar
// credential is to be renewed: a dot on the menu and on it says so, from every page. A dot
// on the health's tab says the vehicle needs attention (a tyre, the service, a warning),
// from every page of the vehicle.
const props = defineProps<{ vehicle?: string }>()
const { t } = useI18n()
const { unread } = useLimits()
const route = useRoute()
const { xs } = useDisplay()

type Section = 'state' | 'health' | 'trips' | 'charges' | 'stats'
const sections: Record<string, Section> = {
  vehicle: 'state',
  health: 'health',
  // The battery's estimated capacity is a figure of the health, reached from its page.
  battery: 'health',
  // So is the history of its software.
  software: 'health',
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
watchEffect(() => {
  if (props.vehicle) rememberVehicle(props.vehicle)
})
// The vehicle of the bar and of the digits: the page's, else, among the account's, the one
// the connection was opened from, the one seen last, the first.
const shown = computed(() => {
  const listed = (id?: string) => (vehicles.value?.some((v) => v.id === id) ? id : undefined)
  return (
    props.vehicle ??
    listed(from.value.vehicle) ??
    listed(rememberedVehicle.value) ??
    vehicles.value?.[0]?.id
  )
})
const { connection: connectionSettings } = useConnectionSettings()
// A vehicle the offer leaves unread is no failure: its notice tells it, on its pages.
const now = useNow()
const attention = computed(() =>
  vehicles.value?.some((v) => {
    const s = collectionState(v, now.value, unread(v.id))
    return s.kind !== 'unread' && needsAttention(s)
  }) || renewalDue(connectionSettings.value?.polestar, now.value)
    ? t('connection.attention')
    : undefined,
)
// The health is read on a vehicle's pages only: beyond them, the tabs are not shown.
const { health } = useHealth(
  () => props.vehicle ?? '',
  () => !!props.vehicle,
)
const healthAttention = computed(() =>
  health.value?.checked_at && healthNeedsAttention(health.value)
    ? t('health.attention')
    : undefined,
)
function sectionsOf(vehicle: string) {
  const params = { vehicle }
  // The lists keep the period from one to the other.
  const query = periodQuery(route.query)
  return [
    { value: 'state', icon: mdiGauge, label: t('nav.state'), to: { name: 'vehicle', params } },
    {
      value: 'health',
      icon: mdiHeartPulse,
      label: t('nav.health'),
      to: { name: 'health', params },
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
      // The place too, from a charge back to its list.
      to: { name: 'charges', params, query: { ...query, ...placeQuery(route.query) } },
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
// Beyond a vehicle's pages the digits still lead to the sections of the bar's vehicle: "1"
// is the way back, as its menu is.
const targets = computed(() => (shown.value ? sectionsOf(shown.value) : []))
const { help, hints } = useShortcuts(() => targets.value.map((s) => s.to))
const keys = (key: string) => (shortcutsEnabled.value ? key : undefined)
// The key of the list of the keys, a character on every layout.
const helpKey = '?'
</script>

<template>
  <v-app-bar flat color="background" height="64" class="app-bar">
    <div class="bar d-flex align-center ga-3 ga-sm-4">
      <!-- On a phone with a vehicle, the mark alone: the vehicle's name needs the width. -->
      <router-link :to="{ name: 'home' }" class="brand">
        <span class="mark" aria-hidden="true"><span /><span /><span /><span /></span>
        <span class="wordmark" :class="{ 'd-sr-only': xs && shown }">Runsten</span>
      </router-link>
      <VehicleMenu v-if="shown" :vehicle="shown" class="picker" />
      <v-spacer />
      <UserMenu :attention>
        <div role="listitem">
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
        </div>
        <div role="listitem">
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
        </div>
        <div v-for="(item, i) in extensionMenuItems" :key="i" role="listitem">
          <component :is="item" />
        </div>
        <!-- A phone has no keys to list. -->
        <template v-if="shortcutsEnabled && !xs" #own>
          <div role="listitem">
            <v-list-item
              :prepend-icon="mdiKeyboardOutline"
              :title="t('shortcuts.title')"
              :aria-keyshortcuts="keys(helpKey)"
              role="button"
              class="shortcuts"
              @click="help = true"
            >
              <template #append>
                <kbd class="menu-key" aria-hidden="true">{{ helpKey }}</kbd>
              </template>
            </v-list-item>
          </div>
        </template>
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
    tag="div"
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
          :aria-describedby="
            tab.value === 'health' && healthAttention ? 'health-attention' : undefined
          "
        >
          {{ tab.label }}
          <!-- Out of the tab's name, which stays its label: its description. -->
          <AttentionDot
            v-if="tab.value === 'health' && healthAttention"
            id="health-attention"
            :label="healthAttention"
            aria-hidden="true"
            class="tab-dot"
          />
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
/* Installed on a phone (viewport-fit=cover, index.html), the page runs under the home
   indicator, and in landscape beside the notch: the env() insets, 0 elsewhere, keep the
   content clear of them. */
.bar {
  padding-inline: max(24px, env(safe-area-inset-left)) max(24px, env(safe-area-inset-right));
}
.shell {
  padding: 16px max(24px, env(safe-area-inset-right)) calc(28px + env(safe-area-inset-bottom))
    max(24px, env(safe-area-inset-left));
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
  flex: 0 1 auto;
  min-inline-size: 0;
  max-inline-size: 26rem;
}
.section-bar.top {
  border-block-end: 1px solid rgb(var(--v-theme-line));
}
.section-bar.bottom {
  border-block-start: 1px solid rgb(var(--v-theme-line));
  /* Above the home indicator: Vuetify sets the height inline, and lays v-main out by the
     prop's, which the shell's own inset completes. */
  block-size: calc(64px + env(safe-area-inset-bottom)) !important;
  padding-block-end: env(safe-area-inset-bottom);
}
.sections {
  padding-inline: max(24px, env(safe-area-inset-left)) max(24px, env(safe-area-inset-right));
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
  padding-inline: env(safe-area-inset-left) env(safe-area-inset-right);
}
.sections.narrow :deep(.v-tab) {
  min-inline-size: 0;
  padding-inline: 2px;
  font-size: 0.75rem;
}
/* The keys, while Shift is held: at the icon's corner, the one the health's dot leaves. */
.key-hint {
  position: absolute;
  inset-block-start: 6px;
  inset-inline-end: calc(50% + 10px);
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
/* The health's dot, at the icon's corner. */
.tab-dot {
  position: absolute;
  inset-block-start: 10px;
  inset-inline-start: calc(50% + 8px);
}
/* A key, as a menu tells its shortcuts: the shortcuts' list's, smaller. */
.menu-key {
  display: inline-block;
  min-inline-size: 1.5rem;
  padding: 0 6px;
  border-radius: var(--v-control-radius);
  background: rgb(var(--v-theme-surface-variant));
  color: rgb(var(--v-theme-on-surface));
  font-family: inherit;
  font-size: 0.75rem;
  font-weight: 500;
  line-height: 1.375rem;
  text-align: center;
}
@media (max-width: 599.98px) {
  .bar {
    padding-inline: max(10px, env(safe-area-inset-left)) max(10px, env(safe-area-inset-right));
  }
  .shell {
    padding: 10px max(10px, env(safe-area-inset-right)) calc(16px + env(safe-area-inset-bottom))
      max(10px, env(safe-area-inset-left));
  }
}
</style>
