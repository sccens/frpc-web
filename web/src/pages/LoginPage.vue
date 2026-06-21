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
    await router.replace(String(route.query.redirect || '/topology'))
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
          <span class="auth-pill"><Sparkles :size="14" :stroke-width="1.8" /> 安全访问</span>
          <h1>欢迎回来</h1>
          <p>使用本机访问密钥进入控制台，查看 frpc 配置、连接拓扑与实时状态，编辑配置并触发热重载。</p>
        </div>

        <div class="control-preview-card">
          <div class="control-preview-top">
            <span><i class="live-dot" /> 本机控制台</span>
            <code>会话 Cookie</code>
          </div>
          <div class="control-preview-grid">
            <div>
              <strong>127.0.0.1</strong>
              <span>默认监听</span>
            </div>
            <div>
              <strong>12h</strong>
              <span>会话有效期</span>
            </div>
          </div>
        </div>
      </div>

      <form class="auth-glass-card" @submit.prevent="submit">
        <div class="auth-card-icon">
          <ShieldCheck :size="22" :stroke-width="1.7" />
        </div>
        <div class="auth-card-heading">
          <p class="overline">管理员登录</p>
          <h2>安全访问</h2>
          <span>输入访问密钥后，后端会签发 HttpOnly 会话 Cookie。首次使用请用初始密钥登录（见安装输出 / README），登录后需设置自己的新密码。</span>
        </div>

        <label class="auth-control-field">
          <span>访问密钥</span>
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
