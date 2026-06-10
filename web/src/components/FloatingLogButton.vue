<script setup lang="ts">
import { ref } from 'vue'
import { ScrollText, X } from 'lucide-vue-next'
import { getServerLogs, getServers } from '../api/client'
import { errorMessage } from '../utils/errors'

const showDialog = ref(false)
const loading = ref(false)
const logContent = ref('')

async function openLogs() {
  showDialog.value = true
  loading.value = true
  try {
    const servers = await getServers()
    const running = servers.find((server) => server.status === 'running') ?? servers[0]
    if (!running) {
      logContent.value = '暂无服务器日志'
      return
    }
    const lines = await getServerLogs(running.id, 500)
    logContent.value = lines
      .map((line) => (line.time ? `[${line.time}] ${line.message}` : line.message))
      .join('\n')
  } catch (err) {
    logContent.value = `加载失败: ${errorMessage(err, '未知错误')}`
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div>
    <button class="floating-log-button" @click="openLogs" aria-label="查看日志">
      <ScrollText :size="20" :stroke-width="2" />
    </button>

    <div v-if="showDialog" class="log-overlay" @click="showDialog = false">
      <div class="log-dialog" @click.stop>
        <div class="log-header">
          <div>
            <h3>系统日志</h3>
            <span>最近 500 行</span>
          </div>
          <button class="icon-button" @click="showDialog = false" aria-label="关闭">
            <X :size="18" :stroke-width="2" />
          </button>
        </div>
        <div class="log-body" v-loading="loading">
          <pre class="log-content">{{ logContent || '暂无日志' }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.floating-log-button {
  position: fixed;
  right: 24px;
  bottom: 24px;
  width: 56px;
  height: 56px;
  border-radius: 50%;
  background:
    radial-gradient(circle at 30% 20%, rgba(255, 255, 255, 0.24), transparent 32%),
    linear-gradient(135deg, #111827 0%, #2563eb 58%, #10b981 100%);
  color: #ffffff;
  border: 1px solid rgba(255, 255, 255, 0.32);
  box-shadow:
    0 18px 36px rgba(15, 23, 42, 0.24),
    inset 0 1px 0 rgba(255, 255, 255, 0.24);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition:
    transform 0.3s ease,
    box-shadow 0.3s ease,
    filter 0.3s ease;
  z-index: 1000;
}

.floating-log-button:hover {
  transform: scale(1.1);
  filter: saturate(1.08);
  box-shadow:
    0 22px 44px rgba(37, 99, 235, 0.28),
    inset 0 1px 0 rgba(255, 255, 255, 0.3);
}

.floating-log-button:focus-visible {
  outline: 3px solid rgba(37, 99, 235, 0.35);
  outline-offset: 3px;
}

.log-overlay {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2000;
  animation: fadeIn 0.2s ease;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.log-dialog {
  width: 90%;
  max-width: 1000px;
  height: 80vh;
  max-height: 700px;
  background: var(--el-bg-color);
  border-radius: 8px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
  display: flex;
  flex-direction: column;
  animation: slideUp 0.3s ease;
}

@keyframes slideUp {
  from {
    transform: translateY(20px);
    opacity: 0;
  }
  to {
    transform: translateY(0);
    opacity: 1;
  }
}

.log-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--el-border-color);
}

.log-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.log-header span {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-left: 8px;
}

.log-body {
  flex: 1;
  overflow: auto;
  padding: 16px;
  background: var(--el-fill-color-light);
}

.log-content {
  font-family: 'SF Mono', 'Monaco', 'Cascadia Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: var(--el-text-color-regular);
  margin: 0;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.icon-button {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: var(--el-text-color-regular);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s;
  flex-shrink: 0;
}

.icon-button:hover {
  background: var(--el-fill-color);
  color: var(--el-text-color-primary);
}

@media (max-width: 720px) {
  .floating-log-button {
    right: 16px;
    bottom: 16px;
    width: 50px;
    height: 50px;
  }

  .log-overlay {
    align-items: flex-end;
    padding: 10px;
  }

  .log-dialog {
    width: 100%;
    height: min(78vh, 640px);
    border-radius: 18px;
  }

  .log-header {
    padding: 14px 16px;
  }

  .log-body {
    padding: 12px;
  }
}
</style>
