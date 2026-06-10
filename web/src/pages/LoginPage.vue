<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Activity, ArrowRight, KeyRound, ShieldCheck, Sparkles } from 'lucide-vue-next'
import { login } from '../api/client'
import ThemeToggle from '../components/ThemeToggle.vue'
import { errorMessage } from '../utils/errors'
import { useThemePreference } from '../utils/theme'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const accessKey = ref('')
const { theme } = useThemePreference()

const canSubmit = computed(() => accessKey.value.trim().length >= 8)

async function submit() {
  if (!canSubmit.value) return
  loading.value = true
  try {
    await login({ accessKey: accessKey.value.trim() })
    await router.replace(String(route.query.redirect || '/dashboard'))
  } catch (err) {
    ElMessage.error(errorMessage(err, '登录失败'))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="auth-control-shell" :data-auth-theme="theme">
    <div class="auth-aurora auth-aurora-a" />
    <div class="auth-aurora auth-aurora-b" />

    <ThemeToggle class="theme-float" />

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
            <code>Session Cookie</code>
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
          <span>输入 Access Key 后，后端会签发 HttpOnly 会话 Cookie。</span>
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
