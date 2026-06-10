<script setup lang="ts">
import { useRouter } from 'vue-router'
import {
  Activity,
  BarChart3,
  LogOut,
  Network,
  Server,
  Settings,
} from 'lucide-vue-next'
import { logout } from '../api/client'
import FloatingLogButton from '../components/FloatingLogButton.vue'
import ThemeToggle from '../components/ThemeToggle.vue'

const navItems = [
  { to: '/dashboard', label: '总览', icon: Activity },
  { to: '/servers', label: '服务器', icon: Server },
  { to: '/topology', label: '拓扑', icon: Network },
  { to: '/traffic', label: '流量', icon: BarChart3 },
  { to: '/settings', label: '设置', icon: Settings },
]

const router = useRouter()

async function signOut() {
  try {
    await logout()
  } finally {
    // 注销请求失败也要回到登录页，避免用户被卡在已失效的会话里
    await router.replace('/login')
  }
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
          <ThemeToggle class="console-theme-toggle" />
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

    <FloatingLogButton />
  </div>
</template>
