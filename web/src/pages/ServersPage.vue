<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { FlaskConical, Pencil, Plus, Search, SlidersHorizontal, Trash2, X } from 'lucide-vue-next'
import {
  checkServer,
  createRule,
  createServer,
  deleteRule,
  deleteServer,
  getMe,
  getServers,
  reloadServer,
  startServer,
  stopServer,
  updateRule,
  updateServer,
  type ProxyRule,
  type ProxyRuleInput,
  type Server,
  type ServerInput,
  type User,
} from '../api/client'
import ServerTable from '../components/ServerTable.vue'

type RuleRow = ProxyRule & { serverName: string }

interface RuleForm extends ProxyRuleInput {
  id: string
  serverId: string
  customDomainsText: string
  locationsText: string
  requestHeadersText: string
}

const loading = ref(false)
const saving = ref(false)
const servers = ref<Server[]>([])
const search = ref('')
const serverDrawerOpen = ref(false)
const ruleDrawerOpen = ref(false)
const editingServerId = ref('')
const editingRuleId = ref('')
const currentUser = ref<User | null>(null)
const showExperimentalConfigMode = ref(false)

const serverForm = ref<ServerInput>(defaultServerForm())
const ruleForm = ref<RuleForm>(defaultRuleForm())
const canOperate = computed(() => Boolean(currentUser.value?.enabled))
const isTCPUDP = computed(() => ruleForm.value.type === 'tcp' || ruleForm.value.type === 'udp')
const isHTTP = computed(() => ruleForm.value.type === 'http' || ruleForm.value.type === 'https')
const isSecretRule = computed(() => ruleForm.value.type === 'stcp' || ruleForm.value.type === 'xtcp')
const isVisitorRule = computed(() => isSecretRule.value && ruleForm.value.role === 'visitor')
const showStoreModeOption = computed(() => showExperimentalConfigMode.value || serverForm.value.configMode === 'store_api')

