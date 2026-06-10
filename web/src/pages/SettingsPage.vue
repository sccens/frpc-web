<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  Download,
  KeyRound,
  RefreshCw,
  RotateCw,
  Save,
  Trash2,
  Upload,
  Filter,
} from 'lucide-vue-next'
import {
  activateFrpcVersion,
  changeAccessKey,
  checkLatestFrpc,
  clearAuditLogs,
  getFrpcVersion,
  getFrpcVersions,
  getSettings,
  exportConfig,
  installFrpcOffline,
  installFrpcOnline,
  importConfig,
  getAuditLogs,
  updateSettings,
  type ConfigBundle,
  type FrpcVersion,
  type Settings,
  type AuditLogPage,
} from '../api/client'
import { errorMessage } from '../utils/errors'

const router = useRouter()
const loading = ref(false)
const saving = ref(false)
const securitySaving = ref(false)
const versionLoading = ref(false)
const installing = ref(false)
const checking = ref(false)
const exporting = ref(false)
const importing = ref(false)
const auditLoading = ref(false)
const settings = ref<Settings | null>(null)
const version = ref<FrpcVersion | null>(null)
const versions = ref<FrpcVersion[]>([])
const auditPage = ref<AuditLogPage>({ items: [], total: 0, page: 1, pageSize: 20 })
const githubProxy = ref('')
const onlineVersion = ref('latest')
const installGithubProxy = ref('')
const fileInput = ref<HTMLInputElement | null>(null)
const importFileInput = ref<HTMLInputElement | null>(null)
const importMode = ref<'merge' | 'replace'>('merge')
const currentAccessKey = ref('')
const newAccessKey = ref('')
const confirmAccessKey = ref('')
const auditAction = ref('')
const auditResult = ref('')

const canChangeAccessKey = computed(
  () =>
    currentAccessKey.value.trim().length >= 8 &&
    newAccessKey.value.trim().length >= 8 &&
    newAccessKey.value === confirmAccessKey.value,
)

