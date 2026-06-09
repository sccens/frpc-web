<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { RefreshCw, RotateCw } from 'lucide-vue-next'
import {
  getServerLogs,
  getServers,
  getSettings,
  updateSettings,
  type LogLine,
  type Server,
  type Settings,
} from '../api/client'
import LogViewer from '../components/LogViewer.vue'

const servers = ref<Server[]>([])
const selectedServer = ref('')
const logs = ref<LogLine[]>([])
const loading = ref(false)
const preferenceSaving = ref(false)
const settings = ref<Settings | null>(null)
const autoRefresh = ref(false)
const refreshInterval = ref(5)
let preferenceReady = false
let refreshTimer: ReturnType<typeof window.setInterval> | undefined

const currentServer = computed(() => servers.value.find((server) => server.id === selectedServer.value))

async function loadLogs(serverId: string) {
  if (!serverId) return
  loading.value = true
  try {
    logs.value = await getServerLogs(serverId)
  } finally {
    loading.value = false
  }
}

function syncRefreshTimer() {
  if (refreshTimer) {
    window.clearInterval(refreshTimer)
    refreshTimer = undefined
  }
  if (!autoRefresh.value || !selectedServer.value) return
  refreshTimer = window.setInterval(() => {
    void loadLogs(selectedServer.value)
  }, Math.max(2, refreshInterval.value) * 1000)
}

async function saveRefreshPreference() {
  if (!preferenceReady || !settings.value) return
  preferenceSaving.value = true
  try {
    settings.value = await updateSettings({
      githubProxy: settings.value.githubProxy || '',
      logAutoRefresh: autoRefresh.value,
      logRefreshInterval: Number(refreshInterval.value || 5),
    })
  } catch (err) {
    ElMessage.error(errorMessage(err, '保存日志刷新偏好失败'))
  } finally {
    preferenceSaving.value = false
  }
}

onMounted(async () => {
  const [serverList, appSettings] = await Promise.all([
    getServers(),
    getSettings().catch(() => null),
  ])
  servers.value = serverList
  settings.value = appSettings
  autoRefresh.value = appSettings?.logAutoRefresh ?? false
  refreshInterval.value = appSettings?.logRefreshInterval || 5
  selectedServer.value = servers.value[0]?.id ?? ''
  preferenceReady = true
  syncRefreshTimer()
})

watch(selectedServer, (serverId) => {
  void loadLogs(serverId)
  syncRefreshTimer()
})

watch([autoRefresh, refreshInterval], () => {
  syncRefreshTimer()
  void saveRefreshPreference()
})

onBeforeUnmount(() => {
  if (refreshTimer) {
    window.clearInterval(refreshTimer)
  }
})

function errorMessage(err: unknown, fallback = '操作失败') {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || fallback
  }
  return err instanceof Error ? err.message : fallback
}
</script>

<template>
  <div class="page-stack animate-enter">
    <section class="surface-panel terminal-panel">
      <div class="section-heading">
        <div>
          <p class="overline">Live Logs</p>
          <h2>实时日志</h2>
          <span>{{ currentServer?.name ?? '选择服务器' }}</span>
        </div>
        <div class="toolbar clean">
          <select v-model="selectedServer" class="native-select">
            <option v-for="server in servers" :key="server.id" :value="server.id">{{ server.name }}</option>
          </select>
          <label class="refresh-toggle">
            <input v-model="autoRefresh" type="checkbox" />
            <RotateCw :size="15" :stroke-width="1.7" />
            自动刷新
          </label>
          <select v-model.number="refreshInterval" class="native-select compact">
            <option :value="2">2 秒</option>
            <option :value="5">5 秒</option>
            <option :value="10">10 秒</option>
            <option :value="30">30 秒</option>
            <option :value="60">60 秒</option>
          </select>
          <span v-if="preferenceSaving" class="toolbar-note">保存中</span>
          <button class="ghost-action strong" type="button" :disabled="loading" @click="loadLogs(selectedServer)">
            <RefreshCw :size="15" :stroke-width="1.7" />
            刷新
          </button>
        </div>
      </div>

      <LogViewer :lines="logs" />
    </section>
  </div>
</template>
