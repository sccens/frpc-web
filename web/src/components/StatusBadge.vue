<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  status: string
}>()

const meta = computed(() => {
  switch (props.status) {
    case 'running':
      return { label: 'Running', className: 'is-running' }
    case 'stopped':
      return { label: 'Stopped', className: 'is-stopped' }
    case 'no-admin':
      return { label: 'No Admin', className: 'is-warning' }
    case 'config_dirty':
      return { label: 'Dirty', className: 'is-warning' }
    case 'error':
      return { label: 'Error', className: 'is-error' }
    case 'starting':
      return { label: 'Starting', className: 'is-warning' }
    case 'reloading':
      return { label: 'Reloading', className: 'is-warning' }
    default:
      return { label: props.status, className: 'is-stopped' }
  }
})
</script>

<template>
  <span class="status-badge" :class="meta.className">
    <span class="status-dot" aria-hidden="true" />
    {{ meta.label }}
  </span>
</template>
