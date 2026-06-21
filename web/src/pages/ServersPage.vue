<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Download, Search, X } from 'lucide-vue-next'
import {
  getServers,
  getServerLogs,
  readConfigFile,
  reloadViaAdmin,
  saveConfigFile,
  type LogLine,
  type ProxyRule,
  type Server,
} from '../api/client'
import { errorMessage } from '../utils/errors'
import ServerTable from '../components/ServerTable.vue'

// nodeName 是规则所属节点的名称；不要占用 serverName——
// 那是 STCP/XTCP visitor 规则自身的字段（要访问的目标服务名）。
type RuleRow = ProxyRule & { nodeName: string }

const loading = ref(false)
const servers = ref<Server[]>([])
const search = ref('')

const allRules = computed<RuleRow[]>(() => {
  const keyword = search.value.trim().toLowerCase()
  const rows = servers.value.flatMap((server) =>
    (server.rules ?? []).map((rule) => ({
      ...rule,
      nodeName: server.name,
    })),
  )
  if (!keyword) return rows
  return rows.filter((rule) =>
    [
      rule.name,
      rule.type,
      rule.nodeName,
      `${rule.localIp}:${rule.localPort}`,
      `${rule.remotePort ?? ''}`,
      rule.serverName ?? '',
      rule.bindAddr ?? '',
      `${rule.bindPort ?? ''}`,
      ...(rule.customDomains ?? []),
    ]
      .join(' ')
      .toLowerCase()
      .includes(keyword),
  )
})

async function loadServers() {
  loading.value = true
  try {
    servers.value = await getServers()
  } catch (err) {
    ElMessage.error(errorMessage(err, '加载服务器列表失败'))
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void loadServers()
})

// ——— 编辑配置文件原文 ———
const configDrawerOpen = ref(false)
const editingServer = ref<Server | null>(null)
const configContent = ref('')
const configWritable = ref(false)
const configPath = ref('')
const savingConfig = ref(false)

async function openEditConfig(server: Server) {
  editingServer.value = server
  configDrawerOpen.value = true
  savingConfig.value = true
  try {
    const file = await readConfigFile(server.id)
    configContent.value = file.content
    configWritable.value = file.writable
    configPath.value = file.path
  } catch (err) {
    ElMessage.error(errorMessage(err, '读取配置失败'))
    configDrawerOpen.value = false
  } finally {
    savingConfig.value = false
  }
}

async function saveConfig() {
  const server = editingServer.value
  if (!server) return
  savingConfig.value = true
  try {
    const result = await saveConfigFile(server.id, configContent.value)
    if (result.ok) {
      ElMessage.success(result.message)
    } else {
      ElMessage.warning(result.message)
    }
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err, '保存配置失败'))
  } finally {
    savingConfig.value = false
  }
}

function downloadConfig() {
  const server = editingServer.value
  if (!server) return
  const blob = new Blob([configContent.value], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = configPath.value.split('/').pop() || 'frpc.toml'
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

// ——— 热重载 ———
const reloadingId = ref('')
async function doReload(server: Server) {
  reloadingId.value = server.id
  try {
    const result = await reloadViaAdmin(server.id)
    if (result.ok) {
      ElMessage.success(result.message)
    } else {
      ElMessage.warning(result.message)
    }
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err, '热重载失败'))
  } finally {
    reloadingId.value = ''
  }
}

// ——— 日志查看 ———
const logDialogOpen = ref(false)
const logServerName = ref('')
const logContent = ref('')
const logLoading = ref(false)

async function openLogs(server: Server) {
  logServerName.value = server.name
  logDialogOpen.value = true
  logLoading.value = true
  try {
    const lines = await getServerLogs(server.id, 500)
    logContent.value = lines
      .map((line: LogLine) => (line.time ? `[${line.time}] ${line.message}` : line.message))
      .join('\n')
  } catch (err) {
    logContent.value = `加载失败: ${errorMessage(err, '未知错误')}`
  } finally {
    logLoading.value = false
  }
}

function remoteTarget(rule: ProxyRule) {
  if (rule.type === 'tcp' || rule.type === 'udp') {
    return rule.remotePort ? `:${rule.remotePort}` : '-'
  }
  if ((rule.type === 'stcp' || rule.type === 'xtcp') && rule.role === 'visitor') {
    return `${rule.bindAddr || '127.0.0.1'}:${rule.bindPort || '-'}`
  }
  if (rule.type === 'stcp' || rule.type === 'xtcp') {
    return rule.serverName ? `secret -> ${rule.serverName}` : 'secret visitor'
  }
  return rule.customDomains?.join(', ') || '-'
}

