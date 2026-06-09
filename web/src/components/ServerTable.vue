<script setup lang="ts">
import { ElTooltip } from 'element-plus'
import {
  Copy,
  Ellipsis,
  Plus,
  Play,
  Power,
  RefreshCw,
  Settings,
} from 'lucide-vue-next'
import type { Server } from '../api/client'
import StatusBadge from './StatusBadge.vue'

defineProps<{
  servers: Server[]
  compact?: boolean
  canOperate?: boolean
}>()

const emit = defineEmits<{
  add: []
  edit: [server: Server]
  start: [server: Server]
  stop: [server: Server]
  reload: [server: Server]
  check: [server: Server]
}>()
</script>

<template>
  <section class="surface-panel">
    <div class="section-heading">
      <div>
        <p class="overline">Server Nodes</p>
        <h2>服务器节点</h2>
        <span>多实例状态、代理数量和待处理变更</span>
      </div>
      <button v-if="!compact && canOperate" class="primary-action" type="button" @click="emit('add')">
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
          </div>
          <button v-if="!compact && canOperate" class="icon-button ghost" type="button" aria-label="配置检查" @click="emit('check', server)">
            <Ellipsis :size="17" :stroke-width="1.8" />
          </button>
        </div>

        <div class="server-meta-box">
          <div class="meta-block wide">
            <span>Remote Host</span>
            <code>{{ server.serverAddr }}:{{ server.serverPort }}</code>
            <Copy class="copy-icon" :size="14" :stroke-width="1.7" />
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
            <strong>{{ server.autoStart ? 'Enabled' : 'Disabled' }}</strong>
          </div>
          <div class="meta-block">
            <span>Self Heal</span>
            <strong>{{ server.autoRestart ? `${server.maxRestarts || 3}x` : 'Disabled' }}</strong>
          </div>
          <div class="meta-block">
            <span>Reloaded</span>
            <strong>{{ server.lastReloadAt }}</strong>
          </div>
        </div>

        <div v-if="!compact && canOperate" class="server-actions">
          <ElTooltip content="启动" placement="top">
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
          <ElTooltip content="热重载" placement="top">
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
          <ElTooltip content="停止" placement="top">
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
