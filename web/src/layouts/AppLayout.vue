<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Activity,
  FileClock,
  LogOut,
  Moon,
  ScrollText,
  Server,
  Settings,
  Signal,
  Sun,
  UserRound,
} from 'lucide-vue-next'
import { getMe, logout, type User } from '../api/client'
import { useThemePreference } from '../utils/theme'

const navItems = [
  { to: '/dashboard', label: '总览', icon: Activity },
  { to: '/servers', label: '服务器', icon: Server },
  { to: '/logs', label: '日志', icon: ScrollText },
  { to: '/stats', label: '统计', icon: Signal },
  { to: '/audit', label: '审计', icon: FileClock },
  { to: '/settings', label: '设置', icon: Settings },
]

const router = useRouter()
const user = ref<User | null>(null)
const { theme, setTheme } = useThemePreference()
const isDark = computed(() => theme.value === 'dark')

onMounted(async () => {
  try {
    user.value = await getMe()
  } catch {
    user.value = null
  }
})

async function signOut() {
  await logout()
  await router.replace('/login')
}

function toggleTheme() {
  setTheme(isDark.value ? 'light' : 'dark')
}
</script>

<template>
  <div class="app-shell">
    <div class="page-grid" />
    <div class="ambient-glow" />

    <header class="topbar">
      <div class="topbar-inner">
        <RouterLink to="/dashboard" class="brand">
          <span class="brand-mark">
            <Activity :size="17" :stroke-width="1.7" />
          </span>
          <span class="brand-name">FRPC<span>Web</span></span>
        </RouterLink>

        <nav class="main-nav" aria-label="主导航">
          <RouterLink v-for="item in navItems" :key="item.to" :to="item.to">
            <component :is="item.icon" :size="15" :stroke-width="1.6" />
            <span>{{ item.label }}</span>
          </RouterLink>
        </nav>

        <div class="topbar-actions">
          <button
            class="theme-switch console-theme-toggle"
            :class="{ 'is-dark': isDark }"
            type="button"
            :aria-label="isDark ? '切换浅色模式' : '切换暗色模式'"
            :aria-pressed="isDark"
            @click="toggleTheme"
          >
            <span class="theme-switch-icon theme-switch-sun">
              <Sun :size="14" :stroke-width="1.9" />
            </span>
            <span class="theme-switch-icon theme-switch-moon">
              <Moon :size="14" :stroke-width="1.9" />
            </span>
            <span class="theme-switch-thumb" aria-hidden="true" />
          </button>
          <span class="user-chip icon-only" v-if="user" aria-label="已登录">
            <UserRound :size="16" :stroke-width="1.7" />
          </span>
          <button class="avatar-button" type="button" aria-label="退出登录" @click="signOut">
            <LogOut :size="16" :stroke-width="1.7" />
          </button>
        </div>
      </div>
    </header>

    <main class="main-area">
      <section class="content">
        <RouterView />
      </section>
    </main>
  </div>
</template>
