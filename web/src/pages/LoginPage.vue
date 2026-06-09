<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Activity, ArrowRight, KeyRound, Moon, ShieldCheck, Sparkles, Sun } from 'lucide-vue-next'
import { login } from '../api/client'
import { useThemePreference } from '../utils/theme'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const accessKey = ref('')
const { theme, resolvedTheme, setTheme } = useThemePreference()

const canSubmit = computed(() => accessKey.value.trim().length >= 8)
const isDark = computed(() => theme.value === 'dark')

async function submit() {
  if (!canSubmit.value) return
  loading.value = true
  try {
    await login({ accessKey: accessKey.value.trim() })
    await router.replace(String(route.query.redirect || '/dashboard'))
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    loading.value = false
  }
}

function errorMessage(err: unknown) {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || '登录失败'
  }
  return err instanceof Error ? err.message : '登录失败'
}

function toggleTheme() {
  setTheme(isDark.value ? 'light' : 'dark')
}
</script>

<template>
  <main class="auth-control-shell" :data-auth-theme="resolvedTheme">
    <div class="auth-aurora auth-aurora-a" />
    <div class="auth-aurora auth-aurora-b" />

    <button
      class="theme-switch theme-float"
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

    <section class="auth-control-grid animate-enter">
      <div class="auth-story">
        <div class="auth-brand glass-brand">
          <span class="brand-mark">
            <Activity :size="17" :stroke-width="1.7" />
          </span>
          <span class="brand-name">FRPC<span>Web</span></span>
        </div>

        <div class="auth-story-copy">
          <span class="auth-pill"><Sparkles :size="14" :stroke-width="1.8" /> Secure Access</span>
          <h1>Welcome Back</h1>
          <p>使用本机 Access Key 进入控制台，管理 frpc 安装、代理规则、日志与运行状态。</p>
        </div>

        <div class="control-preview-card">
          <div class="control-preview-top">
            <span><i class="live-dot" /> Local Console</span>
            <code>JWT Cookie</code>
          </div>
          <div class="control-preview-grid">
            <div>
              <strong>127.0.0.1</strong>
              <span>Default Bind</span>
            </div>
            <div>
              <strong>12h</strong>
              <span>Session TTL</span>
            </div>
          </div>
        </div>
      </div>

      <form class="auth-glass-card" @submit.prevent="submit">
        <div class="auth-card-icon">
          <ShieldCheck :size="22" :stroke-width="1.7" />
        </div>
        <div class="auth-card-heading">
          <p class="overline">Owner Login</p>
          <h2>Secure Access</h2>
          <span>输入 Access Key 后，后端会签发 HttpOnly JWT 会话 Cookie。</span>
        </div>

        <label class="auth-control-field">
          <span>Access Key</span>
          <KeyRound :size="17" :stroke-width="1.7" />
          <input v-model="accessKey" type="password" autocomplete="current-password" placeholder="输入你的访问密钥" />
        </label>

        <button class="auth-primary-button" type="submit" :disabled="!canSubmit || loading">
          <span>{{ loading ? '验证中' : '进入控制台' }}</span>
          <ArrowRight :size="16" :stroke-width="1.9" />
        </button>
      </form>
    </section>
  </main>
</template>
