<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CurrencyCode, Settings } from '@/entities/settings'
import { isApiError } from '@/shared/api'
import { useFormat } from '@/shared/lib'
import { useSetCurrency } from '../composables/useSetCurrency'

// CurrencyForm chooses the account's currency, saved on request. The server refuses a
// change once prices exist (currency_in_use): their amounts have no unit of their own.
const props = defineProps<{ settings: Settings }>()
const { t } = useI18n()
const f = useFormat()
const { setCurrency, isPending, error, reset } = useSetCurrency()

const chosen = ref<CurrencyCode | null>(props.settings.currency?.code ?? null)
watch(
  () => props.settings.currency?.code,
  (code) => (chosen.value = code ?? null),
)
const items = computed(() =>
  props.settings.currencies.map((c) => ({
    value: c.code,
    title: t('settings.currency.option', { name: f.currencyName(c.code), code: c.code }),
  })),
)
const status = ref('')

const message = computed(() => {
  const e = error.value
  if (!e) return ''
  if (isApiError(e, 'currency_in_use')) return t('settings.currency.inUse')
  if (isApiError(e, 'unavailable') || isApiError(e, 'network'))
    return t('settings.currency.unavailable')
  return t('settings.currency.error')
})

function choose(code: CurrencyCode | null) {
  chosen.value = code
  status.value = ''
  reset()
}

async function submit() {
  if (!chosen.value) return
  try {
    await setCurrency(chosen.value)
    status.value = t('settings.currency.saved')
  } catch {
    // Shown from error.
  }
}
</script>

<template>
  <v-form class="currency-form" @submit.prevent="submit">
    <p v-if="!settings.currency" class="no-currency text-body-medium mb-3">
      {{ t('settings.currency.none') }}
    </p>
    <div class="d-flex flex-wrap align-start ga-3">
      <v-select
        id="settings-currency"
        :model-value="chosen"
        :items="items"
        :label="t('settings.currency.label')"
        :hint="t('settings.currency.help')"
        persistent-hint
        class="currency"
        @update:model-value="choose"
      />
      <v-btn
        type="submit"
        color="primary"
        size="large"
        :disabled="!chosen"
        :loading="isPending"
        class="save mt-1"
      >
        {{ t('settings.currency.save') }}
      </v-btn>
    </div>
    <v-alert v-if="message" type="error" variant="tonal" class="mt-4">{{ message }}</v-alert>
    <p role="status" class="status text-body-medium mt-2">{{ status }}</p>
  </v-form>
</template>

<style scoped>
.currency {
  flex: 1 1 16rem;
  max-width: 24rem;
}
</style>
