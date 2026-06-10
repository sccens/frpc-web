import { ref } from 'vue'

export type ThemePreference = 'light' | 'dark'

const storageKey = 'frpc-web-theme'

// 模块级单例，保证页面内多个组件（如主题开关与页面背景）共享同一状态。
const theme = ref<ThemePreference>(loadThemePreference())

export function useThemePreference() {
  function setTheme(next: ThemePreference) {
    theme.value = next
    localStorage.setItem(storageKey, next)
    applyThemePreference(next)
  }

  return { theme, setTheme }
}

export function loadThemePreference(): ThemePreference {
  const stored = localStorage.getItem(storageKey)
  if (stored === 'dark') return 'dark'
  return 'light'
}

export function applyThemePreference(preference: ThemePreference) {
  document.documentElement.dataset.theme = preference
  document.documentElement.style.colorScheme = preference
}
