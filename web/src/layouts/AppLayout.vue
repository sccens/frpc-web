<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Activity,
  Gauge,
  LogOut,
  Network,
  Server,
  Settings,
} from 'lucide-vue-next'
import { changeAccessKey, getAuthStatus, logout } from '../api/client'
import FloatingLogButton from '../components/FloatingLogButton.vue'
import ThemeToggle from '../components/ThemeToggle.vue'
import { errorMessage } from '../utils/errors'

const navItems = [
  { to: '/topology', label: '拓扑', icon: Network },
  { to: '/servers', label: '服务器', icon: Server },
  { to: '/traffic', label: '流量', icon: Gauge },
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

// 首次以初始密钥登录后，强制改密：弹窗在设置好新密码前不可关闭。
const mustChange = ref(false)
const newPwd = ref('')
const confirmPwd = ref('')
const submitting = ref(false)

const lenOk = computed(() => newPwd.value.length >= 8 && newPwd.value.length <= 20)
const upperOk = computed(() => /[A-Z]/.test(newPwd.value))
const lowerOk = computed(() => /[a-z]/.test(newPwd.value))
const digitOk = computed(() => /[0-9]/.test(newPwd.value))
const alnumOk = computed(() => newPwd.value.length > 0 && /^[A-Za-z0-9]+$/.test(newPwd.value))
const matchOk = computed(() => confirmPwd.value.length > 0 && newPwd.value === confirmPwd.value)
const canSubmit = computed(
  () => lenOk.value && upperOk.value && lowerOk.value && digitOk.value && alnumOk.value && matchOk.value,
)

async function refreshAuth() {
  try {
    const status = await getAuthStatus()
    mustChange.value = Boolean(status.authenticated && status.mustChangePassword)
  } catch {
    mustChange.value = false
  }
}
onMounted(refreshAuth)

async function submitPassword() {
  if (!canSubmit.value || submitting.value) return
  submitting.value = true
  try {
    // 首次设置无需当前密钥：有效会话即凭证（后端据 mustChange 状态放行）。
    await changeAccessKey({ currentAccessKey: '', newAccessKey: newPwd.value })
    ElMessage.success('密码已设置，请用新密码重新登录')
    await router.replace('/login')
  } catch (err) {
    ElMessage.error(errorMessage(err, '设置密码失败'))
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="app-shell">
    <div class="page-grid" />
    <div class="ambient-glow" />

    <header class="topbar">
      <div class="topbar-inner">
        <RouterLink to="/topology" class="brand">
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

    <el-dialog
      :model-value="mustChange"
      title="首次登录 · 请设置新密码"
      width="440px"
      align-center
      :show-close="false"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
    >
      <p class="force-change-hint">
        当前使用的是初始密钥，为安全起见请立即设置你自己的密码。设置后初始密钥将立即失效。
      </p>

      <el-input
        v-model="newPwd"
        type="password"
        show-password
        autocomplete="new-password"
        placeholder="新密码（8-20 位）"
        maxlength="20"
      />
      <div class="pwd-meter" :class="{ ok: lenOk }">
        长度 {{ newPwd.length }} / 20
      </div>

      <el-input
        v-model="confirmPwd"
        type="password"
        show-password
        autocomplete="new-password"
        placeholder="确认新密码"
        maxlength="20"
        style="margin-top: 10px"
      />

      <ul class="pwd-rules">
        <li :class="{ ok: lenOk }">长度 8-20 位</li>
        <li :class="{ ok: upperOk }">含大写字母</li>
        <li :class="{ ok: lowerOk }">含小写字母</li>
        <li :class="{ ok: digitOk }">含数字</li>
        <li :class="{ ok: alnumOk }">仅包含字母和数字</li>
        <li :class="{ ok: matchOk }">两次输入一致</li>
      </ul>

      <template #footer>
        <el-button type="primary" :disabled="!canSubmit" :loading="submitting" @click="submitPassword">
          设置并重新登录
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.force-change-hint {
  margin: 0 0 14px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--el-text-color-secondary, #909399);
}

.pwd-meter {
  margin-top: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary, #909399);
}

.pwd-rules {
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 16px;
}

.pwd-rules li {
  position: relative;
  padding-left: 18px;
  font-size: 12px;
  color: var(--el-text-color-secondary, #909399);
}

.pwd-rules li::before {
  content: '○';
  position: absolute;
  left: 0;
}

.pwd-rules li.ok,
.pwd-meter.ok {
  color: var(--el-color-success, #67c23a);
}

.pwd-rules li.ok::before {
  content: '✓';
}
</style>
