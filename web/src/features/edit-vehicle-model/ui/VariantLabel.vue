<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { SourceLevel, VariantOption } from '@/entities/vehicle'
import { useFormat } from '@/shared/lib'

// VariantLabel is a variant's radio label: its name and years, its figures, and where the
// catalog has them from.
const props = defineProps<{ option: VariantOption; recognized: boolean }>()
const { t } = useI18n()
const f = useFormat()

const kw = (v: number) => f.quantity(v, 'kW')
const kwh = (v: number | null) => f.quantity(v, 'kWh')
function years(): string {
  const y = props.option.model_years
  if (y.to === null) return t('vehicleModel.yearsSince', { from: y.from })
  if (y.to === y.from) return String(y.from)
  return t('vehicleModel.yearsRange', { from: y.from, to: y.to })
}
function figures(): string {
  const o = props.option
  const capacity =
    o.net_kwh === null
      ? t('vehicleModel.grossOnly', { gross: kwh(o.gross_kwh) })
      : t('vehicleModel.capacity', { gross: kwh(o.gross_kwh), net: kwh(o.net_kwh) })
  const ac =
    o.ac_option_kw === null
      ? t('vehicleModel.ac', { kw: kw(o.ac_max_kw) })
      : t('vehicleModel.acOption', { kw: kw(o.ac_max_kw), option: kw(o.ac_option_kw) })
  // Most plug-in hybrids do not charge on DC.
  const dc =
    o.dc_max_kw === null ? t('vehicleModel.noDC') : t('vehicleModel.dc', { kw: kw(o.dc_max_kw) })
  return [capacity, ac, dc].join(' · ')
}
// sources tells where the figures come from: the manufacturer, public sources, or both,
// naming the figures public sources give.
function sources(): string {
  const l = props.option.levels
  const named: [SourceLevel | null, string][] = [
    [l.gross_kwh, t('vehicleModel.figure.gross')],
    [l.net_kwh, t('vehicleModel.figure.net')],
    [l.ac_max_kw, t('vehicleModel.figure.ac')],
    [l.ac_option_kw, t('vehicleModel.figure.acOption')],
    [l.dc_max_kw, t('vehicleModel.figure.dc')],
  ]
  const present = named.filter(([level]) => level !== null)
  const secondary = present.filter(([level]) => level === 'secondary').map(([, name]) => name)
  if (secondary.length === 0) return t('vehicleModel.fromManufacturer')
  if (secondary.length === present.length) return t('vehicleModel.fromPublic')
  return t('vehicleModel.fromBoth', { figures: secondary.join(', ') })
}
</script>

<template>
  <span>
    <span class="d-block text-body-large">
      {{ t('vehicleModel.option', { name: option.name, years: years() }) }}
      <span v-if="recognized" class="recognized text-primary">
        · {{ t('vehicleModel.recognized') }}
      </span>
    </span>
    {{ ' ' }}
    <span class="d-block text-body-medium">{{ figures() }}</span>
    {{ ' ' }}
    <span class="d-block text-body-small text-medium-emphasis">{{ sources() }}</span>
  </span>
</template>
