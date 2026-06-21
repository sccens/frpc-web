<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Download, KeyRound, RefreshCw, Save, Upload } from 'lucide-vue-next'
import {
  applyAppUpdate,
  changeAccessKey,
  checkAppUpdate,
  exportConfig,
  getAuthStatus,
  getSettings,
  importConfig,
  updateSettings,
  type ConfigBundle,
  type Settings,
  type UpdateCheck,
} from '../api/client'
import { errorMessage } from '../utils/errors'

const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const securitySaving = ref(false)
const exporting = ref(false)
const importing = ref(false)
const settings = ref<Settings | null>(null)
const githubProxy = ref('')
const importFileInput = ref<HTMLInputElement | null>(null)
const currentAccessKey = ref('')
const newAccessKey = ref('')
const confirmAccessKey = ref('')

const canChangeAccessKey = computed(
  () =>
    currentAccessKey.value.trim().length >= 8 &&
    newAccessKey.value.trim().length >= 8 &&
    newAccessKey.value === confirmAccessKey.value,
)

onMounted(() => {
  void loadSettings()
  void checkForUpdate(true)
})

async function loadSettings() {
  loading.value = true
  try {
    settings.value = await getSettings()
    githubProxy.value = settings.value.githubProxy || ''
  } catch (err) {
    ElMessage.error(errorMessage(err, '加载设置失败'))
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    settings.value = await updateSettings({ githubProxy: githubProxy.value.trim() })
    githubProxy.value = settings.value.githubProxy || ''
    ElMessage.success('下载代理已保存')
  } catch (err) {
    ElMessage.error(errorMessage(err, '保存设置失败'))
  } finally {
    saving.value = false
  }
}

async function exportBackup() {
  exporting.value = true
  try {
    await ElMessageBox.confirm('导出包含所有扫描到的配置文件原文（含明文 token 与密码），请妥善保管。', '导出配置', {
      type: 'warning',
      confirmButtonText: '导出',
      cancelButtonText: '取消',
    })
    const bundle = await exportConfig()
    downloadJSON(bundle, `frpc-web-config-${new Date().toISOString().slice(0, 10)}.json`)
    ElMessage.success('配置已导出')
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(errorMessage(err, '导出失败'))
    }
  } finally {
    exporting.value = false
  }
}

async function pickImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importing.value = true
  try {
    await ElMessageBox.confirm(
      '将把备份中的每个配置文件原文写回其路径，覆盖同名文件；不在扫描范围内或不可写的会跳过。',
      '导入配置',
      { type: 'warning', confirmButtonText: '导入', cancelButtonText: '取消' },
    )
    const text = await file.text()
    const bundle = JSON.parse(text) as ConfigBundle
    const result = await importConfig({ bundle })
    if (result.ok) {
      ElMessage.success(result.message || '配置已导入')
    } else {
      ElMessage.warning(result.message || '导入未完全成功')
    }
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(errorMessage(err, '导入失败'))
    }
  } finally {
    importing.value = false
    input.value = ''
  }
}

function downloadJSON(payload: unknown, filename: string) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

async function submitAccessKeyChange() {
  if (!canChangeAccessKey.value) return
  securitySaving.value = true
  try {
    await changeAccessKey({
      currentAccessKey: currentAccessKey.value.trim(),
      newAccessKey: newAccessKey.value.trim(),
    })
    ElMessage.success('访问密钥已更新')
    await router.replace('/login')
  } catch (err) {
    ElMessage.error(errorMessage(err, '更新失败'))
  } finally {
    securitySaving.value = false
  }
}

const updateInfo = ref<UpdateCheck | null>(null)
const updateChecking = ref(false)
const updating = ref(false)

const updateState = computed(() => {
  if (updateChecking.value) {
    return {
      eyebrow: '检查中',
      title: '正在检查更新',
      description: '正在连接 GitHub Releases 获取最新发布版本。',
      tone: 'checking',
    }
  }

  const info = updateInfo.value
  if (!info) {
    return {
      eyebrow: '状态',
      title: '等待检查',
      description: '点击检查更新获取 frpc-web 最新 Release。',
      tone: 'idle',
    }
  }

  if (info.hasUpdate && !info.canApply) {
    return {
      eyebrow: '手动更新',
      title: `发现新版本 ${info.latest}`,
      description: info.applyHint || '当前环境需要手动升级。',
      tone: 'warning',
    }
  }

  if (info.hasUpdate) {
    return {
      eyebrow: '可更新',
      title: `发现新版本 ${info.latest}`,
      description: '更新会校验发布签名与 SHA256，随后原地重启服务。',
      tone: 'available',
    }
  }

  return {
    eyebrow: '已最新',
    title: '已是最新版本',
    description: '当前版本不低于 GitHub 最新发布版本。',
    tone: 'current',
  }
})

async function checkForUpdate(silent = false) {
  updateChecking.value = true
  try {
    updateInfo.value = await checkAppUpdate()
    if (!silent) {
      if (updateInfo.value.hasUpdate) {
        ElMessage.success(`发现新版本 ${updateInfo.value.latest}`)
      } else {
        ElMessage.info('当前已是最新版本')
      }
    }
  } catch (err) {
    if (!silent) ElMessage.error(errorMessage(err, '检查更新失败'))
  } finally {
    updateChecking.value = false
  }
}

