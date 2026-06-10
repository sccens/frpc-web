<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { LineChart, type LineSeriesOption } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
  type GridComponentOption,
  type LegendComponentOption,
  type TooltipComponentOption,
} from 'echarts/components'
import * as echarts from 'echarts/core'
import type { ComposeOption, ECharts } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { ArrowDownToLine, ArrowUpFromLine, TrendingUp, Activity, Server, Network, Route, AlertTriangle } from 'lucide-vue-next'
import { getStats, type Stats } from '../api/client'
import { errorMessage } from '../utils/errors'

type TrafficChartOption = ComposeOption<
  | LineSeriesOption
  | GridComponentOption
  | LegendComponentOption
  | TooltipComponentOption
>

echarts.use([
  LineChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  CanvasRenderer,
])

const loading = ref(false)
const error = ref('')
const stats = ref<Stats | null>(null)
const autoRefresh = ref(false)
const refreshInterval = ref(5)
const chartRef = ref<HTMLDivElement | null>(null)
let chart: ECharts | null = null
let timer: ReturnType<typeof setInterval> | null = null
let resizeObserver: ResizeObserver | null = null
let themeObserver: MutationObserver | null = null
let lastSample: { trafficIn: number; trafficOut: number; sampledAt: number } | null = null

const trafficHistory = ref<{
  timestamps: string[]
  trafficIn: number[]
  trafficOut: number[]
  trafficInRate: number[]
  trafficOutRate: number[]
}>({
  timestamps: [],
  trafficIn: [],
  trafficOut: [],
  trafficInRate: [],
  trafficOutRate: [],
})

const maxHistoryPoints = 48

onMounted(async () => {
  await load()
  await nextTick()
  ensureChart()
})

onUnmounted(() => {
  stopAutoRefresh()
  disposeChart()
})

watch(autoRefresh, (enabled) => {
  if (enabled) {
    startAutoRefresh()
  } else {
    stopAutoRefresh()
  }
})

watch(refreshInterval, () => {
  if (autoRefresh.value) {
    stopAutoRefresh()
    startAutoRefresh()
  }
})

// 后台轮询时不再触发全页 loading 遮罩；inFlight 防止慢响应下的并发请求乱序覆盖数据。
let inFlight = false

async function load(silent = false) {
  if (inFlight) return
  inFlight = true
  if (!silent) loading.value = true
  error.value = ''
  try {
    stats.value = await getStats()
    if (!stats.value.summary.trafficAvailable) {
      disposeChart()
    }
    updateTrafficHistory()
    await nextTick()
    ensureChart()
    updateTrafficChart()
  } catch (err) {
    error.value = errorMessage(err, '加载失败')
  } finally {
    inFlight = false
    if (!silent) loading.value = false
  }
}

function updateTrafficHistory() {
  if (!stats.value?.summary.trafficAvailable) return

  const now = new Date().toLocaleTimeString('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
  const sampledAt = getSampleTimestamp(stats.value.sampledAt)
  const trafficIn = stats.value.summary.totalTrafficIn
  const trafficOut = stats.value.summary.totalTrafficOut
  const elapsedSeconds = lastSample
    ? Math.max(1, (sampledAt - lastSample.sampledAt) / 1000)
    : refreshInterval.value

  trafficHistory.value.timestamps.push(now)
  trafficHistory.value.trafficIn.push(trafficIn)
  trafficHistory.value.trafficOut.push(trafficOut)
  trafficHistory.value.trafficInRate.push(lastSample ? Math.max(0, (trafficIn - lastSample.trafficIn) / elapsedSeconds) : 0)
  trafficHistory.value.trafficOutRate.push(lastSample ? Math.max(0, (trafficOut - lastSample.trafficOut) / elapsedSeconds) : 0)

  lastSample = { trafficIn, trafficOut, sampledAt }
  trimTrafficHistory()
}

function trimTrafficHistory() {
  while (trafficHistory.value.timestamps.length > maxHistoryPoints) {
    trafficHistory.value.timestamps.shift()
    trafficHistory.value.trafficIn.shift()
    trafficHistory.value.trafficOut.shift()
    trafficHistory.value.trafficInRate.shift()
    trafficHistory.value.trafficOutRate.shift()
  }
}

function getSampleTimestamp(sampledAt: string) {
  const parsed = Date.parse(sampledAt)
  return Number.isFinite(parsed) ? parsed : Date.now()
}

function ensureChart() {
  if (!chartRef.value || !stats.value?.summary.trafficAvailable) return
  if (chart) return

  chart = echarts.init(chartRef.value, undefined, { renderer: 'canvas' })
  window.addEventListener('resize', resizeChart)

  if ('ResizeObserver' in window) {
    resizeObserver = new ResizeObserver(resizeChart)
    resizeObserver.observe(chartRef.value)
  }

  themeObserver = new MutationObserver(updateTrafficChart)
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  })
}