onMounted(() => {
  void loadSettings()
  void loadVersions()
  void loadAuditLogs(1)
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

async function loadVersions() {
  versionLoading.value = true
  try {
    const [current, list] = await Promise.all([getFrpcVersion(), getFrpcVersions()])
    version.value = current
    versions.value = list
  } catch (err) {
    ElMessage.error(errorMessage(err, '加载 frpc 版本信息失败'))
  } finally {
    versionLoading.value = false
  }
}

async function loadAuditLogs(nextPage = auditPage.value.page) {
  auditLoading.value = true
  try {
    auditPage.value = await getAuditLogs({
      page: nextPage,
      pageSize: auditPage.value.pageSize,
      action: auditAction.value,
      result: auditResult.value,
    })
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    auditLoading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    settings.value = await updateSettings({ githubProxy: githubProxy.value.trim() })
    githubProxy.value = settings.value.githubProxy || ''
    ElMessage.success('设置已保存')
  } catch (err) {
    ElMessage.error(errorMessage(err, '保存设置失败'))
  } finally {
    saving.value = false
  }
}

async function installOnline() {
  installing.value = true
  try {
    await installFrpcOnline({
      version: onlineVersion.value.trim() || 'latest',
      platform: '',
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
    ElMessage.success('默认版本已切换')
    await loadVersions()
  } catch (err) {
    ElMessage.error(errorMessage(err, '切换版本失败'))
  }
}

async function exportBackup() {
  exporting.value = true
  try {
    await ElMessageBox.confirm('备份文件包含完整配置（含 token 和密码），请妥善保管。', '导出配置', {
      type: 'warning',
      confirmButtonText: '导出',
      cancelButtonText: '取消',
    })
    const bundle = await exportConfig()
    downloadJSON(bundle, `frpc-web-${new Date().toISOString().slice(0, 10)}.json`)
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
    if (importMode.value === 'replace') {
      await ElMessageBox.confirm('将删除当前所有配置', '替换导入', {
        type: 'warning',
        confirmButtonText: '确认',
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
    ElMessage.success('Access Key 已更新')
    await router.replace('/login')
  } catch (err) {
    ElMessage.error(errorMessage(err, '更新失败'))
  } finally {
    securitySaving.value = false
  }
}

function resetAuditFilters() {
  auditAction.value = ''
  auditResult.value = ''
  void loadAuditLogs(1)
}

async function clearAudit() {
  try {
    await ElMessageBox.confirm('将删除全部审计日志记录，且不可恢复。', '清理审计日志', {
      type: 'warning',
      confirmButtonText: '清理',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  auditLoading.value = true
  try {
    await clearAuditLogs()
    ElMessage.success('审计日志已清理')
    await loadAuditLogs(1)
  } catch (err) {
    ElMessage.error(errorMessage(err, '清理失败'))
  } finally {
    auditLoading.value = false
  }
}

function actionLabel(value: string) {
  const labels: Record<string, string> = {
    'auth.login': '登录',
    'auth.access_key': '修改密钥',
    'audit.clear': '清理审计日志',
    'settings.update': '更新设置',
    'servers.start': '启动',
    'servers.reload': '热重载',
    'frpc.install_online': '在线安装',
  }
  return labels[value] || value
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <section class="surface-panel settings-panel" v-loading="versionLoading || installing">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">Binary</p>
          <h2>frpc 版本</h2>
          <span>{{ version?.path || '尚未安装' }}</span>
        </div>
        <div class="toolbar clean">
          <label class="search-box version-input">
            <input v-model="onlineVersion" type="text" placeholder="latest" />
          </label>
          <label class="search-box proxy-input">
            <input v-model="installGithubProxy" type="url" placeholder="代理" />
          </label>
          <button class="ghost-action strong" type="button" :disabled="checking" @click="checkLatest">
            <RotateCw :size="15" :stroke-width="1.8" />
            检查
          </button>
          <button class="primary-action" type="button" :disabled="installing" @click="installOnline">
            <Download :size="15" :stroke-width="1.8" />
            安装
          </button>
          <button class="ghost-action strong" type="button" :disabled="installing" @click="fileInput?.click()">
            <Upload :size="15" :stroke-width="1.8" />
            上传
          </button>
          <input ref="fileInput" class="hidden-input" type="file" @change="pickOfflineFile" />
        </div>
      </div>

      <div class="settings-console single-column">
        <div class="settings-form">
          <label class="settings-control">
            <span class="settings-control-icon"><Download :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">GitHub Proxy</p>
              <strong>默认下载代理</strong>
              <small>用于检查和在线安装 frpc；单次安装输入框会优先覆盖这里。</small>
            </span>
            <input v-model="githubProxy" type="url" placeholder="https://gh-proxy.example.com/" />
            <button class="primary-action wide" type="button" :disabled="saving" @click="saveSettings">
              <Save :size="15" :stroke-width="1.8" />
              保存默认代理
            </button>
          </label>
        </div>
      </div>

      <div class="version-registry">
        <article v-for="item in versions" :key="item.id" class="session-row version-row" :class="{ current: item.active }">
          <div class="settings-row-copy">
            <p class="overline">{{ item.active ? 'Active' : item.source }}</p>
            <strong>{{ item.version }}</strong>
            <span>{{ item.platform }}/{{ item.arch }}</span>
            <div class="session-meta">
              <code>{{ item.path }}</code>
            </div>
          </div>
          <button v-if="!item.active" class="ghost-action strong" type="button" @click="activateVersion(item.id)">
            设为默认
          </button>
        </article>
        <div v-if="versions.length === 0" class="empty-state">暂无已安装版本</div>
      </div>
    </section>

    <section class="surface-panel settings-panel">
      <div class="section-heading settings-heading">
        <div>
          <p class="overline">Security & Backup</p>
          <h2>安全与备份</h2>
          <span>访问密钥管理与配置备份</span>
        </div>
      </div>

      <div class="settings-console">
        <div class="settings-form">
          <label class="settings-control">
            <span class="settings-control-icon"><KeyRound :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">Access Key</p>
              <strong>修改访问密钥</strong>
              <small>修改后需重新登录</small>
            </span>
            <input v-model="currentAccessKey" type="password" placeholder="当前密钥" />
            <input v-model="newAccessKey" type="password" placeholder="新密钥" />
            <input v-model="confirmAccessKey" type="password" placeholder="确认新密钥" />
            <button class="primary-action wide" type="button" :disabled="!canChangeAccessKey || securitySaving" @click="submitAccessKeyChange">
              <Save :size="15" :stroke-width="1.8" />
              更新 Access Key
            </button>
          </label>
        </div>

        <div class="settings-form">
          <button class="primary-action wide" type="button" :disabled="exporting" @click="exportBackup">
            <Download :size="15" :stroke-width="1.8" />
            导出配置
          </button>

          <label class="settings-control">
            <span class="settings-control-icon"><Upload :size="16" :stroke-width="1.7" /></span>
            <span>
              <p class="overline">Import Mode</p>
              <strong>导入模式</strong>
              <small>Merge 追加，Replace 替换</small>
            </span>
            <select v-model="importMode">
              <option value="merge">Merge</option>
              <option value="replace">Replace</option>
            </select>
            <button class="ghost-action strong wide" type="button" :disabled="importing" @click="importFileInput?.click()">
              <Upload :size="15" :stroke-width="1.8" />
              选择文件
            </button>
            <input ref="importFileInput" class="hidden-input" type="file" accept=".json" @change="pickImportFile" />
          </label>
        </div>
      </div>
    </section>

    <section class="surface-panel">
      <div class="section-heading">
        <div>
          <p class="overline">Audit Trail</p>
          <h2>审计日志</h2>
          <span>登录、配置变更、进程操作记录</span>
        </div>
        <div class="row-actions">
          <button class="ghost-action strong" type="button" @click="loadAuditLogs()">
            <RefreshCw :size="15" :stroke-width="1.8" />
            刷新
          </button>
          <button class="ghost-action strong" type="button" :disabled="auditLoading" @click="clearAudit">
            <Trash2 :size="15" :stroke-width="1.8" />
            清理
          </button>
        </div>
      </div>

      <div class="rule-toolbar">
        <select v-model="auditAction" class="native-select compact" @change="loadAuditLogs(1)">
          <option value="">全部动作</option>
          <option value="auth.login">登录</option>
          <option value="servers.start">启动</option>
          <option value="servers.reload">热重载</option>
        </select>
        <select v-model="auditResult" class="native-select compact" @change="loadAuditLogs(1)">
          <option value="">全部结果</option>
          <option value="success">成功</option>
          <option value="failure">失败</option>
        </select>
        <button class="ghost-action strong" type="button" @click="resetAuditFilters">
          <Filter :size="15" :stroke-width="1.8" />
          重置
        </button>
      </div>

      <div class="rule-table-wrap">
        <table class="rule-table">
          <thead>
            <tr>
              <th>结果</th>
              <th>动作</th>
              <th>IP</th>
              <th>时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in auditPage.items" :key="log.id">
              <td>
                <span class="status-badge" :class="log.result === 'success' ? 'is-running' : 'is-error'">
                  <span class="status-dot" />
                  {{ log.result === 'success' ? '成功' : '失败' }}
                </span>
              </td>
              <td><strong>{{ actionLabel(log.action) }}</strong></td>
              <td><code>{{ log.ip || '-' }}</code></td>
              <td>{{ log.createdAt }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="pager-row">
        <span>{{ auditPage.page }} / {{ Math.ceil(auditPage.total / auditPage.pageSize) || 1 }}</span>
        <div class="row-actions">
          <button class="ghost-action strong" type="button" :disabled="auditPage.page <= 1" @click="loadAuditLogs(auditPage.page - 1)">上一页</button>
          <button class="ghost-action strong" type="button" :disabled="auditPage.page >= Math.ceil(auditPage.total / auditPage.pageSize)" @click="loadAuditLogs(auditPage.page + 1)">下一页</button>
        </div>
      </div>
    </section>
  </div>
</template>
