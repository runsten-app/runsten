<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { isApiError } from '@/shared/api'
import { useSignIn } from '../composables/useSignIn'

const emit = defineEmits<{ signedIn: [] }>()
const { t } = useI18n()
const { signIn, isPending, error, reset } = useSignIn()

const username = ref('')
const password = ref('')

// Focused once mounted, not with Vuetify's autofocus: it focuses the field again 50 ms
// after it shows, taking the focus back from a quick Tab to the password.
const usernameField = ref<{ focus: () => void } | null>(null)
onMounted(() => usernameField.value?.focus())

const message = computed(() => {
  const e = error.value
  if (!e) return ''
  if (isApiError(e, 'invalid_credentials')) return t('auth.error.invalidCredentials')
  if (isApiError(e, 'too_many_attempts'))
    return t('auth.error.tooManyAttempts', { minutes: Math.ceil((e.retryAfter ?? 60) / 60) })
  if (isApiError(e, 'unavailable') || isApiError(e, 'network')) return t('auth.error.unavailable')
  return t('auth.error.internal')
})

async function submit() {
  try {
    await signIn({ username: username.value, password: password.value })
    emit('signedIn')
  } catch {
    // Shown from error.
  }
}
</script>

<template>
  <v-form @submit.prevent="submit" @update:model-value="reset">
    <v-text-field
      ref="usernameField"
      v-model="username"
      :label="t('auth.username')"
      name="username"
      autocomplete="username"
      autocapitalize="none"
      spellcheck="false"
      required
    />
    <v-text-field
      v-model="password"
      :label="t('auth.password')"
      name="password"
      type="password"
      autocomplete="current-password"
      required
    />
    <v-alert v-if="message" type="error" variant="tonal" class="mb-4" role="alert">
      {{ message }}
    </v-alert>
    <v-btn
      type="submit"
      color="primary"
      block
      size="large"
      :loading="isPending"
      :disabled="!username || !password"
    >
      {{ t('auth.signIn') }}
    </v-btn>
  </v-form>
</template>