async function performUpdate() {
  const info = updateInfo.value
  if (!info?.hasUpdate) return
  try {
    await ElMessageBox.confirm(
      `将更新到 ${info.latest} 并自动重启服务（PID 不变）。`,
      '一键更新',
      { type: 'warning', confirmButtonText: '立即更新', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  updating.value = true
  try {
    const result = await applyAppUpdate()
    if (!result.ok) {
      ElMessage.error(result.message)
      updating.value = false
      return
    }
    ElMessage.success(result.message)
    awaitRestartThenReload()
  } catch (err) {
    ElMessage.error(errorMessage(err, '更新失败'))
    updating.value = false
  }
}

// 等服务重启完成后刷新页面加载新版前端；最多等 60 秒
function awaitRestartThenReload() {
  const startedAt = Date.now()
  window.setTimeout(function probe() {
    getAuthStatus()
      .then(() => window.location.reload())
      .catch(() => {
        if (Date.now() - startedAt > 60000) {
          window.location.reload()
          return
        }
        window.setTimeout(probe, 1500)
      })
  }, 3000)
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <section class="surface-panel settings-panel">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">安全</p>
          <h2>安全</h2>
          <span>访问密钥管理与配置备份</span>
        </div>
      </div>

      <div class="settings-console">
        <div class="settings-form">
          <label class="settings-control">
            <span class="settings-control-icon"><KeyRound :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">访问密钥</p>
              <strong>修改访问密钥</strong>
              <small>修改后需重新登录</small>
            </span>
            <input v-model="currentAccessKey" type="password" placeholder="当前密钥" />
            <input v-model="newAccessKey" type="password" placeholder="新密钥（8-20 位，含大小写字母与数字）" />
            <input v-model="confirmAccessKey" type="password" placeholder="确认新密钥" />
            <button class="primary-action wide" type="button" :disabled="!canChangeAccessKey || securitySaving" @click="submitAccessKeyChange">
              <Save :size="15" :stroke-width="1.8" />
              更新访问密钥
            </button>
          </label>
        </div>

        <div class="settings-form">
          <div class="settings-control">
            <span class="settings-control-icon"><Download :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">配置备份</p>
              <strong>配置备份</strong>
              <small>导出所有扫描到的配置文件原文；导入时写回各自路径</small>
            </span>
            <button class="primary-action wide" type="button" :disabled="exporting" @click="exportBackup">
              <Download :size="15" :stroke-width="1.8" />
              导出配置
            </button>

            <button class="ghost-action strong wide" type="button" :disabled="importing" @click="importFileInput?.click()">
              <Upload :size="15" :stroke-width="1.8" />
              {{ importing ? '导入中…' : '导入配置' }}
            </button>
            <input ref="importFileInput" class="hidden-input" type="file" accept=".json" @change="pickImportFile" />
          </div>
        </div>
      </div>
    </section>

    <section class="surface-panel settings-panel">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">系统</p>
          <h2>系统更新</h2>
          <span>frpc-web 自身的版本检查与一键更新；下载代理用于访问 GitHub</span>
        </div>
      </div>

      <div class="settings-console single-column">
        <div class="settings-form">
          <label class="settings-control">
            <span class="settings-control-icon"><Download :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">下载代理</p>
              <strong>下载代理</strong>
              <small>检查/下载更新时使用；留空则直连或走 FRPC_WEB_GITHUB_PROXY 环境变量。</small>
            </span>
            <input v-model="githubProxy" type="url" placeholder="https://gh-proxy.example.com/" />
            <button class="primary-action wide" type="button" :disabled="saving" @click="saveSettings">
              <Save :size="15" :stroke-width="1.8" />
              保存代理
            </button>
          </label>
        </div>
      </div>

      <div class="system-update-board">
        <div class="system-version-grid">
          <div class="system-version-item">
            <span>当前版本</span>
            <strong>{{ updateInfo?.current || '读取中' }}</strong>
          </div>
          <a
            v-if="updateInfo"
            class="system-version-item system-version-link"
            :href="updateInfo.notesUrl"
            target="_blank"
            rel="noopener"
          >
            <span>最新版本</span>
            <strong>{{ updateInfo.latest }}</strong>
            <small>查看发布说明</small>
          </a>
          <div v-else class="system-version-item">
            <span>最新版本</span>
            <strong>待检查</strong>
            <small>检查后显示</small>
          </div>
        </div>

        <div class="system-update-status" :class="`status-${updateState.tone}`">
          <span class="system-update-indicator"><span /></span>
          <div class="settings-row-copy">
            <p class="overline">{{ updateState.eyebrow }}</p>
            <strong>{{ updateState.title }}</strong>
            <span>{{ updateState.description }}</span>
          </div>
        </div>

        <div class="system-update-actions">
          <button class="ghost-action strong" type="button" :disabled="updateChecking" @click="checkForUpdate()">
            <RefreshCw :size="15" :stroke-width="1.8" :class="{ 'spin-icon': updateChecking }" />
            {{ updateChecking ? '检查中…' : '检查更新' }}
          </button>
          <button
            v-if="updateInfo?.hasUpdate && updateInfo?.canApply"
            class="primary-action"
            type="button"
            :disabled="updating"
            @click="performUpdate"
          >
            <Download :size="15" :stroke-width="1.8" />
            {{ updating ? '更新中…' : `一键更新到 ${updateInfo.latest}` }}
          </button>
          <p v-else-if="updateInfo?.hasUpdate && !updateInfo.canApply" class="system-update-note">需要手动更新</p>
          <p v-else-if="updateInfo" class="system-update-note">无需操作</p>
        </div>
      </div>
    </section>
  </div>
</template>
