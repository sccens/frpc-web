<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Activity, Edit3, Plus, RefreshCw, Save, Trash2, X } from 'lucide-vue-next'
import {
  createFrpsTarget,
  deleteFrpsTarget,
  getFrpsMetrics,
  updateFrpsTarget,
  type FrpsMetricsOverview,
  type FrpsProxyMetric,
  type FrpsTargetMetrics,
  type FrpsTargetView,
} from '../api/client'
import { errorMessage } from '../utils/errors'
import { formatTime } from '../utils/time'

const POLL_INTERVAL = 3000

const loading = ref(false)
const saving = ref(false)
const deletingId = ref('')
const overview = ref<FrpsMetricsOverview | null>(null)
const selectedTargetId = ref('')
let timer: number | undefined

const formOpen = ref(false)
const editingTarget = ref<FrpsTargetView | null>(null)
const form = reactive({
  name: '',
  url: '',
  username: '',
  password: '',
  enabled: true,
  intervalSeconds: 5,
})

onMounted(() => {
  void loadMetrics()
  timer = window.setInterval(() => void loadMetrics(true), POLL_INTERVAL)
})

onBeforeUnmount(() => {
  if (timer !== undefined) {
    window.clearInterval(timer)
  }
})

const targets = computed(() => overview.value?.targets ?? [])
const totals = computed(() => overview.value?.totals)
const selectedTarget = computed(() => {
  if (!selectedTargetId.value) return targets.value[0] || null
  return targets.value.find((target) => target.target.id === selectedTargetId.value) || targets.value[0] || null
})

const proxyRows = computed(() => {
  const rows: Array<FrpsProxyMetric & { targetName: string; targetId: string }> = []
  targets.value.forEach((target) => {
    ;(target.proxies ?? []).forEach((proxy) => {
      rows.push({
        ...proxy,
        targetName: target.target.name,
        targetId: target.target.id,
      })
    })
  })
  return rows
    .sort((a, b) => (b.trafficInRate + b.trafficOutRate) - (a.trafficInRate + a.trafficOutRate))
    .slice(0, 12)
})

const selectedHistory = computed(() => selectedTarget.value?.history ?? [])
const chartInPoints = computed(() => chartPoints(selectedHistory.value, 'trafficInRate'))
const chartOutPoints = computed(() => chartPoints(selectedHistory.value, 'trafficOutRate'))
const chartMaxLabel = computed(() => {
  const history = selectedHistory.value
  const max = Math.max(0, ...history.map((point) => Math.max(point.trafficInRate, point.trafficOutRate)))
  return formatRate(max)
})

async function loadMetrics(silent = false) {
  if (!silent) loading.value = true
  try {
    overview.value = await getFrpsMetrics()
    if (!selectedTargetId.value && targets.value.length > 0) {
      selectedTargetId.value = targets.value[0].target.id
    }
  } catch (err) {
    if (!silent) ElMessage.error(errorMessage(err, '加载 frps 流量失败'))
  } finally {
    if (!silent) loading.value = false
  }
}

function openCreate() {
  editingTarget.value = null
  Object.assign(form, {
    name: '',
    url: '',
    username: '',
    password: '',
    enabled: true,
    intervalSeconds: 5,
  })
  formOpen.value = true
}

function openEdit(target: FrpsTargetView) {
  editingTarget.value = target
  Object.assign(form, {
    name: target.name,
    url: target.url,
    username: target.username || '',
    password: '',
    enabled: target.enabled,
    intervalSeconds: target.intervalSeconds || 5,
  })
  formOpen.value = true
}

async function submitTarget() {
  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      url: form.url.trim(),
      username: form.username.trim(),
      password: form.password.trim(),
      enabled: form.enabled,
      intervalSeconds: Number(form.intervalSeconds) || 5,
    }
    if (editingTarget.value) {
      await updateFrpsTarget(editingTarget.value.id, payload)
      ElMessage.success('frps 目标已更新')
    } else {
      const created = await createFrpsTarget(payload)
      selectedTargetId.value = created.id
      ElMessage.success('frps 目标已添加')
    }
    formOpen.value = false
    await loadMetrics(true)
  } catch (err) {
    ElMessage.error(errorMessage(err, '保存 frps 目标失败'))
  } finally {
    saving.value = false
  }
}

