<script setup lang="ts">
import { mdiDatabaseImportOutline } from '@mdi/js'
import { useFormat } from '@/shared/lib'

// ImportedBadge marks an event imported from another application, named: Runsten did not
// derive it from its own readings. As calm as a reconstructed event's: it is as true,
// only told by another.
defineProps<{ source: string }>()
const f = useFormat()
</script>

<template>
  <v-chip
    size="small"
    variant="flat"
    color="surface-variant"
    :prepend-icon="mdiDatabaseImportOutline"
    class="imported"
    >{{ f.importedFrom(source) }}</v-chip
  >
</template>
