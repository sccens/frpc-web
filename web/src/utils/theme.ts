import { computed, ref } from 'vue'

export type ThemePreference = 'light' | 'dark'

const storageKey = 'frpc-web-theme'

export function useThemePreference() {
	const theme = ref<ThemePreference>(loadThemePreference())
	const resolvedTheme = computed(() => resolveThemePreference(theme.value))
	applyThemePreference(theme.value)

	function setTheme(next: ThemePreference) {
		theme.value = next
		localStorage.setItem(storageKey, next)
		applyThemePreference(next)
	}

	return { theme, resolvedTheme, setTheme }
}

export function loadThemePreference(): ThemePreference {
  const stored = localStorage.getItem(storageKey)
  if (stored === 'dark') return 'dark'
  return 'light'
}

export function resolveThemePreference(preference: ThemePreference) {
	return preference
}

export function applyThemePreference(preference: ThemePreference) {
	const resolved = resolveThemePreference(preference)
	document.documentElement.dataset.theme = resolved
	document.documentElement.style.colorScheme = resolved
}