function disposeChart() {
  window.removeEventListener('resize', resizeChart)
  resizeObserver?.disconnect()
  resizeObserver = null
  themeObserver?.disconnect()
  themeObserver = null
  chart?.dispose()
  chart = null
}

function resizeChart() {
  chart?.resize()
}

function updateTrafficChart() {
  if (!chart || !stats.value?.summary.trafficAvailable) return

  const textColor = getCssVar('--text', '#18181b')
  const mutedColor = getCssVar('--muted', '#71717a')
  const lineColor = getCssVar('--line', 'rgba(113, 113, 122, 0.22)')
  const blueColor = getCssVar('--blue', '#2563eb')
  const greenColor = getCssVar('--green', '#10b981')

  const option: TrafficChartOption = {
    color: [blueColor, greenColor],
    animationDuration: 320,
    tooltip: {
      trigger: 'axis',
      valueFormatter: (value) => `${formatBytes(Number(value))}/s`,
      backgroundColor: getCssVar('--panel-solid', '#ffffff'),
      borderColor: lineColor,
      textStyle: {
        color: textColor,
      },
      axisPointer: {
        type: 'line',
        lineStyle: {
          color: lineColor,
          type: 'dashed',
        },
      },
    },
    legend: {
      top: 0,
      right: 0,
      itemWidth: 18,
      itemHeight: 8,
      icon: 'roundRect',
      textStyle: {
        color: mutedColor,
        fontSize: 12,
      },
    },
    grid: {
      top: 44,
      right: 18,
      bottom: 28,
      left: 68,
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      data: trafficHistory.value.timestamps,
      axisTick: {
        show: false,
      },
      axisLine: {
        lineStyle: {
          color: lineColor,
        },
      },
      axisLabel: {
        color: mutedColor,
        hideOverlap: true,
      },
    },
    yAxis: {
      type: 'value',
      min: 0,
      axisLabel: {
        color: mutedColor,
        formatter: (value) => `${formatBytesShort(Number(value))}/s`,
      },
      splitLine: {
        lineStyle: {
          color: lineColor,
          type: 'dashed',
        },
      },
    },
    series: [
      {
        name: '入站速率',
        type: 'line',
        smooth: true,
        showSymbol: false,
        sampling: 'lttb',
        lineStyle: {
          width: 3,
        },
        areaStyle: {
          opacity: 0.14,
        },
        emphasis: {
          focus: 'series',
        },
        data: trafficHistory.value.trafficInRate,
      },
      {
        name: '出站速率',
        type: 'line',
        smooth: true,
        showSymbol: false,
        sampling: 'lttb',
        lineStyle: {
          width: 3,
        },
        areaStyle: {
          opacity: 0.12,
        },
        emphasis: {
          focus: 'series',
        },
        data: trafficHistory.value.trafficOutRate,
      },
    ],
  }

  chart.setOption(option, true)
  resizeChart()
}