function localTarget(rule: ProxyRule) {
  if ((rule.type === 'stcp' || rule.type === 'xtcp') && rule.role === 'visitor') {
    return rule.serverName || '-'
  }
  return `${rule.localIp}:${rule.localPort}`
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <ServerTable
      :servers="servers"
      @edit-config="openEditConfig"
      @reload="doReload"
      @logs="openLogs"
    />

    <section class="surface-panel">
      <div class="section-heading">
        <div>
          <p class="overline">Proxy Rules</p>
          <h2>代理规则</h2>
          <span>只读展示配置文件中解析出的代理规则</span>
        </div>
      </div>

      <div class="rule-toolbar">
        <label class="search-box">
          <Search class="field-icon" :size="15" :stroke-width="1.7" />
          <input v-model="search" type="search" placeholder="搜索规则名称、端口或域名..." />
        </label>
      </div>

      <div class="rule-table-wrap">
        <table class="rule-table">
          <thead>
            <tr>
              <th>状态</th>
              <th>规则名称</th>
              <th>协议</th>
              <th>所属节点</th>
              <th>内网服务源</th>
              <th>公网映射入口</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in allRules" :key="rule.id">
              <td>
                <span class="rule-toggle" :class="{ active: rule.enabled }" />
              </td>
              <td><strong>{{ rule.name }}</strong></td>
              <td><span class="protocol-pill">{{ rule.type.toUpperCase() }}</span></td>
              <td :title="rule.nodeName">{{ rule.nodeName }}</td>
              <td><code>{{ localTarget(rule) }}</code></td>
              <td><code>{{ remoteTarget(rule) }}</code></td>
            </tr>
          </tbody>
        </table>
        <div v-if="allRules.length === 0" class="empty-state">暂无代理规则</div>
      </div>
    </section>

    <Teleport to="body">
      <div v-if="configDrawerOpen" class="drawer-layer">
        <button class="drawer-backdrop" type="button" aria-label="关闭" @click="configDrawerOpen = false" />
        <aside class="rule-drawer config-drawer">
          <header class="drawer-header">
            <div>
              <p class="overline">Config File</p>
              <h2>编辑配置文件</h2>
              <span class="version-path" :title="configPath">{{ configPath }}</span>
            </div>
            <button class="icon-button ghost" type="button" aria-label="关闭" @click="configDrawerOpen = false">
              <X :size="16" :stroke-width="1.8" />
            </button>
          </header>

          <div class="drawer-body">
            <el-alert
              v-if="!configWritable"
              type="warning"
              :closable="false"
              title="该配置文件不可写"
              description="保存按钮已禁用。可下载修改后的内容，或按部署开启可写权限。"
              show-icon
              style="margin-bottom: 12px"
            />
            <el-alert
              v-else
              type="info"
              :closable="false"
              title="保存只写盘，不会自动重载"
              description="保存后点卡片上的「热重载」（需启用 admin API）或重启 frpc 服务。"
              show-icon
              style="margin-bottom: 12px"
            />
            <textarea
              v-model="configContent"
              class="config-editor"
              spellcheck="false"
              :disabled="savingConfig"
              rows="22"
            />
          </div>

          <footer class="drawer-footer">
            <button class="ghost-action strong" type="button" :disabled="savingConfig" @click="downloadConfig">
              <Download :size="15" :stroke-width="1.8" />
              下载
            </button>
            <button
              class="primary-action wide"
              type="button"
              :disabled="!configWritable || savingConfig"
              @click="saveConfig"
            >
              {{ savingConfig ? '保存中…' : '保存配置' }}
            </button>
          </footer>
        </aside>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="logDialogOpen" class="log-overlay" @click="logDialogOpen = false">
        <div class="log-dialog" @click.stop>
          <div class="log-header">
            <div>
              <h3>{{ logServerName }} 日志</h3>
              <span>最近 500 行</span>
            </div>
            <button class="icon-button" type="button" aria-label="关闭" @click="logDialogOpen = false">
              <X :size="18" :stroke-width="2" />
            </button>
          </div>
          <div class="log-body" v-loading="logLoading">
            <pre class="log-content">{{ logContent || '暂无日志' }}</pre>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
/* el-alert 的 description 必须完整换行显示，不能被父容器裁切 */
:deep(.el-alert__description) {
  white-space: normal;
  word-break: break-word;
  overflow: visible;
  line-height: 1.5;
}

.config-drawer {
  width: 760px;
  max-width: 94vw;
}

.config-editor {
  width: 100%;
  min-height: 420px;
  padding: 12px 14px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: var(--code-bg);
  color: var(--text);
  font-family: 'SF Mono', 'Monaco', 'Cascadia Code', 'Consolas', monospace;
  font-size: 12.5px;
  line-height: 1.6;
  resize: vertical;
}

.config-editor:focus {
  outline: none;
  border-color: var(--blue);
}

.log-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2000;
}

.log-dialog {
  width: 90%;
  max-width: 1000px;
  height: 80vh;
  max-height: 700px;
  background: var(--el-bg-color);
  border-radius: 8px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
}

.log-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--el-border-color);
}

.log-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.log-header span {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: 8px;
}

.log-body {
  flex: 1;
  overflow: auto;
  padding: 16px;
  background: var(--el-fill-color-light);
}

.log-content {
  font-family: 'SF Mono', 'Monaco', 'Cascadia Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
  margin: 0;
  white-space: pre-wrap;
  word-wrap: break-word;
}

@media (max-width: 720px) {
  .config-drawer {
    width: 100%;
  }

  .log-overlay {
    align-items: flex-end;
    padding: 10px;
  }

  .log-dialog {
    width: 100%;
    height: min(78vh, 640px);
    border-radius: 18px;
  }
}
</style>
