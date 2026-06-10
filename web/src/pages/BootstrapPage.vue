<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Activity, ArrowRight, KeyRound, ShieldCheck, Sparkles } from 'lucide-vue-next'
import { bootstrapAdmin } from '../api/client'
import ThemeToggle from '../components/ThemeToggle.vue'
import { errorMessage } from '../utils/errors'
import { useThemePreference } from '../utils/theme'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const accessKey = ref('')
const confirmAccessKey = ref('')
const { theme } = useThemePreference()

const canSubmit = computed(() => accessKey.value.trim().length >= 8 && accessKey.value === confirmAccessKey.value)

async function submit() {
  if (!canSubmit.value) return
  loading.value = true
  try {
    await bootstrapAdmin({ accessKey: accessKey.value.trim() })
    ElMessage.success('Access Key 已初始化')
    await router.replace(String(route.query.redirect || '/dashboard'))
  } catch (err) {
    ElMessage.error(errorMessage(err, '初始化失败'))
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
          <span class="auth-pill"><Sparkles :size="14" :stroke-width="1.8" /> First Run</span>
          <h1>Secure Access</h1>
          <p>创建唯一的本机 Access Key。之后所有控制台请求都会通过 HttpOnly 会话 Cookie 和服务端会话校验。</p>
        </div>

        <div class="control-preview-card">
          <div class="control-preview-top">
            <span><i class="live-dot" /> Single Owner</span>
            <code>SQLite Session</code>
          </div>
          <div class="control-preview-grid">
            <div>
              <strong>1 Key</strong>
              <span>Access Model</span>
            </div>
            <div>
              <strong>0600</strong>
              <span>Data Files</span>
            </div>
          </div>
        </div>
      </div>

      <form class="auth-glass-card" @submit.prevent="submit">
        <div class="auth-card-icon">
          <ShieldCheck :size="22" :stroke-width="1.7" />
        </div>
        <div class="auth-card-heading">
          <p class="overline">Bootstrap</p>
          <h2>Create Access Key</h2>
          <span>密钥长度至少 8 位；如果设置了 FRPC_WEB_ACCESS_KEY，则无需初始化。</span>
        </div>

        <label class="auth-control-field">
          <span>Access Key</span>
          <KeyRound :size="17" :stroke-width="1.7" />
          <input v-model="accessKey" type="password" autocomplete="new-password" placeholder="创建访问密钥" />
        </label>

        <label class="auth-control-field">
          <span>Confirm</span>
          <KeyRound :size="17" :stroke-width="1.7" />
          <input v-model="confirmAccessKey" type="password" autocomplete="new-password" placeholder="再次输入访问密钥" />
        </label>

        <button class="auth-primary-button" type="submit" :disabled="!canSubmit || loading">
          <span>{{ loading ? '创建中' : '创建并进入' }}</span>
          <ArrowRight :size="16" :stroke-width="1.9" />
        </button>
      </form>
    </section>
  </main>
</template>
