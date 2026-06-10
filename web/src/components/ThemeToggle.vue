<script setup lang="ts">
import { computed } from 'vue'
import { Moon, Sun } from 'lucide-vue-next'
import { useThemePreference } from '../utils/theme'

const { theme, setTheme } = useThemePreference()
const isDark = computed(() => theme.value === 'dark')

function toggle() {
  setTheme(isDark.value ? 'light' : 'dark')
}
</script>

<template>
  <button
    class="theme-switch"
    :class="{ 'is-dark': isDark }"
    type="button"
    :aria-label="isDark ? '切换浅色模式' : '切换暗色模式'"
    :aria-pressed="isDark"
    @click="toggle"
  >
    <span class="theme-switch-icon theme-switch-sun">
      <Sun :size="14" :stroke-width="1.9" />
    </span>
    <span class="theme-switch-icon theme-switch-moon">
      <Moon :size="14" :stroke-width="1.9" />
    </span>
    <span class="theme-switch-thumb" aria-hidden="true" />
  </button>
</template>
