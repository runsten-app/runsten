<script setup lang="ts">
import { mdiDelete, mdiPlus } from '@mdi/js'
import { computed, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { isCalendarDay, useFormat } from '@/shared/lib'
import { move, newWindow, type VersionDraft } from '../model/draft'
import { field } from '../model/validate'
import PriceWindowFields from './PriceWindowFields.vue'

// TariffVersionCard is one price of a place, from its day on: a base price and the time
// windows that replace it. The card is a fieldset, its legend the day.
const v = defineModel<VersionDraft>({ required: true })
const props = defineProps<{
  index: number
  removable: boolean
  priceSuffix: string
  error: (id: string) => string | undefined
  maxWindows: number
}>()
defineEmits<{ remove: [] }>()
const { t } = useI18n()
const f = useFormat()

const id = computed(() => ({
  validFrom: field.validFrom(props.index),
  price: field.price(props.index),
  add: field.addWindow(props.index),
}))
const legend = computed(() =>
  isCalendarDay(v.value.validFrom)
    ? t('place.tariff.versionFrom', { date: f.calendarDay(v.value.validFrom) })
    : t('place.tariff.versionNew'),
)
const full = computed(() => v.value.windows.length >= props.maxWindows)

const focus = async (id: string) => {
  await nextTick()
  document.getElementById(id)?.focus()
}

function addWindow() {
  v.value.windows.push(newWindow())
  void focus(field.days(props.index, v.value.windows.length - 1))
}

// A moved window keeps the focus on the button pressed, at its new place, or on the
// other one when it reached an end.
function moveWindow(j: number, by: -1 | 1) {
  const to = j + by
  move(v.value.windows, j, to)
  const edge = to === 0 || to === v.value.windows.length - 1
  const button = (by === -1) !== edge ? 'up' : 'down'
  void focus(`place-v${props.index}-w${to}-${button}`)
}

function removeWindow(j: number) {
  v.value.windows.splice(j, 1)
  void focus(id.value.add)
}
</script>

<template>
  <v-card class="tariff-version mb-4">
    <v-card-text>
      <fieldset>
        <legend class="text-title-medium mb-2">{{ legend }}</legend>
        <div class="d-flex flex-wrap ga-3">
          <v-text-field
            :id="id.validFrom"
            v-model="v.validFrom"
            :label="t('place.tariff.validFrom')"
            :error-messages="error(id.validFrom)"
            :aria-invalid="error(id.validFrom) ? 'true' : undefined"
            :hint="t('place.tariff.validFromHelp')"
            persistent-hint
            type="date"
            density="compact"
            class="date"
          />
          <v-text-field
            :id="id.price"
            v-model="v.price"
            :label="t('place.tariff.price')"
            :error-messages="error(id.price)"
            :aria-invalid="error(id.price) ? 'true' : undefined"
            :suffix="priceSuffix"
            inputmode="decimal"
            autocomplete="off"
            density="compact"
            class="price"
          />
        </div>

        <p v-if="!v.windows.length" class="text-body-medium text-medium-emphasis mt-3">
          {{ t('place.tariff.noWindow') }}
        </p>
        <PriceWindowFields
          v-for="(w, j) in v.windows"
          :key="w.key"
          :model-value="w"
          :version="index"
          :index="j"
          :count="v.windows.length"
          :price-suffix
          :error
          @up="moveWindow(j, -1)"
          @down="moveWindow(j, 1)"
          @remove="removeWindow(j)"
        />

        <div class="d-flex flex-wrap ga-2 mt-4">
          <v-btn
            :id="id.add"
            :prepend-icon="mdiPlus"
            :disabled="full"
            variant="tonal"
            color="primary"
            class="add-window"
            @click="addWindow"
          >
            {{ t('place.tariff.addWindow') }}
          </v-btn>
          <v-btn
            v-if="removable"
            :prepend-icon="mdiDelete"
            variant="text"
            color="error"
            class="remove-version"
            @click="$emit('remove')"
          >
            {{ t('place.tariff.removeVersion') }}
          </v-btn>
        </div>
        <p v-if="full" class="text-body-small text-medium-emphasis mt-2">
          {{ t('place.tariff.tooManyWindows', { max: maxWindows }) }}
        </p>
      </fieldset>
    </v-card-text>
  </v-card>
</template>

<style scoped>
fieldset {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.date {
  flex: 1 1 12rem;
  max-width: 16rem;
}
.price {
  flex: 1 1 10rem;
  max-width: 14rem;
}
</style>
