<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { nextPath, SignInForm } from '@/features/auth'
import { LocaleSelect } from '@/features/switch-locale'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

function signedIn() {
  void router.replace(nextPath(route.query.next))
}
</script>

<template>
  <v-main>
    <div class="login">
      <v-card class="card pa-6" elevation="2" rounded="lg">
        <h1 class="text-headline-small mb-1">Runsten</h1>
        <p class="text-medium-emphasis mb-6">{{ t('auth.intro') }}</p>
        <SignInForm @signed-in="signedIn" />
        <div class="d-flex justify-end mt-6">
          <LocaleSelect />
        </div>
      </v-card>
    </div>
  </v-main>
</template>

<style scoped>
.login {
  display: grid;
  place-items: center;
  min-height: 100dvh;
  padding: 1rem;
}
.card {
  width: 100%;
  max-width: 26rem;
}
</style>
