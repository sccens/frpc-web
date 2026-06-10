import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { getDashboard, type Dashboard } from '../api/client'
import { errorMessage } from '../utils/errors'

export const useDashboardStore = defineStore('dashboard', () => {
  const data = ref<Dashboard | null>(null)
  const loading = ref(false)
  const error = ref('')

  const servers = computed(() => data.value?.servers ?? [])
  const summary = computed(() => data.value?.summary)

  async function load() {
    loading.value = true
    error.value = ''
    try {
      data.value = await getDashboard()
    } catch (err) {
      error.value = errorMessage(err, '加载数据失败')
    } finally {
      loading.value = false
    }
  }

  return {
    data,
    loading,
    error,
    servers,
    summary,
    load,
  }
})

