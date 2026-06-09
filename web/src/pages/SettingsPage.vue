<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  AlertTriangle,
  CheckCircle2,
  Download,
  FileCog,
  FolderOpen,
  Globe2,
  KeyRound,
  Laptop,
  Lock,
  Monitor,
  RotateCw,
  Save,
  ShieldAlert,
  Trash2,
  Upload,
} from 'lucide-vue-next'
import {
  activateFrpcVersion,
  changeAccessKey,
  checkLatestFrpc,
  getFrpcVersion,
  getFrpcVersions,
  getSessions,
  getSettings,
  exportConfig,
  installFrpcOffline,
  installFrpcOnline,
  importConfig,
  revokeSession,
  updateSettings,
  type ConfigBundle,
  type FrpcVersion,
  type Session,
  type Settings,
} from '../api/client'

const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const securitySaving = ref(false)
const versionLoading = ref(false)
const installing = ref(false)
const checking = ref(false)
const exporting = ref(false)
const importing = ref(false)
const settings = ref<Settings | null>(null)
const sessions = ref<Session[]>([])
const version = ref<FrpcVersion | null>(null)
const versions = ref<FrpcVersion[]>([])
const githubProxy = ref('')
const onlineVersion = ref('latest')
const installGithubProxy = ref('')
const latestVersion = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const importFileInput = ref<HTMLInputElement | null>(null)
const includeSensitiveExport = ref(false)
const importMode = ref<'merge' | 'replace'>('merge')
const currentAccessKey = ref('')
const newAccessKey = ref('')
const confirmAccessKey = ref('')

const installedCount = computed(() => versions.value.filter((item) => item.installed).length)
const currentSession = computed(() => sessions.value.find((item) => item.current) ?? sessions.value[0])
const otherSessions = computed(() => sessions.value.filter((item) => item.id !== currentSession.value?.id))

const canChangeAccessKey = computed(
  () =>
    currentAccessKey.value.trim().length >= 8 &&
    newAccessKey.value.trim().length >= 8 &&
    newAccessKey.value === confirmAccessKey.value,
)

const isPublicBind = computed(() => {
  const addr = settings.value?.addr ?? ''
  return addr.startsWith('0.0.0.0:') || addr.startsWith('[::]:') || addr.startsWith(':')
})

const rows = computed(() => [
  {
    label: 'Web Listen',
    title: settings.value?.addr || '-',
    description: '通过 FRPC_WEB_ADDR 修改，重启 frpc-web 后生效。',
    icon: Monitor,
  },
  {
    label: 'Data Directory',
    title: settings.value?.dataDir || '-',
    description: '通过 FRPC_WEB_DATA_DIR 修改，包含数据库、配置和日志。',
    icon: FolderOpen,
  },
  {
    label: 'Systemd Env',
    title: '/opt/frpc-web/frpc-web.env',
    description: 'Linux systemd 安装后编辑该文件，并重启 frpc-web 服务让环境变量生效。',
    icon: FileCog,
  },
  {
    label: 'Authentication',
    title: 'JWT Cookie',
    description: settings.value?.authNotice || '-',
    icon: Lock,
    tone: isPublicBind.value ? 'warning' : 'default',
  },
])

onMounted(() => {
  void loadSettings()
  void loadSessions()
  void loadVersions()
})

async function loadSettings() {
  loading.value = true
  try {
    settings.value = await getSettings()
    githubProxy.value = settings.value.githubProxy || ''
  } finally {
    loading.value = false
  }
}

async function loadVersions() {
  versionLoading.value = true
  try {
    const [current, list] = await Promise.all([getFrpcVersion(), getFrpcVersions()])
    version.value = current
    versions.value = list
  } finally {
    versionLoading.value = false
  }
}

async function loadSessions() {
  try {
    sessions.value = await getSessions()
  } catch (err) {
    ElMessage.error(errorMessage(err, '读取会话失败'))
  }
}

