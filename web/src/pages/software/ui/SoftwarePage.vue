<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { AppShell } from '@/widgets/app-shell'
import { SoftwareSummary, SoftwareVersions } from '@/widgets/software-history'

// SoftwarePage is the history of the vehicle's software: the version running and the
// update waiting first, then every version with how long the vehicle ran it and how long
// its installation took. Reached from the health page, whose tab it keeps: the software
// is a part of the vehicle's health.
defineProps<{ vehicle: string }>()
const { t } = useI18n()
</script>

<template>
  <AppShell :vehicle="vehicle">
    <h1 class="text-headline-small mb-4">{{ t('software.title') }}</h1>
    <SoftwareSummary :vehicle="vehicle" :linked="false" class="mb-3" />
    <SoftwareVersions :vehicle="vehicle" />
  </AppShell>
</template>
