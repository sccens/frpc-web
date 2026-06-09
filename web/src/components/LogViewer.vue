<script setup lang="ts">
import type { LogLine } from '../api/client'

defineProps<{
  lines: LogLine[]
}>()
</script>

<template>
  <div class="log-viewer">
    <div v-for="line in lines" :key="`${line.time}-${line.message}`" class="log-line">
      <span class="log-time">{{ line.time }}</span>
      <span class="log-level" :class="`level-${line.level}`">{{ line.level }}</span>
      <span class="log-message">{{ line.message }}</span>
    </div>
  </div>
</template>

