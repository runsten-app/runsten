<script setup lang="ts">
import { mdiCarOff } from '@mdi/js'
import { computed, ref, watchEffect } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useConnectionSettings } from '@/features/manage-api-key'
import { useFlag } from '@/features/view-flags'
import { useVehicles } from '@/features/select-vehicle'
import { AppShell } from '@/widgets/app-shell'
import { extensionChoices, extensionMakeMentions, type HomeMake } from '../extensions'

// Home leads to the first vehicle of the account; without one, it asks which car, Volvo,
// Polestar, Tesla, BMW or Hyundai, Kia and Genesis (one key per brand, all read through
// Pleos), choices of equal weight, and says how Runsten reads each one
// offered, a part per brand joined as the language lists. A Volvo leads to the Volvo ID
// connection, or first to the Connection page while the account's key is missing: without
// it, runsten-api refuses to start the flow (an instance without a key of its own). A
// Polestar, a Tesla, a BMW or a Hyundai leads to the Connection page's tab of its brand,
// which takes its credential, its refresh token, its client ID or its key. On an instance without a Volvo
// application, the Volvo is left out; the Tesla, while the flag source.tesla is off for the
// account; the BMW, while source.bmw is; the Hyundai, while source.pleos is. A downstream build may add its own choices after
// the makes, and a mention on a make's button (extensions.ts).
const { t, locale } = useI18n()
const router = useRouter()
const { vehicles, isPending, error } = useVehicles()
const { connection } = useConnectionSettings()
const noVolvo = computed(() => connection.value?.volvo === false)
const teslaOn = useFlag('source.tesla')
const bmwOn = useFlag('source.bmw')
const pleosOn = useFlag('source.pleos')
const keyMissing = computed(
  () => !!connection.value && !connection.value.instance_key && !connection.value.api_key,
)
const mentions = extensionMakeMentions?.() ?? ref<Partial<Record<HomeMake, string>>>({})

// choose is a make's button's label, with its mention if any.
function choose(make: HomeMake): string {
  const label = {
    volvo: t('home.choose.volvo'),
    polestar: t('home.choose.polestar'),
    tesla: t('home.choose.tesla'),
    bmw: t('home.choose.bmw'),
    pleos: t('home.choose.pleos'),
  }[make]
  const mention = mentions.value[make]
  return mention ? `${label} · ${mention}` : label
}

const text = computed(() => {
  const makes = [
    ...(noVolvo.value ? [] : [keyMissing.value ? t('home.empty.volvoKey') : t('home.empty.volvo')]),
    t('home.empty.polestar'),
    ...(teslaOn.value ? [t('home.empty.tesla')] : []),
    ...(bmwOn.value ? [t('home.empty.bmw')] : []),
    ...(pleosOn.value ? [t('home.empty.pleos')] : []),
  ]
  const list = new Intl.ListFormat(locale.value, { type: 'conjunction' }).format(makes)
  const parts = [t('home.empty.ask'), t('home.empty.reads', { makes: list })]
  if (noVolvo.value) parts.push(t('home.empty.noVolvo'))
  return parts.join(' ')
})

watchEffect(() => {
  const first = vehicles.value?.[0]
  if (first) void router.replace({ name: 'vehicle', params: { vehicle: first.id } })
})
</script>

<template>
  <AppShell>
    <v-alert v-if="error" type="error" variant="tonal">{{ t('home.error') }}</v-alert>
    <v-progress-linear v-else-if="isPending || vehicles?.length" indeterminate />
    <v-empty-state v-else :icon="mdiCarOff" :headline="t('home.empty.title')" :text>
      <template #actions>
        <v-btn
          v-if="keyMissing && !noVolvo"
          :to="{ name: 'connection' }"
          color="primary"
          variant="flat"
          class="volvo"
          >{{ choose('volvo') }}</v-btn
        >
        <!-- A full page load: the Volvo ID flow is served by runsten-api. -->
        <v-btn
          v-else-if="!noVolvo"
          href="auth/volvo/start"
          color="primary"
          variant="flat"
          class="volvo"
          >{{ choose('volvo') }}</v-btn
        >
        <v-btn
          :to="{ name: 'connection', hash: '#polestar' }"
          color="primary"
          variant="flat"
          class="polestar"
          >{{ choose('polestar') }}</v-btn
        >
        <v-btn
          v-if="teslaOn"
          :to="{ name: 'connection', hash: '#tesla' }"
          color="primary"
          variant="flat"
          class="tesla"
          >{{ choose('tesla') }}</v-btn
        >
        <v-btn
          v-if="bmwOn"
          :to="{ name: 'connection', hash: '#bmw' }"
          color="primary"
          variant="flat"
          class="bmw"
          >{{ choose('bmw') }}</v-btn
        >
        <v-btn
          v-if="pleosOn"
          :to="{ name: 'connection', hash: '#pleos' }"
          color="primary"
          variant="flat"
          class="pleos"
          >{{ choose('pleos') }}</v-btn
        >
        <component :is="choice" v-for="(choice, i) in extensionChoices" :key="i" />
      </template>
    </v-empty-state>
  </AppShell>
</template>

<style scoped>
/* The makes' buttons wrap within the page: five of them, and an extension's, are wider
   than a phone. A label too long for the width wraps too. */
.v-empty-state :deep(.v-empty-state__actions) {
  flex-wrap: wrap;
  justify-content: center;
  max-width: 100%;
}
.v-empty-state :deep(.v-empty-state__actions .v-btn) {
  max-width: 100%;
  height: auto;
  min-height: var(--v-btn-height);
}
.v-empty-state :deep(.v-empty-state__actions .v-btn__content) {
  white-space: normal;
}
</style>