const allRules = computed<RuleRow[]>(() => {
  const keyword = search.value.trim().toLowerCase()
  const rows = servers.value.flatMap((server) =>
    (server.rules ?? []).map((rule) => ({
      ...rule,
      serverName: server.name,
    })),
  )
  if (!keyword) return rows
  return rows.filter((rule) =>
    [
      rule.name,
      rule.type,
      rule.serverName,
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
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void Promise.all([loadServers(), loadUser()])
})

async function loadUser() {
  try {
    currentUser.value = await getMe()
  } catch {
    currentUser.value = null
  }
}

function defaultServerForm(): ServerInput {
  return {
    name: '',
    serverAddr: '',
    serverPort: 7000,
    authToken: '',
    transportProtocol: 'tcp',
    configMode: 'toml_reload',
    autoStart: false,
    autoRestart: true,
    maxRestarts: 3,
    adminPort: 0,
    adminUser: '',
    adminPassword: '',
    frpcVersionId: '',
  }
}

function defaultRuleForm(): RuleForm {
  return {
    id: '',
    serverId: servers.value[0]?.id ?? '',
    name: '',
    type: 'tcp',
    localIp: '127.0.0.1',
    localPort: 22,
    remotePort: 6022,
    customDomains: [],
    customDomainsText: '',
    secretKey: '',
    role: 'server',
    serverName: '',
    bindAddr: '127.0.0.1',
    bindPort: 6000,
    useEncryption: false,
    useCompression: false,
    bandwidthLimit: '',
    locations: [],
    locationsText: '',
    hostHeaderRewrite: '',
    httpUser: '',
    httpPassword: '',
    requestHeaders: [],
    requestHeadersText: '',
    enabled: true,
  }
}

function openCreateServer() {
  if (!canOperate.value) return
  editingServerId.value = ''
  serverForm.value = defaultServerForm()
  showExperimentalConfigMode.value = false
  serverDrawerOpen.value = true
}

function openEditServer(server: Server) {
  if (!canOperate.value) return
  editingServerId.value = server.id
  serverForm.value = {
    name: server.name,
    serverAddr: server.serverAddr,
    serverPort: server.serverPort,
    authToken: '',
    transportProtocol: server.transportProtocol || 'tcp',
    configMode: server.configMode || 'toml_reload',
    autoStart: server.autoStart,
    autoRestart: server.autoRestart,
    maxRestarts: server.maxRestarts || 3,
    adminPort: server.adminPort,
    adminUser: server.adminUser || '',
    adminPassword: '',
    frpcVersionId: server.frpcVersionId || '',
  }
  showExperimentalConfigMode.value = server.configMode === 'store_api'
  serverDrawerOpen.value = true
}

async function saveServer() {
  if (!canOperate.value) return
  saving.value = true
  try {
    if (editingServerId.value) {
      await updateServer(editingServerId.value, serverForm.value)
      ElMessage.success('服务器已更新')
    } else {
      await createServer(serverForm.value)
      ElMessage.success('服务器已创建')
    }
    serverDrawerOpen.value = false
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    saving.value = false
  }
}

async function removeServer() {
  if (!canOperate.value) return
  if (!editingServerId.value) return
  await ElMessageBox.confirm('删除该服务器会同时删除其代理规则。', '删除服务器', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  })
  saving.value = true
  try {
    await deleteServer(editingServerId.value)
    ElMessage.success('服务器已删除')
    serverDrawerOpen.value = false
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    saving.value = false
  }
}

function openCreateRule() {
  if (!canOperate.value) return
  if (servers.value.length === 0) {
    ElMessage.warning('请先添加服务器节点')
    return
  }
  editingRuleId.value = ''
  ruleForm.value = defaultRuleForm()
  ruleDrawerOpen.value = true
}

function openEditRule(rule: RuleRow) {
  if (!canOperate.value) return
  editingRuleId.value = rule.id
  ruleForm.value = {
    id: rule.id,
    serverId: rule.serverId,
    name: rule.name,
    type: rule.type,
    localIp: rule.localIp,
    localPort: rule.localPort,
    remotePort: rule.remotePort ?? 0,
    customDomains: rule.customDomains ?? [],
    customDomainsText: (rule.customDomains ?? []).join(', '),
    secretKey: '',
    role: rule.role || 'server',
    serverName: rule.serverName || '',
    bindAddr: rule.bindAddr || '127.0.0.1',
    bindPort: rule.bindPort || 6000,
    useEncryption: rule.useEncryption,
    useCompression: rule.useCompression,
    bandwidthLimit: rule.bandwidthLimit || '',
    locations: rule.locations ?? [],
    locationsText: (rule.locations ?? []).join(', '),
    hostHeaderRewrite: rule.hostHeaderRewrite || '',
    httpUser: rule.httpUser || '',
    httpPassword: '',
    requestHeaders: rule.requestHeaders ?? [],
    requestHeadersText: (rule.requestHeaders ?? []).join('\n'),
    enabled: rule.enabled,
  }
  ruleDrawerOpen.value = true
}

async function saveRule() {
  if (!canOperate.value) return
  const input = ruleInput()
  saving.value = true
  try {
    if (editingRuleId.value) {
      await updateRule(ruleForm.value.serverId, editingRuleId.value, input)
      ElMessage.success('规则已更新')
    } else {
      await createRule(ruleForm.value.serverId, input)
      ElMessage.success('规则已创建')
    }
    ruleDrawerOpen.value = false
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    saving.value = false
  }
}

async function removeRule(rule: RuleRow) {
  if (!canOperate.value) return
  await ElMessageBox.confirm(`删除规则 ${rule.name}？`, '删除代理规则', {
    type: 'warning',
    confirmButtonText: '删除',
    cancelButtonText: '取消',
  })
  try {
    await deleteRule(rule.serverId, rule.id)
    ElMessage.success('规则已删除')
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  }
}

async function runServerAction(server: Server, action: 'start' | 'stop' | 'reload' | 'check') {
  if (!canOperate.value) return
  loading.value = true
  try {
    const result =
      action === 'start'
        ? await startServer(server.id)
        : action === 'stop'
          ? await stopServer(server.id)
          : action === 'reload'
            ? await reloadServer(server.id)
            : await checkServer(server.id)
    ElMessage.success(result.message)
    await loadServers()
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    loading.value = false
  }
}

function ruleInput(): ProxyRuleInput {
  const domains = ruleForm.value.customDomainsText
    .split(/[\s,，]+/)
    .map((item) => item.trim())
    .filter(Boolean)
  const locations = ruleForm.value.locationsText
    .split(/[\s,，]+/)
    .map((item) => item.trim())
    .filter(Boolean)
  const requestHeaders = ruleForm.value.requestHeadersText
    .split(/\n+/)
    .map((item) => item.trim())
    .filter(Boolean)
  return {
    name: ruleForm.value.name,
    type: ruleForm.value.type,
    localIp: ruleForm.value.localIp || '127.0.0.1',
    localPort: Number(ruleForm.value.localPort),
    remotePort: Number(ruleForm.value.remotePort || 0),
    customDomains: domains,
    enabled: ruleForm.value.enabled,
    secretKey: ruleForm.value.secretKey?.trim() || '',
    role: ruleForm.value.role || 'server',
    serverName: ruleForm.value.serverName?.trim() || '',
    bindAddr: ruleForm.value.bindAddr?.trim() || '127.0.0.1',
    bindPort: Number(ruleForm.value.bindPort || 0),
    useEncryption: Boolean(ruleForm.value.useEncryption),
    useCompression: Boolean(ruleForm.value.useCompression),
    bandwidthLimit: ruleForm.value.bandwidthLimit?.trim() || '',
    locations,
    hostHeaderRewrite: ruleForm.value.hostHeaderRewrite?.trim() || '',
    httpUser: ruleForm.value.httpUser?.trim() || '',
    httpPassword: ruleForm.value.httpPassword?.trim() || '',
    requestHeaders,
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

function errorMessage(err: unknown) {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || '操作失败'
  }
  return err instanceof Error ? err.message : '操作失败'
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <ServerTable
      :servers="servers"
      :can-operate="canOperate"
      @add="openCreateServer"
      @edit="openEditServer"
      @start="(server) => runServerAction(server, 'start')"
      @stop="(server) => runServerAction(server, 'stop')"
      @reload="(server) => runServerAction(server, 'reload')"
      @check="(server) => runServerAction(server, 'check')"
    />

    <section class="surface-panel">
      <div class="section-heading">
        <div>
          <p class="overline">Proxy Rules</p>
          <h2>代理规则</h2>
          <span>TCP、UDP、HTTP、HTTPS 四类代理规则</span>
        </div>
        <button v-if="canOperate" class="primary-action" type="button" @click="openCreateRule">
          <Plus :size="15" :stroke-width="1.8" />
          新增规则
        </button>
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
              <th v-if="canOperate">管理</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="rule in allRules" :key="rule.id">
              <td>
                <span class="rule-toggle" :class="{ active: rule.enabled }" />
              </td>
              <td>
                <strong>{{ rule.name }}</strong>
              </td>
              <td>
                <span class="protocol-pill">{{ rule.type.toUpperCase() }}</span>
              </td>
              <td>{{ rule.serverName }}</td>
              <td>
                <code>{{ localTarget(rule) }}</code>
              </td>
              <td>
                <code>{{ remoteTarget(rule) }}</code>
              </td>
              <td v-if="canOperate">
                <div class="row-actions">
                  <button class="icon-button ghost" type="button" aria-label="编辑" @click="openEditRule(rule)">
                    <Pencil :size="15" :stroke-width="1.8" />
                  </button>
                  <button class="icon-button danger" type="button" aria-label="删除" @click="removeRule(rule)">
                    <Trash2 :size="15" :stroke-width="1.8" />
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <Teleport to="body">
      <div v-if="serverDrawerOpen" class="drawer-layer">
        <button class="drawer-backdrop" type="button" aria-label="关闭抽屉" @click="serverDrawerOpen = false" />
        <aside class="rule-drawer">
          <header class="drawer-header">
            <div>
              <p class="overline">Server Config</p>
              <h2>{{ editingServerId ? '编辑服务器' : '添加服务器' }}</h2>
            </div>
            <button class="icon-button ghost" type="button" aria-label="关闭" @click="serverDrawerOpen = false">
              <X :size="16" :stroke-width="1.8" />
            </button>
          </header>

          <div class="drawer-body">
            <section class="form-section">
              <label>
                <span>节点名称</span>
                <input v-model="serverForm.name" placeholder="Home Lab" />
              </label>
              <div class="form-grid">
                <label>
                  <span>FRPS 地址</span>
                  <input v-model="serverForm.serverAddr" placeholder="frp.example.com" />
                </label>
                <label>
                  <span>FRPS 端口</span>
                  <input v-model.number="serverForm.serverPort" type="number" min="1" max="65535" />
                </label>
              </div>
              <label>
                <span>Auth Token</span>
                <input v-model="serverForm.authToken" type="password" placeholder="留空则保留原 token" />
              </label>
              <div class="form-grid">
                <label>
                  <span>传输协议</span>
                  <select v-model="serverForm.transportProtocol">
                    <option value="tcp">TCP</option>
                    <option value="kcp">KCP</option>
                    <option value="quic">QUIC</option>
                    <option value="websocket">WebSocket</option>
                  </select>
                </label>
                <label>
                  <span>配置模式</span>
                  <select v-model="serverForm.configMode">
                    <option value="toml_reload">TOML Reload</option>
                    <option v-if="showStoreModeOption" value="store_api">Store API · 实验</option>
                  </select>
                </label>
              </div>
              <button
                v-if="!showStoreModeOption"
                class="ghost-action strong inline-action"
                type="button"
                @click="showExperimentalConfigMode = true"
              >
                <FlaskConical :size="15" :stroke-width="1.8" />
                显示实验配置模式
              </button>
              <div v-if="serverForm.configMode === 'store_api'" class="security-band compact">
                <FlaskConical :size="17" :stroke-width="1.8" />
                <div>
                  <strong>Store API 为实验功能</strong>
                  <p>依赖 frpc Admin Store API，STCP/XTCP 不支持此模式；个人单机默认建议使用 TOML Reload。</p>
                </div>
              </div>
              <div class="form-grid">
                <label>
                  <span>Admin 端口</span>
                  <input v-model.number="serverForm.adminPort" type="number" min="0" max="65535" placeholder="自动分配" />
                </label>
                <label>
                  <span>Admin 用户</span>
                  <input v-model="serverForm.adminUser" placeholder="frpc-web" />
                </label>
              </div>
              <label>
                <span>Admin 密码</span>
                <input v-model="serverForm.adminPassword" type="password" placeholder="留空则自动生成或保留原密码" />
              </label>
              <div class="form-grid">
                <label>
                  <span>自动启动</span>
                  <select v-model="serverForm.autoStart">
                    <option :value="true">Enabled</option>
                    <option :value="false">Disabled</option>
                  </select>
                </label>
              </div>
              <div class="form-grid">
                <label>
                  <span>崩溃自愈</span>
                  <select v-model="serverForm.autoRestart">
                    <option :value="true">Enabled</option>
                    <option :value="false">Disabled</option>
                  </select>
                </label>
                <label>
                  <span>最大重启次数</span>
                  <input v-model.number="serverForm.maxRestarts" type="number" min="1" max="10" />
                </label>
              </div>
            </section>
          </div>

          <footer class="drawer-footer">
            <button v-if="editingServerId" class="ghost-action strong" type="button" :disabled="saving" @click="removeServer">
              删除
            </button>
            <button class="primary-action wide" type="button" :disabled="saving" @click="saveServer">保存配置</button>
          </footer>
        </aside>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="ruleDrawerOpen" class="drawer-layer">
        <button class="drawer-backdrop" type="button" aria-label="关闭抽屉" @click="ruleDrawerOpen = false" />
        <aside class="rule-drawer">
          <header class="drawer-header">
            <div>
              <p class="overline">Proxy Config</p>
              <h2>{{ editingRuleId ? '编辑代理规则' : '配置代理规则' }}</h2>
            </div>
            <button class="icon-button ghost" type="button" aria-label="关闭" @click="ruleDrawerOpen = false">
              <X :size="16" :stroke-width="1.8" />
            </button>
          </header>

          <div class="drawer-body">
            <section class="form-section">
              <label>
                <span>所属节点</span>
                <select v-model="ruleForm.serverId" :disabled="Boolean(editingRuleId)">
                  <option v-for="server in servers" :key="server.id" :value="server.id">{{ server.name }}</option>
                </select>
              </label>
              <div class="form-grid">
                <label>
                  <span>规则名称</span>
                  <input v-model="ruleForm.name" placeholder="ssh-mac" />
                </label>
                <label>
                  <span>代理协议</span>
                  <select v-model="ruleForm.type">
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                    <option value="http">HTTP</option>
                    <option value="https">HTTPS</option>
                    <option value="stcp">STCP</option>
                    <option value="xtcp">XTCP</option>
                  </select>
                </label>
              </div>
              <div v-if="isSecretRule" class="form-grid">
                <label>
                  <span>STCP/XTCP 角色</span>
                  <select v-model="ruleForm.role">
                    <option value="server">Server</option>
                    <option value="visitor">Visitor</option>
                  </select>
                </label>
                <label>
                  <span>Secret Key</span>
                  <input v-model="ruleForm.secretKey" type="password" placeholder="留空则保留原密钥" />
                </label>
              </div>
            </section>

            <section class="route-map">
              <div v-if="!isVisitorRule" class="route-node">
                <p class="overline">Local</p>
                <div class="form-grid compact">
                  <label>
                    <span>IP Address</span>
                    <input v-model="ruleForm.localIp" />
                  </label>
                  <label>
                    <span>Port</span>
                    <input v-model.number="ruleForm.localPort" type="number" min="1" max="65535" />
                  </label>
                </div>
              </div>
              <div class="route-node remote">
                <p class="overline">{{ isVisitorRule ? 'Visitor' : 'Remote' }}</p>
                <label v-if="isTCPUDP">
                  <span>Remote Port</span>
                  <input v-model.number="ruleForm.remotePort" type="number" min="1" max="65535" />
                </label>
                <label v-else-if="isHTTP">
                  <span>Custom Domains</span>
                  <input v-model="ruleForm.customDomainsText" placeholder="app.example.com, api.example.com" />
                </label>
                <div v-else-if="isVisitorRule" class="form-grid compact">
                  <label>
                    <span>Server Name</span>
                    <input v-model="ruleForm.serverName" placeholder="ssh-secure" />
                  </label>
                  <label>
                    <span>Bind Port</span>
                    <input v-model.number="ruleForm.bindPort" type="number" min="1" max="65535" />
                  </label>
                  <label>
                    <span>Bind Addr</span>
                    <input v-model="ruleForm.bindAddr" placeholder="127.0.0.1" />
                  </label>
                </div>
                <div v-else class="muted-inline">STCP/XTCP server 规则不需要远程端口。</div>
              </div>
              <div class="route-node remote">
                <p class="overline">State</p>
                <label>
                  <span>Enabled</span>
                  <select v-model="ruleForm.enabled">
                    <option :value="true">Enabled</option>
                    <option :value="false">Disabled</option>
                  </select>
                </label>
              </div>
            </section>

            <details class="advanced-panel">
              <summary>
                <SlidersHorizontal :size="15" :stroke-width="1.8" />
                高级选项
              </summary>
              <div class="form-section">
                <div class="form-grid">
                  <label>
                    <span>加密传输</span>
                    <select v-model="ruleForm.useEncryption">
                      <option :value="false">Disabled</option>
                      <option :value="true">Enabled</option>
                    </select>
                  </label>
                  <label>
                    <span>压缩传输</span>
                    <select v-model="ruleForm.useCompression">
                      <option :value="false">Disabled</option>
                      <option :value="true">Enabled</option>
                    </select>
                  </label>
                </div>
                <label>
                  <span>带宽限制</span>
                  <input v-model="ruleForm.bandwidthLimit" placeholder="例如 2MB 或 512KB，留空不限制" />
                </label>
                <template v-if="isHTTP">
                  <label>
                    <span>Locations</span>
                    <input v-model="ruleForm.locationsText" placeholder="/, /api" />
                  </label>
                  <label>
                    <span>Host Header Rewrite</span>
                    <input v-model="ruleForm.hostHeaderRewrite" placeholder="internal.example.local" />
                  </label>
                  <div class="form-grid">
                    <label>
                      <span>Basic Auth 用户</span>
                      <input v-model="ruleForm.httpUser" placeholder="可选" />
                    </label>
                    <label>
                      <span>Basic Auth 密码</span>
                      <input v-model="ruleForm.httpPassword" type="password" placeholder="留空则保留原密码" />
                    </label>
                  </div>
                  <label>
                    <span>请求头设置</span>
                    <textarea
                      v-model="ruleForm.requestHeadersText"
                      rows="4"
                      placeholder="X-Forwarded-Proto: https&#10;X-App-Name: frpc-web"
                    />
                  </label>
                </template>
              </div>
            </details>
          </div>

          <footer class="drawer-footer">
            <button class="primary-action wide" type="button" :disabled="saving" @click="saveRule">保存并同步</button>
            <button class="ghost-action strong" type="button" :disabled="saving" @click="ruleDrawerOpen = false">取消</button>
          </footer>
        </aside>
      </div>
    </Teleport>
  </div>
</template>
