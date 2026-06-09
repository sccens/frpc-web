import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '../layouts/AppLayout.vue'
import BootstrapPage from '../pages/BootstrapPage.vue'
import DashboardPage from '../pages/DashboardPage.vue'
import LoginPage from '../pages/LoginPage.vue'
import ServersPage from '../pages/ServersPage.vue'
import LogsPage from '../pages/LogsPage.vue'
import SettingsPage from '../pages/SettingsPage.vue'
import StatsPage from '../pages/StatsPage.vue'
import AuditPage from '../pages/AuditPage.vue'
import { getAuthStatus } from '../api/client'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/bootstrap', name: 'bootstrap', component: BootstrapPage, meta: { public: true } },
    { path: '/login', name: 'login', component: LoginPage, meta: { public: true } },
    {
      path: '/',
      component: AppLayout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', name: 'dashboard', component: DashboardPage },
        { path: 'servers', name: 'servers', component: ServersPage },
        { path: 'logs', name: 'logs', component: LogsPage },
        { path: 'versions', redirect: '/settings' },
        { path: 'stats', name: 'stats', component: StatsPage },
        { path: 'audit', name: 'audit', component: AuditPage },
        { path: 'settings', name: 'settings', component: SettingsPage },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  const status = await getAuthStatus()
  const isPublic = Boolean(to.meta.public)

  if (!status.bootstrapped) {
    return to.name === 'bootstrap' ? true : { name: 'bootstrap', query: { redirect: to.fullPath } }
  }

  if (isPublic) {
    if (status.authenticated) {
      return { name: 'dashboard' }
    }
    return to.name === 'login' ? true : { name: 'login', query: { redirect: to.fullPath } }
  }

  if (!status.authenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }

  return true
})
