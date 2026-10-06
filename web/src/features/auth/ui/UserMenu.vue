<script setup lang="ts">
import { mdiAccountCircle, mdiLogout, mdiTune } from '@mdi/js'
import { useI18n } from 'vue-i18n'
import { useDisplay } from 'vuetify'
import { AttentionDot } from '@/shared/ui'
import { useSession } from '../composables/useSession'
import { useSignOut } from '../composables/useSignOut'

// UserMenu is the one menu of the app bar: what the page around it puts first (the
// slot), the user's own preferences, and the sign-out. Named beyond a phone; on one, an
// icon with the same name.
// attention: a dot on it, that something in it needs the user, saying what to screen
// readers along with its name.
defineProps<{ attention?: string }>()
const { t } = useI18n()
const { xs } = useDisplay()
const { username } = useSession()
const { signOut, isPending } = useSignOut()
</script>

<template>
  <v-menu>
    <template #activator="{ props }">
      <span class="anchor">
        <v-btn
          v-bind="props"
          :icon="xs ? mdiAccountCircle : undefined"
          :prepend-icon="xs ? undefined : mdiAccountCircle"
          :text="xs ? undefined : username"
          :aria-label="t('user.menu', { username })"
          :title="xs ? t('user.menu', { username }) : undefined"
          :aria-describedby="attention ? 'user-menu-attention' : undefined"
          :loading="isPending"
          variant="text"
          class="user-menu"
        />
        <AttentionDot v-if="attention" id="user-menu-attention" :label="attention" class="dot" />
      </span>
    </template>
    <v-list density="compact">
      <slot />
      <v-list-item
        :to="{ name: 'preferences' }"
        :prepend-icon="mdiTune"
        :title="t('preferences.open')"
        class="preferences"
      />
      <v-list-item
        :prepend-icon="mdiLogout"
        :title="t('auth.signOut')"
        class="sign-out"
        @click="signOut"
      />
    </v-list>
  </v-menu>
</template>

<style scoped>
.anchor {
  position: relative;
}
/* Over the corner of the button, as a badge. */
.dot {
  position: absolute;
  inset-block-start: 6px;
  inset-inline-end: 6px;
  pointer-events: none;
}
</style>
