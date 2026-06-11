<script setup lang="ts">
import { onMounted } from 'vue'
import { AlertTriangle, ArrowRight, CheckCircle2, Network, Route } from 'lucide-vue-next'
import ServerTable from '../components/ServerTable.vue'
import { useDashboardStore } from '../stores/dashboard'
import { formatTime } from '../utils/time'

const store = useDashboardStore()

onMounted(() => {
  void store.load()
})
</script>

<template>
  <div class="page-stack animate-enter" v-loading="store.loading">
    <el-alert v-if="store.error" type="error" :title="store.error" show-icon />

    <section class="ops-panel" v-if="store.summary">
      <div class="ops-copy">
        <p class="overline">Overview</p>
        <h1>控制台</h1>
        <div class="ops-meta">
          <span><i class="live-dot" /> API Online</span>
          <code>frpc {{ store.data?.currentFrpc.version ?? '-' }}</code>
          <code>{{ store.summary.proxyRules }} proxies</code>
        </div>
      </div>

      <div class="ops-rail">
        <div>
          <strong>{{ store.summary.runningServers }}/{{ store.summary.totalServers }}</strong>
          <span>Nodes Online</span>
        </div>
        <RouterLink to="/servers" class="primary-action">
          管理节点
          <ArrowRight :size="15" :stroke-width="1.8" />
        </RouterLink>
      </div>
    </section>

    <div class="stat-grid" v-if="store.summary">
      <div class="stat-tile">
        <span class="metric-icon"><Network :size="17" :stroke-width="1.7" /></span>
        <p>服务器</p>
        <strong>{{ store.summary.totalServers }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon success"><CheckCircle2 :size="17" :stroke-width="1.7" /></span>
        <p>运行中</p>
        <strong>{{ store.summary.runningServers }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon blue"><Route :size="17" :stroke-width="1.7" /></span>
        <p>代理规则</p>
        <strong>{{ store.summary.proxyRules }}</strong>
      </div>
      <div class="stat-tile">
        <span class="metric-icon amber"><AlertTriangle :size="17" :stroke-width="1.7" /></span>
        <p>健康事件</p>
        <strong>{{ store.summary.openEvents }}</strong>
      </div>
    </div>

    <div class="dashboard-grid" v-if="store.data">
      <ServerTable :servers="store.servers" compact />

      <div class="side-stack">
        <section class="surface-panel tight">
          <div class="section-heading compact">
            <div>
              <p class="overline">Runtime</p>
              <h2>frpc 版本</h2>
              <span>{{ store.data.currentFrpc.path }}</span>
            </div>
          </div>
          <div class="version-box">
            <strong>{{ store.data.currentFrpc.version }}</strong>
            <span>最新 {{ store.data.currentFrpc.latest }}</span>
          </div>
        </section>

        <section class="surface-panel tight">
          <div class="section-heading compact">
            <div>
              <p class="overline">Signals</p>
              <h2>健康事件</h2>
              <span>最近需要关注的状态变化</span>
            </div>
          </div>
          <div class="event-list">
            <div v-for="event in store.data.health" :key="event.id" class="event-item">
              <span :class="['event-level', `level-${event.level}`]" />
              <div>
                <strong>{{ event.server || '系统' }}</strong>
                <p>{{ event.message }}</p>
              </div>
              <time :title="event.createdAt">{{ formatTime(event.createdAt) }}</time>
            </div>
            <div v-if="store.data.health.length === 0" class="empty-state">暂无健康事件，一切正常</div>
          </div>
        </section>
      </div>
    </div>
  </div>
</template>
