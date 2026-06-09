<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Filter, RefreshCw, Search, ShieldAlert } from 'lucide-vue-next'
import { getAuditLogs, type AuditLog, type AuditLogPage } from '../api/client'

const loading = ref(false)
const page = ref<AuditLogPage>({ items: [], total: 0, page: 1, pageSize: 50 })
const user = ref('')
const action = ref('')
const result = ref('')

const totalPages = computed(() => Math.max(1, Math.ceil(page.value.total / page.value.pageSize)))
const currentRange = computed(() => {
  if (page.value.total === 0) return '0 / 0'
  const start = (page.value.page - 1) * page.value.pageSize + 1
  const end = Math.min(page.value.page * page.value.pageSize, page.value.total)
  return `${start}-${end} / ${page.value.total}`
})

onMounted(() => {
  void loadAuditLogs(1)
})

async function loadAuditLogs(nextPage = page.value.page) {
  loading.value = true
  try {
    page.value = await getAuditLogs({
      page: nextPage,
      pageSize: page.value.pageSize,
      user: user.value.trim(),
      action: action.value,
      result: result.value,
    })
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    loading.value = false
  }
}

function resetFilters() {
  user.value = ''
  action.value = ''
  result.value = ''
  void loadAuditLogs(1)
}

function actionLabel(value: string) {
  const labels: Record<string, string> = {
    'auth.bootstrap': '初始化',
    'auth.login': '登录',
    'auth.logout': '登出',
    'auth.access_key': '修改密钥',
    'auth.session_revoke': '撤销会话',
    'auth.session_revoke_others': '撤销其它会话',
    'settings.update': '更新设置',
    'config.export': '导出配置',
    'config.import': '导入配置',
    'servers.create': '创建服务器',
    'servers.update': '更新服务器',
    'servers.delete': '删除服务器',
    'servers.start': '启动',
    'servers.stop': '停止',
    'servers.restart': '重启',
    'servers.reload': '热重载',
    'servers.check': '配置检查',
    'rules.create': '创建规则',
    'rules.update': '更新规则',
    'rules.delete': '删除规则',
    'frpc.activate': '切换版本',
    'frpc.check_latest': '检查版本',
    'frpc.install_online': '在线安装',
    'frpc.install_offline': '离线安装',
  }
  return labels[value] || value
}

function actor(log: AuditLog) {
  return log.username || log.userId || '-'
}

function target(log: AuditLog) {
  if (!log.resourceType && !log.resourceId) return '-'
  return [log.resourceType, log.resourceId].filter(Boolean).join(':')
}

function errorMessage(err: unknown) {
  if (typeof err === 'object' && err !== null && 'response' in err) {
    const response = (err as { response?: { data?: { error?: string; message?: string } } }).response
    return response?.data?.error || response?.data?.message || '加载失败'
  }
  return err instanceof Error ? err.message : '加载失败'
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <section class="surface-panel">
      <div class="section-heading">
        <div>
          <p class="overline">Audit Trail</p>
          <h2>审计日志</h2>
          <span>登录、会话、安全设置、配置变更、进程操作和版本安装记录</span>
        </div>
        <button class="ghost-action strong" type="button" :disabled="loading" @click="loadAuditLogs()">
          <RefreshCw :size="15" :stroke-width="1.8" />
          刷新
        </button>
      </div>

      <div class="security-band compact">
        <ShieldAlert :size="17" :stroke-width="1.8" />
        <div>
          <strong>审计日志只追加写入</strong>
          <p>不会记录明文 token、密码、JWT 或上传文件内容。</p>
        </div>
      </div>
    </section>

    <section class="surface-panel">
      <div class="rule-toolbar">
        <label class="search-box">
          <Search class="field-icon" :size="15" :stroke-width="1.7" />
          <input v-model="user" type="search" placeholder="按 owner 或用户 ID 筛选..." @keyup.enter="loadAuditLogs(1)" />
        </label>
        <select v-model="action" class="native-select compact" @change="loadAuditLogs(1)">
          <option value="">全部动作</option>
          <option value="auth.login">登录</option>
          <option value="auth.access_key">修改密钥</option>
          <option value="auth.session_revoke">撤销会话</option>
          <option value="settings.update">更新设置</option>
          <option value="config.export">导出配置</option>
          <option value="config.import">导入配置</option>
          <option value="servers.start">启动</option>
          <option value="servers.reload">热重载</option>
          <option value="rules.update">更新规则</option>
          <option value="frpc.install_online">在线安装</option>
          <option value="frpc.install_offline">离线安装</option>
        </select>
        <select v-model="result" class="native-select compact" @change="loadAuditLogs(1)">
          <option value="">全部结果</option>
          <option value="success">成功</option>
          <option value="failure">失败</option>
        </select>
        <button class="ghost-action strong" type="button" @click="resetFilters">
          <Filter :size="15" :stroke-width="1.8" />
          重置
        </button>
      </div>

      <div class="rule-table-wrap">
        <table class="rule-table audit-table">
          <thead>
            <tr>
              <th>结果</th>
              <th>动作</th>
              <th>用户</th>
              <th>对象</th>
              <th>来源 IP</th>
              <th>时间</th>
              <th>错误</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="log in page.items" :key="log.id">
              <td>
                <span class="status-badge" :class="log.result === 'success' ? 'is-running' : 'is-error'">
                  <span class="status-dot" />
                  {{ log.result === 'success' ? 'Success' : 'Failure' }}
                </span>
              </td>
              <td>
                <strong>{{ actionLabel(log.action) }}</strong>
                <code>{{ log.action }}</code>
              </td>
              <td>
                <strong>{{ actor(log) }}</strong>
                <span class="muted-cell">{{ log.role || '-' }}</span>
              </td>
              <td><code>{{ target(log) }}</code></td>
              <td><code>{{ log.ip || '-' }}</code></td>
              <td>{{ log.createdAt }}</td>
              <td>{{ log.error || '-' }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="pager-row">
        <span>{{ currentRange }}</span>
        <div class="row-actions">
          <button class="ghost-action strong" type="button" :disabled="page.page <= 1" @click="loadAuditLogs(page.page - 1)">上一页</button>
          <button class="ghost-action strong" type="button" :disabled="page.page >= totalPages" @click="loadAuditLogs(page.page + 1)">下一页</button>
        </div>
      </div>
    </section>
  </div>
</template>
