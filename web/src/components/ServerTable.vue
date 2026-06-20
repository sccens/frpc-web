<script setup lang="ts">
import {
  Copy,
  Ellipsis,
  Plus,
  Play,
  Power,
  RefreshCw,
  RotateCw,
  Settings,
} from 'lucide-vue-next'
import type { Server } from '../api/client'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  servers: Server[]
  compact?: boolean
}>()

const emit = defineEmits<{
  add: []
  edit: [server: Server]
  start: [server: Server]
  stop: [server: Server]
  restart: [server: Server]
  reload: [server: Server]
  check: [server: Server]
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
</script>

<template>
  <section class="surface-panel">
    <div class="section-heading">
      <div>
        <p class="overline">Server Nodes</p>
        <h2>服务器节点</h2>
        <span>每个节点是一个本机 frpc 进程，连接到对应的 frps；启停操作均作用于本机进程</span>
      </div>
      <button v-if="!compact" class="primary-action" type="button" @click="emit('add')">
        <Plus :size="15" :stroke-width="1.8" />
        添加节点
      </button>
    </div>

    <div class="server-bento-grid" :class="{ 'is-compact': compact }">
      <article v-for="server in servers" :key="server.id" class="server-card">
        <div class="card-spotlight" />

        <div class="server-card-top">
          <div class="server-title-row">
            <StatusBadge :status="server.status" />
            <h3>{{ server.name }}</h3>
            <span v-if="server.managementMode === 'attached'" class="mode-chip attached-chip" title="只读观察模式：面板无法控制此进程的启停">
              👁️ 只读
            </span>
          </div>
          <button v-if="!compact" class="icon-button ghost" type="button" aria-label="配置检查" @click="emit('check', server)">
            <Ellipsis :size="17" :stroke-width="1.8" />
          </button>
        </div>

        <div class="server-meta-box">
          <div class="meta-block wide">
            <span>Remote Host</span>
            <code>{{ server.serverAddr }}:{{ server.serverPort }}</code>
            <Copy
              class="copy-icon"
              :size="14"
              :stroke-width="1.7"
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
            <span>Uptime</span>
            <strong>{{ server.uptime }}</strong>
          </div>
          <div class="meta-block">
            <span>Auto Start</span>
            <strong>{{ server.autoStart ? '已启用' : '已禁用' }}</strong>
          </div>
          <div class="meta-block">
            <span>Self Heal</span>
            <strong>{{ server.autoRestart ? `${server.maxRestarts || 3}x` : '已禁用' }}</strong>
          </div>
          <div class="meta-block">
            <span>Reloaded</span>
            <strong>{{ server.lastReloadAt }}</strong>
          </div>
        </div>

        <div v-if="!compact" class="server-actions">
          <ElTooltip content="启动本机 frpc 进程" placement="top">
            <button
              class="control-button primary"
              type="button"
              :disabled="server.status === 'running' || server.status === 'starting'"
              @click="emit('start', server)"
            >
              <Play :size="15" :stroke-width="1.8" />
              启动
            </button>
          </ElTooltip>
          <ElTooltip content="热重载：frpc 重读配置并向 frps 重新注册代理" placement="top">
            <button
              class="control-button warning"
              type="button"
              :disabled="server.status === 'stopped' || server.restartRequired"
              @click="emit('reload', server)"
            >
              <RefreshCw :size="15" :stroke-width="1.8" />
              重载
            </button>
          </ElTooltip>
          <ElTooltip content="重启本机 frpc 进程（连接地址等公共配置变更后需重启生效）" placement="top">
            <button
              class="icon-button"
              type="button"
              :disabled="server.status === 'stopped'"
              aria-label="重启"
              @click="emit('restart', server)"
            >
              <RotateCw :size="15" :stroke-width="1.8" />
            </button>
          </ElTooltip>
          <ElTooltip content="停止本机 frpc 进程" placement="top">
            <button
              class="icon-button"
              type="button"
              :disabled="server.status === 'stopped'"
              aria-label="停止"
              @click="emit('stop', server)"
            >
              <Power :size="15" :stroke-width="1.8" />
            </button>
          </ElTooltip>
          <ElTooltip content="设置" placement="top">
            <button class="icon-button" type="button" aria-label="设置" @click="emit('edit', server)">
              <Settings :size="15" :stroke-width="1.8" />
            </button>
          </ElTooltip>
        </div>
      </article>
    </div>
  </section>
</template>