function getCssVar(name: string, fallback: string) {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

function startAutoRefresh() {
  stopAutoRefresh()
  timer = setInterval(() => {
    void load(true)
  }, refreshInterval.value * 1000)
}

function stopAutoRefresh() {
  if (timer) {
    clearInterval(timer)
    timer = null
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

function formatBytesShort(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0'
  const units = ['B', 'K', 'M', 'G', 'T']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size.toFixed(1)}${units[index]}`
}

const trafficRate = computed(() => {
  const lastIn = trafficHistory.value.trafficInRate.at(-1) ?? 0
  const lastOut = trafficHistory.value.trafficOutRate.at(-1) ?? 0
  return {
    in: lastIn,
    out: lastOut,
  }
})

const hasTrafficChart = computed(() => Boolean(stats.value?.summary.trafficAvailable && trafficHistory.value.timestamps.length > 0))
const totalTraffic = computed(() => {
  if (!stats.value) return 0
  return stats.value.summary.totalTrafficIn + stats.value.summary.totalTrafficOut
})
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <el-alert v-if="error" type="error" :title="error" show-icon />

    <section class="ops-panel">
      <div class="ops-copy">
        <p class="overline">Analytics</p>
        <h1>流量仪表盘</h1>
        <div class="ops-meta">
          <span v-if="autoRefresh"><i class="live-dot" /> 自动刷新</span>
          <span v-else>手动刷新</span>
          <code v-if="stats">{{ stats.sampledAt }}</code>
        </div>
      </div>
      <div class="ops-rail">
        <el-switch v-model="autoRefresh" active-text="自动刷新" />
        <el-select v-model="refreshInterval" :disabled="!autoRefresh" style="width: 100px">
          <el-option :value="3" label="3 秒" />
          <el-option :value="5" label="5 秒" />
          <el-option :value="10" label="10 秒" />
          <el-option :value="30" label="30 秒" />
        </el-select>
        <button class="primary-action" type="button" :disabled="loading" @click="load()">
          <Activity :size="15" :stroke-width="1.8" />
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
    <div class="stat-grid compact-grid" v-if="stats && stats.summary.trafficAvailable">
      <div class="stat-tile">
        <span class="metric-icon blue"><ArrowDownToLine :size="17" :stroke-width="1.7" /></span>
        <p>入站速率</p>
        <strong class="stat-text">{{ formatBytes(trafficRate.in) }}/s</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon success"><ArrowUpFromLine :size="17" :stroke-width="1.7" /></span>
        <p>出站速率</p>
        <strong class="stat-text">{{ formatBytes(trafficRate.out) }}/s</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon"><TrendingUp :size="17" :stroke-width="1.7" /></span>
        <p>累计入站</p>
        <strong class="stat-text">{{ formatBytes(stats.summary.totalTrafficIn) }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon amber"><TrendingUp :size="17" :stroke-width="1.7" /></span>
        <p>累计出站</p>
        <strong class="stat-text">{{ formatBytes(stats.summary.totalTrafficOut) }}</strong>
      </div>
    </div>

    <section class="traffic-dashboard-panel" v-if="hasTrafficChart">
      <div class="section-heading compact">
        <div>
          <p class="overline">Traffic</p>
          <h2>实时流量趋势</h2>
          <span>基于 frpc Admin API 采样差值计算，最近 {{ trafficHistory.timestamps.length }} / {{ maxHistoryPoints }} 个数据点</span>
        </div>
        <div class="chart-sampling">
          <strong>{{ refreshInterval }}s</strong>
          <span>刷新间隔</span>
        </div>
      </div>

      <div class="chart-container">
        <div class="chart-readout">
          <div>
            <span class="readout-line inbound" />
            <p>入站实时速率</p>
            <strong>{{ formatBytes(trafficRate.in) }}/s</strong>
          </div>
          <div>
            <span class="readout-line outbound" />
            <p>出站实时速率</p>
            <strong>{{ formatBytes(trafficRate.out) }}/s</strong>
          </div>
          <div>
            <span class="readout-line total" />
            <p>累计总流量</p>
            <strong>{{ formatBytes(totalTraffic) }}</strong>
          </div>
        </div>

        <div ref="chartRef" class="traffic-chart" aria-label="实时入站和出站流量折线图" />
      </div>
    </section>

    <section v-if="stats && !stats.summary.trafficAvailable" class="security-band compact">
      <Activity :size="18" :stroke-width="1.8" />
      <div>
        <strong>流量数据不可用</strong>
        <p>当前 frpc Admin API 未返回流量数据。请确保服务器正在运行且配置了 Admin API。</p>
      </div>
    </section>

    <section class="surface-panel tight" v-if="stats">
      <div class="section-heading compact">
        <div>
          <p class="overline">Servers</p>
          <h2>服务器详情</h2>
          <span>按每个 frpc 实例聚合运行状态和流量</span>
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
              <th>入站流量</th>
              <th>出站流量</th>
              <th>错误</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="server in stats.servers" :key="server.serverId">
              <td><strong>{{ server.name }}</strong></td>
              <td><span class="protocol-pill">{{ server.status }}</span></td>
              <td><code>:{{ server.adminPort }}</code></td>
              <td>{{ server.onlineProxies }}/{{ server.proxyCount }}</td>
              <td>{{ server.trafficAvailable ? formatBytes(server.trafficIn) : '-' }}</td>
              <td>{{ server.trafficAvailable ? formatBytes(server.trafficOut) : '-' }}</td>
              <td>{{ server.error || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="surface-panel tight" v-if="stats && stats.proxies.length > 0">
      <div class="section-heading compact">
        <div>
          <p class="overline">Proxies</p>
          <h2>代理详情</h2>
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
              <th>入站</th>
              <th>出站</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="proxy in stats.proxies" :key="`${proxy.serverId}-${proxy.name}`">
              <td><strong>{{ proxy.name }}</strong></td>
              <td>{{ proxy.serverName }}</td>
              <td><span class="protocol-pill">{{ proxy.type || '-' }}</span></td>
              <td>{{ proxy.status || '-' }}</td>
              <td><code>{{ proxy.localAddr || '-' }}</code></td>
              <td><code>{{ proxy.remoteAddr || '-' }}</code></td>
              <td>{{ proxy.trafficAvailable ? formatBytes(proxy.trafficIn) : '-' }}</td>
              <td>{{ proxy.trafficAvailable ? formatBytes(proxy.trafficOut) : '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>

<style scoped>
.traffic-dashboard-panel {
  position: relative;
  overflow: hidden;
  padding: 20px;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background:
    radial-gradient(circle at 18% 0%, rgba(37, 99, 235, 0.16), transparent 32%),
    radial-gradient(circle at 88% 8%, rgba(16, 185, 129, 0.14), transparent 30%),
    var(--panel);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.76),
    var(--shadow-soft);
  backdrop-filter: blur(12px);
}

.traffic-dashboard-panel .section-heading {
  align-items: flex-start;
  margin-bottom: 16px;
}

.chart-sampling {
  display: grid;
  justify-items: end;
  gap: 2px;
  flex: 0 0 auto;
  padding: 8px 11px;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: rgba(250, 250, 250, 0.68);
}

.chart-sampling strong {
  color: var(--text);
  font-size: 16px;
}

.chart-sampling span {
  color: var(--muted);
  font-size: 11px;
}

.chart-container {
  display: grid;
  grid-template-columns: minmax(170px, 0.28fr) minmax(0, 1fr);
  gap: 18px;
  min-width: 0;
}

.chart-readout {
  display: grid;
  gap: 10px;
  align-content: start;
}

.chart-readout > div {
  position: relative;
  overflow: hidden;
  padding: 14px;
  border: 1px solid var(--line-soft);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.64);
  box-shadow: var(--shadow-subtle);
}

.chart-readout p {
  margin: 8px 0 4px;
  color: var(--muted);
  font-size: 12px;
}

.chart-readout strong {
  color: var(--text);
  font-size: 18px;
  line-height: 1.2;
  overflow-wrap: anywhere;
}

.readout-line {
  display: block;
  width: 34px;
  height: 4px;
  border-radius: 999px;
  background: var(--blue);
}

.readout-line.outbound {
  background: var(--green);
}

.readout-line.total {
  background: linear-gradient(90deg, var(--blue), var(--green));
}

.traffic-chart {
  min-width: 0;
  width: 100%;
  height: 360px;
  border: 1px solid var(--line-soft);
  border-radius: 20px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.66), rgba(250, 250, 250, 0.48)),
    var(--panel);
}

.compact-grid {
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
}

html[data-theme="dark"] .traffic-dashboard-panel {
  background:
    radial-gradient(circle at 18% 0%, rgba(96, 165, 250, 0.14), transparent 32%),
    radial-gradient(circle at 88% 8%, rgba(52, 211, 153, 0.12), transparent 30%),
    var(--panel);
}

html[data-theme="dark"] .chart-sampling,
html[data-theme="dark"] .chart-readout > div,
html[data-theme="dark"] .traffic-chart {
  background: rgba(24, 24, 27, 0.68);
}

@media (max-width: 720px) {
  .traffic-dashboard-panel {
    padding: 16px;
  }

  .traffic-dashboard-panel .section-heading {
    display: grid;
  }

  .chart-sampling {
    justify-items: start;
  }

  .chart-container {
    grid-template-columns: 1fr;
  }

  .chart-readout {
    grid-template-columns: 1fr;
  }

  .traffic-chart {
    height: 300px;
  }
}
</style>
