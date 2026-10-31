<script setup lang="ts">
import { mdiChevronDown, mdiChevronUp } from '@mdi/js'
import { computed, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { useFormat } from '@/shared/lib'
import { FactItem } from '@/shared/ui'
import type { Vehicle } from '../model/Vehicle'
import VehiclePicture from './VehiclePicture.vue'

// VehicleSheet is what the vehicle's source tells of it beyond its model, on a tile of the
// vehicle's settings: its picture, then only the values told, never an empty one. What a
// driver knows the car by comes first; the rest folds under a toggle. Nothing at all for a
// source that tells none of it (a Polestar of the Data Portal).
const props = defineProps<{ vehicle: Pick<Vehicle, 'id' | 'sheet'> }>()
const { t } = useI18n()
const f = useFormat()
const id = useId()
const titleId = `${id}-title`
const moreId = `${id}-more`
const open = ref(false)

type Fact = { key: string; label: string; value: string }

const facts = computed(() => {
  const s = props.vehicle.sheet
  if (!s) return { main: [], more: [] }
  const fact = (key: string, label: string, value: string | null | false): Fact[] =>
    value ? [{ key, label, value }] : []
  const gearboxes = { automatic: t('vehicleSheet.automatic'), manual: t('vehicleSheet.manual') }
  const drives = {
    fwd: t('vehicleSheet.fwd'),
    rwd: t('vehicleSheet.rwd'),
    awd: t('vehicleSheet.awd'),
  }
  const sides = { left: t('vehicleSheet.left'), right: t('vehicleSheet.right') }
  return {
    main: [
      ...fact('name', t('vehicleSheet.name'), s.name),
      ...fact('colour', t('vehicleSheet.colour'), s.colour),
      ...fact('drive', t('vehicleSheet.drive'), s.drive && drives[s.drive]),
      ...fact(
        'power',
        t('vehicleSheet.power'),
        s.power_kw !== null && t('vehicleSheet.kW', { value: f.number(s.power_kw, 0) }),
      ),
      ...fact(
        'registration',
        t('vehicleSheet.firstRegistration'),
        s.first_registration && f.calendarDay(s.first_registration),
      ),
      ...fact('seats', t('vehicleSheet.seats'), s.seats !== null && f.number(s.seats)),
    ],
    more: [
      ...fact('gearbox', t('vehicleSheet.gearbox'), s.gearbox && gearboxes[s.gearbox]),
      ...fact(
        'weight',
        t('vehicleSheet.grossWeight'),
        s.gross_weight_kg !== null && t('vehicleSheet.kg', { value: f.number(s.gross_weight_kg) }),
      ),
      ...fact(
        'engine',
        t('vehicleSheet.engine'),
        s.engine_cc !== null && t('vehicleSheet.cc', { value: f.number(s.engine_cc) }),
      ),
      ...fact('upholstery', t('vehicleSheet.upholstery'), s.upholstery),
      ...fact('steering', t('vehicleSheet.steering'), s.steering && sides[s.steering]),
      ...fact('wheels', t('vehicleSheet.wheels'), s.wheels),
      ...fact('roof', t('vehicleSheet.roofColour'), s.roof_colour),
      ...fact('thirdRow', t('vehicleSheet.thirdRow'), s.third_row && t('vehicleSheet.yes')),
      ...fact('performance', t('vehicleSheet.performance'), s.performance && t('vehicleSheet.yes')),
    ],
  }
})
// The main values, or the others when it tells none of them: never a toggle alone.
const shown = computed(() => (facts.value.main.length ? facts.value.main : facts.value.more))
const folded = computed(() => (facts.value.main.length ? facts.value.more : []))
const told = computed(() => !!props.vehicle.sheet?.picture || shown.value.length > 0)
</script>

<template>
  <v-card v-if="told" tag="section" class="vehicle-sheet" :aria-labelledby="titleId">
    <v-card-title :id="titleId" tag="h3" class="text-title-large">
      {{ t('vehicleSheet.title') }}
    </v-card-title>
    <v-card-text class="pt-0">
      <VehiclePicture :vehicle size="large" class="mb-4" />
      <dl v-if="shown.length" class="facts">
        <FactItem
          v-for="fact in shown"
          :key="fact.key"
          :label="fact.label"
          :value="fact.value"
          :class="fact.key"
        />
      </dl>
      <template v-if="folded.length">
        <v-btn
          variant="text"
          class="more-toggle mt-2"
          :append-icon="open ? mdiChevronUp : mdiChevronDown"
          :aria-expanded="open"
          :aria-controls="moreId"
          @click="open = !open"
        >
          {{ open ? t('vehicleSheet.fewer') : t('vehicleSheet.more', { count: folded.length }) }}
        </v-btn>
        <div :id="moreId">
          <dl v-if="open" class="facts mt-2">
            <FactItem
              v-for="fact in folded"
              :key="fact.key"
              :label="fact.label"
              :value="fact.value"
              :class="fact.key"
            />
          </dl>
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.facts {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr));
  gap: 1rem;
}
</style>