async function saveSettings() {
  saving.value = true
  try {
    settings.value = await updateSettings({
      githubProxy: githubProxy.value.trim(),
      logAutoRefresh: settings.value?.logAutoRefresh ?? false,
      logRefreshInterval: settings.value?.logRefreshInterval || 5,
    })
    githubProxy.value = settings.value.githubProxy || ''
    ElMessage.success('设置已保存')
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    saving.value = false
  }
}

async function installOnline() {
  installing.value = true
  try {
    await installFrpcOnline({
      version: onlineVersion.value.trim() || 'latest',
      platform: 'linux',
      arch: '',
      githubProxy: installGithubProxy.value.trim(),
    })
    ElMessage.success('frpc 在线安装完成')
    await loadVersions()
  } catch (err) {
    ElMessage.error(errorMessage(err, '在线安装失败'))
  } finally {
    installing.value = false
  }
}

async function checkLatest() {
  checking.value = true
  try {
    const result = await checkLatestFrpc({ githubProxy: installGithubProxy.value.trim() })
    latestVersion.value = result.latest
    ElMessage.success(`最新版本：${result.latest}`)
  } catch (err) {
    ElMessage.error(errorMessage(err, '检查最新版本失败'))
  } finally {
    checking.value = false
  }
}

async function pickOfflineFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  installing.value = true
  try {
    await installFrpcOffline(file)
    ElMessage.success('frpc 离线安装完成')
    await loadVersions()
  } catch (err) {
    ElMessage.error(errorMessage(err, '离线安装失败'))
  } finally {
    installing.value = false
    input.value = ''
  }
}

async function activateVersion(versionId: string) {
  try {
    await activateFrpcVersion(versionId)
    ElMessage.success('默认版本已切换，重启实例后生效')
    await loadVersions()
  } catch (err) {
    ElMessage.error(errorMessage(err, '切换版本失败'))
  }
}