async function removeTarget(target: FrpsTargetView) {
  try {
    await ElMessageBox.confirm(`删除 ${target.name}？`, '删除 frps 目标', {
      type: 'warning',
      confirmButtonText: '删除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  deletingId.value = target.id
  try {
    await deleteFrpsTarget(target.id)
    if (selectedTargetId.value === target.id) {
      selectedTargetId.value = ''
    }
    ElMessage.success('frps 目标已删除')
    await loadMetrics(true)
  } catch (err) {
    ElMessage.error(errorMessage(err, '删除 frps 目标失败'))
  } finally {
    deletingId.value = ''
  }
}

function statusText(status: FrpsTargetView['status']) {
  switch (status) {
    case 'online':
      return '在线'
    case 'offline':
      return '离线'
    case 'disabled':
      return '停用'
    default:
      return '等待'
  }
}

function statusClass(status: FrpsTargetView['status']) {
  return {
    'is-running': status === 'online',
    'is-error': status === 'offline',
    'is-warning': status === 'pending',
    'is-stopped': status === 'disabled',
  }
}

function formatRate(value: number) {
  return `${formatBytes(value)}/s`
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  const digits = size >= 100 || unit === 0 ? 0 : size >= 10 ? 1 : 2
  return `${size.toFixed(digits)} ${units[unit]}`
}

function formatCount(value: number | undefined) {
  return new Intl.NumberFormat().format(value || 0)
}

function chartPoints(history: FrpsTargetMetrics['history'], key: 'trafficInRate' | 'trafficOutRate') {
  if (history.length < 2) return ''
  const width = 620
  const height = 150
  const pad = 10
  const max = Math.max(1, ...history.map((point) => Math.max(point.trafficInRate, point.trafficOutRate)))
  return history
    .map((point, index) => {
      const x = pad + (index / (history.length - 1)) * (width - pad * 2)
      const y = height - pad - (point[key] / max) * (height - pad * 2)
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <section class="traffic-hero">
      <div class="traffic-hero-copy">
        <p class="overline">FRPS Traffic</p>
        <h1>流量监控</h1>
        <div class="ops-meta">
          <span><span class="live-dot" />{{ formatCount(totals?.onlineCount) }} 在线</span>
          <code>{{ formatCount(totals?.targetCount) }} frps</code>
          <code>{{ formatCount(totals?.proxyCount) }} proxies</code>
        </div>
      </div>
      <div class="traffic-actions">
        <button class="ghost-action strong" type="button" :disabled="loading" @click="loadMetrics()">
          <RefreshCw :size="15" :stroke-width="1.8" />
          刷新
        </button>
        <button class="primary-action" type="button" @click="openCreate">
          <Plus :size="15" :stroke-width="1.8" />
          添加 frps
        </button>
      </div>
    </section>

    <section class="stat-grid traffic-stat-grid">
      <article class="stat-tile">
        <span class="metric-icon success"><Activity :size="16" :stroke-width="1.8" /></span>
        <p>实时入站</p>
        <strong>{{ formatRate(totals?.trafficInRate || 0) }}</strong>
      </article>
      <article class="stat-tile">
        <span class="metric-icon blue"><Activity :size="16" :stroke-width="1.8" /></span>
        <p>实时出站</p>
        <strong>{{ formatRate(totals?.trafficOutRate || 0) }}</strong>
      </article>
      <article class="stat-tile">
        <span class="metric-icon"><Activity :size="16" :stroke-width="1.8" /></span>
        <p>累计入站</p>
        <strong>{{ formatBytes(totals?.trafficIn || 0) }}</strong>
      </article>
      <article class="stat-tile">
        <span class="metric-icon amber"><Activity :size="16" :stroke-width="1.8" /></span>
        <p>连接数</p>
        <strong>{{ formatCount(totals?.connectionCount) }}</strong>
      </article>
    </section>

    <section class="traffic-layout">
      <div class="traffic-main">
        <div class="section-heading">
          <div>
            <p class="overline">Realtime</p>
            <h2>{{ selectedTarget?.target.name || '实时曲线' }}</h2>
            <span>{{ selectedTarget?.target.url || '暂无 frps 目标' }}</span>
          </div>
          <select v-model="selectedTargetId" class="native-select">
            <option v-for="target in targets" :key="target.target.id" :value="target.target.id">
              {{ target.target.name }}
            </option>
          </select>
        </div>

        <div class="traffic-chart-panel">
          <div class="traffic-chart-head">
            <span>峰值 {{ chartMaxLabel }}</span>
            <div>
              <i class="legend in" />入站
              <i class="legend out" />出站
            </div>
          </div>
          <svg class="traffic-chart" viewBox="0 0 620 150" role="img" aria-label="frps traffic chart">
            <polyline v-if="chartInPoints" :points="chartInPoints" class="chart-line chart-in" />
            <polyline v-if="chartOutPoints" :points="chartOutPoints" class="chart-line chart-out" />
          </svg>
          <div v-if="!chartInPoints && !chartOutPoints" class="empty-state compact">等待采样数据</div>
        </div>

        <div class="rule-table-wrap">
          <table class="rule-table">
            <thead>
              <tr>
                <th>Proxy</th>
                <th>frps</th>
                <th>类型</th>
                <th>连接</th>
                <th>入站</th>
                <th>出站</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="proxy in proxyRows" :key="`${proxy.targetId}-${proxy.type}-${proxy.name}`">
                <td><strong>{{ proxy.name || '-' }}</strong></td>
                <td>{{ proxy.targetName }}</td>
                <td><span class="protocol-pill">{{ proxy.type || '-' }}</span></td>
                <td>{{ formatCount(proxy.connectionCount) }}</td>
                <td><code>{{ formatRate(proxy.trafficInRate) }}</code></td>
                <td><code>{{ formatRate(proxy.trafficOutRate) }}</code></td>
              </tr>
            </tbody>
          </table>
          <div v-if="proxyRows.length === 0" class="empty-state">暂无 proxy 流量</div>
        </div>
      </div>

      <aside class="traffic-side">
        <article v-for="item in targets" :key="item.target.id" class="frps-target-card" :class="{ active: item.target.id === selectedTarget?.target.id }">
          <button class="frps-target-main" type="button" @click="selectedTargetId = item.target.id">
            <span class="status-badge" :class="statusClass(item.target.status)">
              <span class="status-dot" />{{ statusText(item.target.status) }}
            </span>
            <strong>{{ item.target.name }}</strong>
            <code>{{ item.target.url }}</code>
            <small v-if="item.target.lastScrapedAt">{{ formatTime(item.target.lastScrapedAt) }}</small>
            <small v-else-if="item.target.lastError">{{ item.target.lastError }}</small>
          </button>
          <div class="frps-target-actions">
            <button class="icon-button" type="button" aria-label="编辑" @click="openEdit(item.target)">
              <Edit3 :size="15" :stroke-width="1.8" />
            </button>
            <button class="icon-button danger" type="button" aria-label="删除" :disabled="deletingId === item.target.id" @click="removeTarget(item.target)">
              <Trash2 :size="15" :stroke-width="1.8" />
            </button>
          </div>
        </article>
        <div v-if="targets.length === 0" class="empty-state">暂无 frps 目标</div>
      </aside>
    </section>

    <el-dialog
      v-model="formOpen"
      :title="editingTarget ? '编辑 frps' : '添加 frps'"
      width="min(520px, calc(100vw - 32px))"
      class="frps-target-dialog"
      modal-class="frps-target-modal"
      body-class="frps-target-dialog-body"
      footer-class="frps-target-dialog-footer"
      append-to-body
      align-center
      center
    >
      <div class="target-form">
        <label>
          <span>名称</span>
          <input v-model="form.name" type="text" placeholder="香港 frps" />
        </label>
        <label>
          <span>Dashboard 地址</span>
          <input v-model="form.url" type="url" placeholder="http://127.0.0.1:7500" />
        </label>
        <label>
          <span>用户名</span>
          <input v-model="form.username" type="text" placeholder="admin" autocomplete="username" />
        </label>
        <label>
          <span>密码</span>
          <input v-model="form.password" type="password" :placeholder="editingTarget?.hasPassword ? '留空保持不变' : 'password'" autocomplete="current-password" />
        </label>
        <div class="target-form-row">
          <label>
            <span>轮询秒数</span>
            <input v-model.number="form.intervalSeconds" type="number" min="2" max="300" />
          </label>
          <label class="switch-row">
            <input v-model="form.enabled" type="checkbox" />
            <span>启用</span>
          </label>
        </div>
      </div>
      <template #footer>
        <div class="target-dialog-footer">
          <button class="ghost-action strong" type="button" @click="formOpen = false">
            <X :size="15" :stroke-width="1.8" />
            取消
          </button>
          <button class="primary-action" type="button" :disabled="saving" @click="submitTarget">
            <Save :size="15" :stroke-width="1.8" />
            保存
          </button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.traffic-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 18px;
  align-items: center;
  padding: 26px;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background:
    radial-gradient(circle at 92% 0%, rgba(37, 99, 235, 0.12), transparent 34%),
    rgba(255, 255, 255, 0.76);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.82),
    var(--shadow-soft);
}

.traffic-hero h1 {
  margin: 0;
  font-size: 34px;
  line-height: 1.08;
  letter-spacing: 0;
}

.traffic-actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: flex-end;
}

.traffic-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 330px;
  gap: 22px;
  align-items: start;
}

.traffic-main,
.traffic-side {
  min-width: 0;
}

.traffic-chart-panel {
  position: relative;
  margin-bottom: 18px;
  padding: 18px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.78);
  box-shadow: var(--shadow-subtle);
}

.traffic-chart-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  color: var(--muted);
  font-size: 12px;
}

