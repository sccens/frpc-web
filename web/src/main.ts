import { createApp } from 'vue'
// v-loading 指令不在按需解析范围内，需要手动注册并引入其样式。
import { ElLoading } from 'element-plus'
import 'element-plus/es/components/loading/style/css'
import App from './App.vue'
import { router } from './router'
import { pinia } from './stores'
import { applyThemePreference, loadThemePreference } from './utils/theme'
import './styles/main.css'

applyThemePreference(loadThemePreference())

createApp(App).use(pinia).use(router).use(ElLoading).mount('#app')