async function exportBackup() {
  exporting.value = true
  try {
    if (includeSensitiveExport.value) {
      await ElMessageBox.confirm('导出文件将包含 frps token、Admin 密码、STCP/XTCP secretKey 等敏感信息。', '包含敏感信息导出', {
        type: 'warning',
        confirmButtonText: '继续导出',
        cancelButtonText: '取消',
      })
    }
    const bundle = await exportConfig(includeSensitiveExport.value)
    downloadJSON(bundle, `frpc-web-config-${new Date().toISOString().slice(0, 10)}.json`)
    ElMessage.success('配置已导出')
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(errorMessage(err, '导出配置失败'))
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
    if (importMode.value === 'replace') {
      await ElMessageBox.confirm('Replace 会停止并删除当前所有服务器配置，再导入备份内容。', '替换导入', {
        type: 'warning',
        confirmButtonText: '替换导入',
        cancelButtonText: '取消',
      })
    }
    const text = await file.text()
    const bundle = JSON.parse(text) as ConfigBundle
    const result = await importConfig({ mode: importMode.value, bundle })
    ElMessage.success(result.message || '配置已导入')
    await Promise.all([loadSettings(), loadVersions()])
  } catch (err) {
    if (err !== 'cancel') {
      ElMessage.error(errorMessage(err, '导入配置失败'))
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
    ElMessage.success('Access Key 已更新，请重新登录')
    await router.replace('/login')
  } catch (err) {
    ElMessage.error(errorMessage(err, '更新 Access Key 失败'))
  } finally {
    securitySaving.value = false
  }
}

async function revoke(id: string, current?: boolean) {
  securitySaving.value = true
  try {
    await revokeSession(id)
    ElMessage.success('会话已撤销')
    if (current) {
      await router.replace('/login')
      return
    }
    await loadSessions()
  } catch (err) {
    ElMessage.error(errorMessage(err, '撤销会话失败'))
  } finally {
    securitySaving.value = false
  }
}

function sessionTitle(session: Session) {
  if (session.current) return '当前会话'
  return session.ip || '未知来源'
}

function shortUserAgent(value: string) {
  if (!value) return '-'
  return value.length > 82 ? `${value.slice(0, 82)}...` : value
}

function errorMessage(err: unknown, fallback = '保存失败') {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || fallback
  }
  return err instanceof Error ? err.message : fallback
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <section class="surface-panel settings-panel">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">System</p>
          <h2>设置</h2>
          <span>本机运行参数、默认下载策略和安全状态</span>
        </div>
        <button class="primary-action" type="button" :disabled="saving" @click="saveSettings">
          <Save :size="15" :stroke-width="1.8" />
          保存
        </button>
      </div>

      <div v-if="isPublicBind" class="security-band">
        <ShieldAlert :size="18" :stroke-width="1.8" />
        <div>
          <strong>当前监听公网地址</strong>
          <p>登录认证已启用；公网访问仍建议叠加 HTTPS、反向代理认证或网络访问控制。</p>
        </div>
      </div>

      <div class="settings-console">
        <div class="settings-list">
          <article v-for="row in rows" :key="row.label" class="settings-row" :class="`tone-${row.tone || 'default'}`">
            <span class="settings-row-icon">
              <component :is="row.icon" :size="17" :stroke-width="1.7" />
            </span>
            <div class="settings-row-copy">
              <p class="overline">{{ row.label }}</p>
              <strong>{{ row.title }}</strong>
              <span>{{ row.description }}</span>
            </div>
          </article>
        </div>

        <div class="settings-form">
          <label class="settings-control">
            <span class="settings-control-icon"><Globe2 :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">GitHub Proxy</p>
              <strong>默认下载代理</strong>
              <small>frpc 在线安装和检查最新版本留空时使用；低于本次操作代理优先级。</small>
            </span>
            <input v-model="githubProxy" placeholder="https://proxyd.example.com/" />
          </label>
        </div>
      </div>
    </section>

    <section class="surface-panel settings-panel">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">Backup</p>
          <h2>配置导入 / 导出</h2>
          <span>用于迁移数据目录或备份服务器与代理规则配置</span>
        </div>
      </div>

      <div class="settings-console">
        <div class="settings-list">
          <article class="settings-row">
            <span class="settings-row-icon">
              <Download :size="17" :stroke-width="1.7" />
            </span>
            <div class="settings-row-copy">
              <p class="overline">Export</p>
              <strong>导出当前配置</strong>
              <span>默认会脱敏 token、Admin 密码、STCP/XTCP secretKey 和 HTTP Basic Auth 密码。</span>
            </div>
          </article>
          <article class="settings-row">
            <span class="settings-row-icon">
              <Upload :size="17" :stroke-width="1.7" />
            </span>
            <div class="settings-row-copy">
              <p class="overline">Import</p>
              <strong>导入备份 JSON</strong>
              <span>Merge 会追加导入；Replace 会先删除当前服务器配置后再恢复备份。</span>
            </div>
          </article>
        </div>

        <div class="settings-form">
          <label class="settings-control inline">
            <span class="settings-control-icon"><ShieldAlert :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">Sensitive</p>
              <strong>包含敏感信息</strong>
              <small>仅在你需要完整迁移 token 和密钥时启用，导出文件请妥善保存。</small>
            </span>
            <input v-model="includeSensitiveExport" class="native-checkbox" type="checkbox" />
          </label>

          <button class="primary-action wide" type="button" :disabled="exporting" @click="exportBackup">
            <Download :size="15" :stroke-width="1.8" />
            导出配置
          </button>

          <label class="settings-control">
            <span class="settings-control-icon"><Upload :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">Import Mode</p>
              <strong>导入模式</strong>
              <small>首选 Merge；Replace 适合空数据目录或完整恢复。</small>
            </span>
            <select v-model="importMode">
              <option value="merge">Merge</option>
              <option value="replace">Replace</option>
            </select>
            <button class="ghost-action strong wide" type="button" :disabled="importing" @click="importFileInput?.click()">
              <Upload :size="15" :stroke-width="1.8" />
              选择备份文件
            </button>
            <input ref="importFileInput" class="hidden-input" type="file" accept="application/json,.json" @change="pickImportFile" />
          </label>
        </div>
      </div>
    </section>

    <section class="surface-panel settings-panel" v-loading="versionLoading || installing">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">Binary</p>
          <h2>frpc 版本</h2>
          <span>{{ version?.path || '尚未安装 frpc' }}</span>
        </div>
        <div class="toolbar clean">
          <label class="search-box version-input">
            <input v-model="onlineVersion" type="text" placeholder="latest 或 0.69.1" />
          </label>
          <label class="search-box proxy-input">
            <input v-model="installGithubProxy" type="url" placeholder="本次 GitHub 代理，可留空" />
          </label>
          <button class="ghost-action strong" type="button" :disabled="checking || installing" @click="checkLatest">
            <RotateCw :size="15" :stroke-width="1.8" />
            检查最新
          </button>
          <button class="primary-action" type="button" :disabled="installing" @click="installOnline">
            <Download :size="15" :stroke-width="1.8" />
            在线安装
          </button>
          <button class="ghost-action strong" type="button" :disabled="installing" @click="fileInput?.click()">
            <Upload :size="15" :stroke-width="1.8" />
            离线上传
          </button>
          <input ref="fileInput" class="hidden-input" type="file" accept=".tar.gz,.tgz,frpc,*" @change="pickOfflineFile" />
        </div>
      </div>

      <div class="security-band compact version-upload-note">
        <AlertTriangle :size="17" :stroke-width="1.8" />
        <div>
          <strong>离线上传会执行校验</strong>
          <p>上传的 frpc 二进制或压缩包会被后端执行 <code>frpc --version</code> 读取版本，请只上传可信来源文件。</p>
        </div>
      </div>

      <div class="version-settings-grid">
        <div class="version-summary">
          <div class="version-detail settings-version-detail" v-if="version">
            <div class="version-card featured">
              <p class="overline">Current</p>
              <strong>{{ version.version }}</strong>
              <span>当前默认版本</span>
            </div>
            <div class="version-card">
              <p class="overline">Latest</p>
              <strong>{{ latestVersion || version.latest || '-' }}</strong>
              <span>手动检查结果</span>
            </div>
            <div class="version-card">
              <p class="overline">Installed</p>
              <strong>{{ installedCount }}</strong>
              <span>已登记二进制</span>
            </div>
            <div class="version-card">
              <p class="overline">State</p>
              <strong>{{ version.installed ? 'Ready' : 'Missing' }}</strong>
              <span class="inline-state">
                <CheckCircle2 v-if="version.installed" :size="15" :stroke-width="1.8" />
                <AlertTriangle v-else :size="15" :stroke-width="1.8" />
                {{ version.installed ? '已安装' : '未安装' }}
              </span>
            </div>
          </div>
          <div v-else class="empty-state">尚未登记 frpc 版本</div>
        </div>

        <div class="version-registry">
          <div class="session-list-head">
            <div>
              <p class="overline">Installed</p>
              <strong>已安装版本</strong>
            </div>
            <button class="ghost-action" type="button" :disabled="versionLoading" @click="loadVersions">
              <RotateCw :size="15" :stroke-width="1.8" />
              刷新
            </button>
          </div>

          <article v-for="item in versions" :key="item.id" class="session-row version-row" :class="{ current: item.active }">
            <span class="settings-row-icon">
              <CheckCircle2 v-if="item.active" :size="17" :stroke-width="1.7" />
              <AlertTriangle v-else :size="17" :stroke-width="1.7" />
            </span>
            <div class="settings-row-copy">
              <p class="overline">{{ item.active ? 'Active' : item.source }}</p>
              <strong>{{ item.version }}</strong>
              <span>{{ item.platform }}/{{ item.arch }} · {{ item.source }}</span>
              <div class="session-meta">
                <code>{{ item.path }}</code>
              </div>
            </div>
            <button class="ghost-action strong" type="button" :disabled="item.active || installing" @click="activateVersion(item.id)">
              设为默认
            </button>
          </article>

          <div v-if="versions.length === 0" class="empty-state">暂无已安装版本</div>
        </div>
      </div>
    </section>

    <section class="surface-panel settings-panel">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">Security</p>
          <h2>Access Key 与会话</h2>
          <span>单管理员密钥控制台，支持服务端会话撤销</span>
        </div>
        <button class="ghost-action strong" type="button" :disabled="securitySaving" @click="loadSessions">
          <RotateCw :size="15" :stroke-width="1.8" />
          刷新会话
        </button>
      </div>

      <div class="settings-console security-console">
        <div class="settings-form">
          <label class="settings-control">
            <span class="settings-control-icon"><KeyRound :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">Access Key</p>
              <strong>修改访问密钥</strong>
              <small>修改成功后会撤销全部会话，并要求重新登录。</small>
            </span>
            <input v-model="currentAccessKey" type="password" autocomplete="current-password" placeholder="当前 Access Key" />
            <input v-model="newAccessKey" type="password" autocomplete="new-password" placeholder="新的 Access Key" />
            <input v-model="confirmAccessKey" type="password" autocomplete="new-password" placeholder="确认新的 Access Key" />
            <button class="primary-action wide" type="button" :disabled="!canChangeAccessKey || securitySaving" @click="submitAccessKeyChange">
              <Save :size="15" :stroke-width="1.8" />
              更新 Access Key
            </button>
          </label>
        </div>

        <div class="session-list">
          <div class="session-list-head">
            <div>
              <p class="overline">Sessions</p>
              <strong>当前会话</strong>
            </div>
          </div>

          <article v-if="currentSession" class="session-row current" :class="{ revoked: currentSession.revokedAt }">
            <span class="settings-row-icon">
              <Laptop :size="17" :stroke-width="1.7" />
            </span>
            <div class="settings-row-copy">
              <p class="overline">Current</p>
              <strong>{{ sessionTitle(currentSession) }}</strong>
              <span>{{ shortUserAgent(currentSession.userAgent) }}</span>
              <div class="session-meta">
                <code>{{ currentSession.ip || '-' }}</code>
                <code>last {{ currentSession.lastAccessAt || '-' }}</code>
                <code>exp {{ currentSession.expiresAt || '-' }}</code>
              </div>
            </div>
            <button
              class="icon-button danger"
              type="button"
              aria-label="撤销会话"
              :disabled="securitySaving || Boolean(currentSession.revokedAt)"
              @click="revoke(currentSession.id, true)"
            >
              <Trash2 :size="15" :stroke-width="1.8" />
            </button>
          </article>

          <details v-if="otherSessions.length > 0" class="advanced-panel session-advanced">
            <summary>
              <Laptop :size="15" :stroke-width="1.8" />
              其它会话
            </summary>
            <article v-for="session in otherSessions" :key="session.id" class="session-row" :class="{ revoked: session.revokedAt }">
              <span class="settings-row-icon">
                <Laptop :size="17" :stroke-width="1.7" />
              </span>
              <div class="settings-row-copy">
                <p class="overline">{{ session.revokedAt ? 'Revoked' : 'Session' }}</p>
                <strong>{{ sessionTitle(session) }}</strong>
                <span>{{ shortUserAgent(session.userAgent) }}</span>
                <div class="session-meta">
                  <code>{{ session.ip || '-' }}</code>
                  <code>last {{ session.lastAccessAt || '-' }}</code>
                  <code>exp {{ session.expiresAt || '-' }}</code>
                </div>
              </div>
              <button
                class="icon-button danger"
                type="button"
                aria-label="撤销会话"
                :disabled="securitySaving || Boolean(session.revokedAt)"
                @click="revoke(session.id, false)"
              >
                <Trash2 :size="15" :stroke-width="1.8" />
              </button>
            </article>
          </details>

          <div v-if="sessions.length === 0" class="empty-state">暂无会话</div>
        </div>
      </div>
    </section>
  </div>
</template>
