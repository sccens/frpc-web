<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { AlertTriangle, ArrowDownToLine, ArrowUpFromLine, Gauge, Network, RefreshCw, Route, Server } from 'lucide-vue-next'
import { getStats, type Stats } from '../api/client'

const loading = ref(false)
const error = ref('')
const stats = ref<Stats | null>(null)

const trafficNote = computed(() => {
  if (stats.value?.summary.trafficAvailable) return 'frpc Admin API 已返回流量字段'
  return '当前 frpc Admin API 未返回流量数据'
})

onMounted(() => {
  void load()
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    stats.value = await getStats()
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}

function errorMessage(err: unknown) {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || '读取统计失败'
  }
  return err instanceof Error ? err.message : '读取统计失败'
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <el-alert v-if="error" type="error" :title="error" show-icon />

    <section class="ops-panel" v-if="stats">
      <div class="ops-copy">
        <p class="overline">Telemetry</p>
        <h1>统计</h1>
        <div class="ops-meta">
          <span><i class="live-dot" /> Admin API</span>
          <code>{{ stats.sampledAt || '-' }}</code>
          <code>{{ trafficNote }}</code>
        </div>
      </div>
      <div class="ops-rail">
        <div>
          <strong>{{ stats.summary.onlineProxies }}/{{ stats.summary.proxyRules }}</strong>
          <span>Proxies Online</span>
        </div>
        <button class="primary-action" type="button" :disabled="loading" @click="load">
          <RefreshCw :size="15" :stroke-width="1.8" />
          刷新
        </button>
      </div>
    </section>

    <div class="stat-grid" v-if="stats">
      <div class="stat-tile">
        <span class="metric-icon"><Server :size="17" :stroke-width="1.7" /></span>
        <p>服务器</p>
        <strong>{{ stats.summary.totalServers }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon success"><Network :size="17" :stroke-width="1.7" /></span>
        <p>运行中</p>
        <strong>{{ stats.summary.runningServers }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon blue"><Route :size="17" :stroke-width="1.7" /></span>
        <p>在线代理</p>
        <strong>{{ stats.summary.onlineProxies }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon amber"><AlertTriangle :size="17" :stroke-width="1.7" /></span>
        <p>异常代理</p>
        <strong>{{ stats.summary.errorProxies }}</strong>
      </div>
    </div>

    <div class="stat-grid compact-grid" v-if="stats">
      <div class="stat-tile">
        <span class="metric-icon"><Gauge :size="17" :stroke-width="1.7" /></span>
        <p>流量状态</p>
        <strong class="stat-text">{{ stats.summary.trafficAvailable ? 'Available' : 'Unavailable' }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon blue"><ArrowDownToLine :size="17" :stroke-width="1.7" /></span>
        <p>入站流量</p>
        <strong class="stat-text">{{ formatBytes(stats.summary.totalTrafficIn) }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon success"><ArrowUpFromLine :size="17" :stroke-width="1.7" /></span>
        <p>出站流量</p>
        <strong class="stat-text">{{ formatBytes(stats.summary.totalTrafficOut) }}</strong>
      </div>
    </div>

    <section v-if="stats && !stats.summary.trafficAvailable" class="security-band compact">
      <Gauge :size="18" :stroke-width="1.8" />
      <div>
        <strong>流量数据不可用</strong>
        <p>统计页只展示 frpc Admin API 实际返回的数据；当前响应没有包含可识别的流量字段。</p>
      </div>
    </section>

    <div class="dashboard-grid" v-if="stats">
      <section class="surface-panel tight">
        <div class="section-heading compact">
          <div>
            <p class="overline">Servers</p>
            <h2>服务器维度</h2>
            <span>按每个 frpc 本地 Admin API 聚合运行状态</span>
          </div>
        </div>
        <div class="rule-table-wrap embedded">
          <table class="rule-table stats-table">
            <thead>
              <tr>
                <th>服务器</th>
                <th>状态</th>
                <th>Admin</th>
                <th>代理</th>
                <th>流量</th>
                <th>错误</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="server in stats.servers" :key="server.serverId">
                <td><strong>{{ server.name }}</strong></td>
                <td><span class="protocol-pill">{{ server.status }}</span></td>
                <td><code>127.0.0.1:{{ server.adminPort }}</code></td>
                <td>{{ server.onlineProxies }}/{{ server.proxyCount }}</td>
                <td>{{ server.trafficAvailable ? `${formatBytes(server.trafficIn)} / ${formatBytes(server.trafficOut)}` : '-' }}</td>
                <td>{{ server.error || '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <div class="side-stack">
        <section class="surface-panel tight">
          <div class="section-heading compact">
            <div>
              <p class="overline">Errors</p>
              <h2>最近错误</h2>
              <span>Admin API 连接或代理状态异常</span>
            </div>
          </div>
          <div class="event-list">
            <div v-if="stats.errors.length === 0" class="empty-state">暂无错误</div>
            <div v-for="item in stats.errors" :key="`${item.serverId}-${item.proxyName || item.message}`" class="event-item">
              <span class="event-level level-warning" />
              <div>
                <strong>{{ item.proxyName || item.serverName }}</strong>
                <p>{{ item.message }}</p>
              </div>
              <time>{{ item.serverName }}</time>
            </div>
          </div>
        </section>
      </div>
    </div>

    <section class="surface-panel tight" v-if="stats">
      <div class="section-heading compact">
        <div>
          <p class="overline">Proxies</p>
          <h2>代理维度</h2>
          <span>来自 frpc Admin API 的代理状态明细</span>
        </div>
      </div>
      <div class="rule-table-wrap embedded">
        <table class="rule-table stats-table">
          <thead>
            <tr>
              <th>代理</th>
              <th>服务器</th>
              <th>类型</th>
              <th>状态</th>
              <th>本地</th>
              <th>远端</th>
              <th>流量</th>
              <th>错误</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="stats.proxies.length === 0">
              <td colspan="8">暂无运行中的代理状态</td>
            </tr>
            <tr v-for="proxy in stats.proxies" :key="`${proxy.serverId}-${proxy.name}`">
              <td><strong>{{ proxy.name }}</strong></td>
              <td>{{ proxy.serverName }}</td>
              <td><span class="protocol-pill">{{ proxy.type || '-' }}</span></td>
              <td>{{ proxy.status || '-' }}</td>
              <td><code>{{ proxy.localAddr || '-' }}</code></td>
              <td><code>{{ proxy.remoteAddr || '-' }}</code></td>
              <td>{{ proxy.trafficAvailable ? `${formatBytes(proxy.trafficIn)} / ${formatBytes(proxy.trafficOut)}` : '-' }}</td>
              <td>{{ proxy.error || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
