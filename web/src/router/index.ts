import { createRouter, createWebHistory } from 'vue-router'
import { getAuthStatus } from '../api/client'

const AppLayout = () => import('../layouts/AppLayout.vue')
const DashboardPage = () => import('../pages/DashboardPage.vue')
const LoginPage = () => import('../pages/LoginPage.vue')
const ServersPage = () => import('../pages/ServersPage.vue')
const SettingsPage = () => import('../pages/SettingsPage.vue')
const TopologyPage = () => import('../pages/TopologyPage.vue')

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginPage, meta: { public: true } },
    {
      path: '/',
      component: AppLayout,
      redirect: '/dashboard',
      children: [
        { path: 'dashboard', name: 'dashboard', component: DashboardPage },
        { path: 'servers', name: 'servers', component: ServersPage },
        { path: 'topology', name: 'topology', component: TopologyPage },
        { path: 'settings', name: 'settings', component: SettingsPage },
        { path: 'logs', redirect: '/dashboard' },
        { path: 'stats', redirect: '/dashboard' },
        { path: 'traffic', redirect: '/dashboard' },
        { path: 'audit', redirect: '/settings' },
        { path: 'versions', redirect: '/settings' },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  const isPublic = Boolean(to.meta.public)
  let status
  try {
    status = await getAuthStatus()
  } catch {
    // 后端暂时不可达时放行导航，避免整个应用卡死；
    // 页面内的请求会展示各自的错误提示。
    return true
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