.traffic-chart-head div {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.legend {
  width: 18px;
  height: 3px;
  border-radius: 999px;
}

.legend.in {
  background: var(--green);
}

.legend.out {
  background: var(--blue);
}

.traffic-chart {
  display: block;
  width: 100%;
  min-height: 180px;
  border-radius: 12px;
  background:
    linear-gradient(to right, rgba(228, 228, 231, 0.52) 1px, transparent 1px),
    linear-gradient(to bottom, rgba(228, 228, 231, 0.52) 1px, transparent 1px);
  background-size: 64px 36px;
}

.chart-line {
  fill: none;
  stroke-width: 4;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.chart-in {
  stroke: var(--green);
}

.chart-out {
  stroke: var(--blue);
}

.traffic-side {
  display: grid;
  gap: 12px;
}

.frps-target-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  background: rgba(255, 255, 255, 0.78);
  box-shadow: var(--shadow-subtle);
}

.frps-target-card.active {
  border-color: rgba(37, 99, 235, 0.42);
}

.frps-target-main {
  min-width: 0;
  padding: 0;
  border: 0;
  background: transparent;
  text-align: left;
}

.frps-target-main strong,
.frps-target-main code,
.frps-target-main small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.frps-target-main strong {
  margin-top: 8px;
  font-size: 15px;
}

.frps-target-main code {
  margin-top: 6px;
  color: var(--muted);
  font-size: 12px;
}

.frps-target-main small {
  margin-top: 6px;
  color: var(--faint);
  font-size: 12px;
}

.frps-target-actions {
  display: grid;
  gap: 8px;
}

.target-form {
  display: grid;
  gap: 14px;
}

:global(.frps-target-dialog) {
  display: flex;
  flex-direction: column;
  max-height: min(640px, calc(100vh - 96px));
  max-height: min(640px, calc(100dvh - 96px));
  margin: 0;
  border-radius: 16px;
}

:global(.frps-target-modal) {
  overflow: hidden;
}

:global(.frps-target-modal .el-overlay-dialog) {
  box-sizing: border-box;
  padding: 48px 16px;
  overflow: hidden;
}

:global(.frps-target-dialog .el-dialog__header) {
  flex-shrink: 0;
}

:global(.frps-target-dialog-body) {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  padding-top: 8px;
}

:global(.frps-target-dialog-footer) {
  flex-shrink: 0;
  border-top: 1px solid var(--line);
  background: var(--surface);
}

.target-dialog-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  width: 100%;
}

