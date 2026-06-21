<script setup lang="ts">
import { Copy, Pencil, RefreshCw, ScrollText } from 'lucide-vue-next'
import type { Server } from '../api/client'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  servers: Server[]
}>()

const emit = defineEmits<{
  editConfig: [server: Server]
  reload: [server: Server]
  logs: [server: Server]
}>()

async function copyEndpoint(server: Server) {
  const endpoint = `${server.serverAddr}:${server.serverPort}`
  try {
    await navigator.clipboard.writeText(endpoint)
    ElMessage.success(`已复制 ${endpoint}`)
  } catch {
    ElMessage.error('复制失败')
  }
}

function adminLabel(server: Server) {
  if (!server.adminPort) return '未配置'
  return `${server.adminAddr || '127.0.0.1'}:${server.adminPort}`
}
</script>

<template>
  <section class="surface-panel">
    <div class="section-heading">
      <div>
        <p class="overline">Frpc Instances</p>
        <h2>frpc 实例</h2>
        <span>从磁盘扫描到的 frpc 配置文件；进程由 systemd 管理，面板只读监控</span>
      </div>
    </div>

    <div class="server-bento-grid">
      <article v-for="server in servers" :key="server.id" class="server-card">
        <div class="card-spotlight" />

        <div class="server-card-top">
          <div class="server-title-row">
            <StatusBadge :status="server.status" />
            <h3 :title="server.name">{{ server.name }}</h3>
            <span class="mode-chip attached-chip" title="外部进程，面板只读监控">只读</span>
          </div>
        </div>

        <div class="server-meta-box">
          <div class="meta-block wide">
            <span>Remote Host</span>
            <code>{{ server.serverAddr }}:{{ server.serverPort }}</code>
            <Copy
              class="copy-icon"
              :size="14"
              role="button"
              aria-label="复制地址"
              style="cursor: pointer"
              @click="copyEndpoint(server)"
            />
          </div>
          <div class="meta-block">
            <span>Rules</span>
            <strong>{{ server.proxyCount }} 条</strong>
          </div>
          <div class="meta-block">
            <span>Admin API</span>
            <strong>{{ adminLabel(server) }}</strong>
          </div>
          <div class="meta-block wide">
            <span>Config Path</span>
            <code class="version-path" :title="server.configPath">{{ server.configPath }}</code>
          </div>
        </div>

        <div class="server-actions">
          <ElTooltip content="编辑此配置文件原文（保存后需热重载或重启生效）" placement="top">
            <button class="control-button primary" type="button" @click="emit('editConfig', server)">
              <Pencil :size="15" :stroke-width="1.8" />
              编辑配置
            </button>
          </ElTooltip>
          <ElTooltip content="通过 frpc admin API 热重载（frpc 重读其启动时的配置文件）" placement="top">
            <button
              class="control-button warning"
              type="button"
              :disabled="!server.adminPort"
              @click="emit('reload', server)"
            >
              <RefreshCw :size="15" :stroke-width="1.8" />
              热重载
            </button>
          </ElTooltip>
          <ElTooltip content="查看 frpc 日志（来自配置的 log.to）" placement="top">
            <button
              class="icon-button"
              type="button"
              :disabled="!server.logPath"
              aria-label="日志"
              @click="emit('logs', server)"
            >
              <ScrollText :size="15" :stroke-width="1.8" />
            </button>
          </ElTooltip>
        </div>
      </article>

      <div v-if="servers.length === 0" class="empty-state">
        未扫描到任何 frpc 配置文件。请把 frpc.toml 放到扫描路径（/etc/frpc、/usr/local/etc/frpc 或数据目录），或用环境变量 FRPC_WEB_CONFIG_PATH 指定。
      </div>
    </div>
  </section>
</template>
