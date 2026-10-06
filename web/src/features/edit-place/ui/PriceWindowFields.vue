<script setup lang="ts">
import { mdiArrowDown, mdiArrowUp, mdiDelete } from '@mdi/js'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { weekdays } from '@/entities/place'
import { useFormat } from '@/shared/lib'
import { dayPresets, windowSpan, type DayPreset, type WindowDraft } from '../model/draft'
import { field } from '../model/validate'

// PriceWindowFields is one time window of a price: its days, hours, months and price.
// The days and the months are each a fieldset of checkboxes, with a legend.
const w = defineModel<WindowDraft>({ required: true })
const props = defineProps<{
  version: number
  index: number
  count: number
  priceSuffix: string
  error: (id: string) => string | undefined
}>()
defineEmits<{ up: []; down: []; remove: [] }>()
const { t } = useI18n()
const f = useFormat()

const n = computed(() => props.index + 1)
const id = computed(() => ({
  days: field.days(props.version, props.index),
  from: field.windowFrom(props.version, props.index),
  to: field.windowTo(props.version, props.index),
  months: field.months(props.version, props.index),
  price: field.windowPrice(props.version, props.index),
  base: `place-v${props.version}-w${props.index}`,
}))
const presets = computed<{ value: DayPreset; label: string }[]>(() => [
  { value: 'everyDay', label: t('place.window.everyDay') },
  { value: 'weekdays', label: t('place.window.weekdays') },
  { value: 'weekend', label: t('place.window.weekend') },
])
const sameDays = (a: readonly string[], b: readonly string[]) =>
  a.length === b.length && a.every((d) => b.includes(d))
const span = computed(() => {
  const s = windowSpan(w.value)
  if (s === 'overnight') return t('place.window.overnight')
  if (s === 'allDay') return t('place.window.allDay')
  return ''
})
const months = Array.from({ length: 12 }, (_, i) => i + 1)
</script>

<template>
  <fieldset :id="id.base" class="price-window">
    <legend class="text-title-small">{{ t('place.window.legend', { n }) }}</legend>

    <fieldset
      class="group"
      :aria-describedby="error(id.days) ? `${id.days}-error` : undefined"
      :aria-invalid="error(id.days) ? 'true' : undefined"
    >
      <legend class="text-body-medium">{{ t('place.window.days') }}</legend>
      <div class="d-flex flex-wrap ga-1 mb-1">
        <v-checkbox
          v-for="(day, i) in weekdays"
          :id="i === 0 ? id.days : undefined"
          :key="day"
          v-model="w.days"
          :value="day"
          :label="f.weekday(i)"
          density="compact"
          hide-details
          class="flex-grow-0"
        />
      </div>
      <div
        class="d-flex flex-wrap ga-2 mb-1"
        role="group"
        :aria-label="t('place.window.presets', { n })"
      >
        <v-btn
          v-for="p in presets"
          :key="p.value"
          :aria-pressed="sameDays(w.days, dayPresets[p.value])"
          :variant="sameDays(w.days, dayPresets[p.value]) ? 'flat' : 'tonal'"
          color="primary"
          size="small"
          @click="w.days = [...dayPresets[p.value]]"
        >
          {{ p.label }}
        </v-btn>
      </div>
      <p v-if="error(id.days)" :id="`${id.days}-error`" class="text-error text-body-small">
        {{ error(id.days) }}
      </p>
    </fieldset>

    <div class="d-flex flex-wrap ga-3 mt-3">
      <v-text-field
        :id="id.from"
        v-model="w.from"
        :label="t('place.window.from')"
        :error-messages="error(id.from)"
        :aria-invalid="error(id.from) ? 'true' : undefined"
        type="time"
        density="compact"
        class="time"
      />
      <v-text-field
        :id="id.to"
        v-model="w.to"
        :label="t('place.window.to')"
        :error-messages="error(id.to)"
        :aria-invalid="error(id.to) ? 'true' : undefined"
        :hint="span"
        :persistent-hint="!!span"
        type="time"
        density="compact"
        class="time"
      />
      <v-text-field
        :id="id.price"
        v-model="w.price"
        :label="t('place.window.price')"
        :error-messages="error(id.price)"
        :aria-invalid="error(id.price) ? 'true' : undefined"
        :suffix="priceSuffix"
        inputmode="decimal"
        autocomplete="off"
        density="compact"
        class="price"
      />
    </div>

    <fieldset
      class="group"
      :aria-describedby="error(id.months) ? `${id.months}-error` : undefined"
      :aria-invalid="error(id.months) ? 'true' : undefined"
    >
      <legend class="text-body-medium">{{ t('place.window.months') }}</legend>
      <v-checkbox
        :id="id.months"
        v-model="w.allYear"
        :label="t('place.window.allYear')"
        density="compact"
        hide-details
      />
      <div v-if="!w.allYear" class="d-flex flex-wrap ga-1">
        <v-checkbox
          v-for="m in months"
          :key="m"
          v-model="w.months"
          :value="m"
          :label="f.month(m)"
          density="compact"
          hide-details
          class="flex-grow-0"
        />
      </div>
      <p v-if="error(id.months)" :id="`${id.months}-error`" class="text-error text-body-small">
        {{ error(id.months) }}
      </p>
    </fieldset>

    <div class="d-flex flex-wrap ga-2 mt-2">
      <v-btn
        :id="`${id.base}-up`"
        :prepend-icon="mdiArrowUp"
        :disabled="index === 0"
        :aria-label="t('place.window.upNamed', { n })"
        variant="text"
        size="small"
        @click="$emit('up')"
      >
        {{ t('place.window.up') }}
      </v-btn>
      <v-btn
        :id="`${id.base}-down`"
        :prepend-icon="mdiArrowDown"
        :disabled="index === count - 1"
        :aria-label="t('place.window.downNamed', { n })"
        variant="text"
        size="small"
        @click="$emit('down')"
      >
        {{ t('place.window.down') }}
      </v-btn>
      <v-btn
        :prepend-icon="mdiDelete"
        :aria-label="t('place.window.removeNamed', { n })"
        variant="text"
        color="error"
        size="small"
        class="remove-window"
        @click="$emit('remove')"
      >
        {{ t('place.window.remove') }}
      </v-btn>
    </div>
  </fieldset>
</template>

<style scoped>
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.price-window {
  border-top: thin solid rgba(var(--v-border-color), var(--v-border-opacity));
  padding-top: 0.75rem;
  margin-top: 0.75rem;
}
.group {
  margin-top: 0.5rem;
}
.time {
  flex: 1 1 8rem;
  max-width: 11rem;
}
.price {
  flex: 1 1 10rem;
  max-width: 14rem;
}
</style>