.target-form label {
  display: grid;
  gap: 7px;
}

.target-form span {
  color: var(--muted);
  font-size: 12px;
  font-weight: 680;
}

.target-form input {
  width: 100%;
  min-height: 38px;
  padding: 0 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  outline: 0;
  background: rgba(255, 255, 255, 0.86);
  color: var(--text);
}

.target-form-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 120px;
  gap: 12px;
  align-items: end;
}

.switch-row {
  display: inline-flex !important;
  grid-auto-flow: column;
  align-items: center;
  justify-content: center;
  min-height: 38px;
  padding: 0 12px;
  border: 1px solid var(--line);
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.72);
}

.switch-row input {
  width: auto;
  min-height: auto;
}

.empty-state.compact {
  padding: 18px;
}

html[data-theme="dark"] .traffic-hero,
html[data-theme="dark"] .traffic-chart-panel,
html[data-theme="dark"] .frps-target-card,
html[data-theme="dark"] .target-form input,
html[data-theme="dark"] .switch-row {
  background: rgba(24, 24, 27, 0.74);
}

@media (max-width: 920px) {
  .traffic-layout,
  .traffic-hero {
    grid-template-columns: 1fr;
  }

  .traffic-actions {
    justify-content: flex-start;
  }
}

@media (max-width: 720px) {
  .traffic-stat-grid,
  .target-form-row {
    grid-template-columns: 1fr;
  }

  .traffic-actions,
  .traffic-actions .primary-action,
  .traffic-actions .ghost-action {
    width: 100%;
  }

  :global(.frps-target-dialog) {
    max-height: calc(100vh - 32px);
    max-height: calc(100dvh - 32px);
  }

  :global(.frps-target-modal .el-overlay-dialog) {
    padding: 16px;
  }

  .target-dialog-footer {
    justify-content: stretch;
  }

  .target-dialog-footer .primary-action,
  .target-dialog-footer .ghost-action {
    flex: 1 1 0;
    min-width: 0;
  }
}
</style>
