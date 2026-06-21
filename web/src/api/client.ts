import axios from 'axios'

// v2.0：面板只读监控 frpc 配置文件，并提供原文编辑与 admin API 热重载。
// 不再管理进程、版本、规则 CRUD，相关类型与方法已移除。

export type ProxyType = 'tcp' | 'udp' | 'http' | 'https' | 'stcp' | 'xtcp'
export type ServerStatus = 'running' | 'stopped' | 'error' | 'no-admin'

export interface ProxyRule {
  id: string
  serverId: string
  name: string
  type: ProxyType
  localIp: string
  localPort: number
  remotePort?: number
  customDomains?: string[]
  enabled: boolean
  secretKey?: string
  role?: 'server' | 'visitor'
  serverName?: string
  bindAddr?: string
  bindPort?: number
  useEncryption: boolean
  useCompression: boolean
  bandwidthLimit?: string
  locations?: string[]
  hostHeaderRewrite?: string
  httpUser?: string
  httpPassword?: string
  requestHeaders?: string[]
}

export interface Server {
  id: string
  name: string
  configPath: string
  serverAddr: string
  serverPort: number
  authToken?: string
  transportProtocol: string
  status: ServerStatus
  adminAddr: string
  adminPort: number
  adminUser?: string
  adminPassword?: string
  logPath?: string
  writable: boolean
  proxyCount: number
  rules?: ProxyRule[]
}

export interface Settings {
  addr: string
  githubProxy: string
}

export interface SettingsInput {
  githubProxy: string
}

export interface ConfigFile {
  path: string
  content: string
  writable: boolean
  isToml: boolean
}

export interface ConfigBundle {
  version: number
  exportedAt: string
  files: ConfigFile[]
  githubProxy?: string
}

export interface ConfigImportInput {
  bundle: ConfigBundle
}

export type ProxyPhase = 'new' | 'wait start' | 'start error' | 'running' | 'check failed' | 'closed'

export interface ProxyStatus {
  name: string
  type: string
  phase: ProxyPhase
  err?: string
  localAddr?: string
  plugin?: string
  remoteAddr?: string
}

export interface ServerProxyStatus {
  serverId: string
  running: boolean
  error?: string
  proxies: ProxyStatus[]
}

export interface AuthStatus {
  authenticated: boolean
  mustChangePassword: boolean
}

export interface AuthInput {
  accessKey: string
}

export interface AccessKeyInput {
  currentAccessKey: string
  newAccessKey: string
}

export interface LogLine {
  time: string
  level: string
  message: string
}

export interface ActionResult {
  ok: boolean
  message: string
  output?: string
}

export interface UpdateCheck {
  current: string
  latest: string
  hasUpdate: boolean
  notesUrl: string
  canApply: boolean
  applyHint?: string
}

export interface AuditLog {
  id: string
  ip: string
  userAgent: string
  action: string
  resourceType: string
  resourceId: string
  result: 'success' | 'failure'
  error?: string
  createdAt: string
}

export interface AuditLogPage {
  items: AuditLog[]
  total: number
  page: number
  pageSize: number
}

export interface AuditLogQuery {
  page?: number
  pageSize?: number
  action?: string
  result?: string
}

const http = axios.create({
  baseURL: '/api',
  timeout: 120000,
})

// ——— 认证 ———

export async function getAuthStatus() {
  const { data } = await http.get<AuthStatus>('/auth/status')
  return data
}

export async function login(input: AuthInput) {
  const { data } = await http.post<{ mustChangePassword: boolean }>('/auth/login', input)
  return data
}

export async function logout() {
  const { data } = await http.post<{ ok: boolean }>('/auth/logout')
  return data
}

export async function changeAccessKey(input: AccessKeyInput) {
  const { data } = await http.post<{ ok: boolean }>('/auth/access-key', input)
  return data
}

// ——— 审计 ———

export async function getAuditLogs(query: AuditLogQuery = {}) {
  const { data } = await http.get<AuditLogPage>('/audit-logs', { params: query })
  return data
}

export async function clearAuditLogs() {
  const { data } = await http.delete<{ ok: boolean }>('/audit-logs')
  return data
}

// ——— 设置 ———

export async function getSettings() {
  const { data } = await http.get<Settings>('/settings')
  return data
}

export async function updateSettings(input: SettingsInput) {
  const { data } = await http.put<Settings>('/settings', input)
  return data
}

// ——— 配置导出/导入 ———

export async function exportConfig() {
  const { data } = await http.get<ConfigBundle>('/config/export')
  return data
}

export async function importConfig(input: ConfigImportInput) {
  const { data } = await http.post<ActionResult>('/config/import', input)
  return data
}

// ——— 配置文件读写与热重载 ———

export async function getConfigFiles() {
  const { data } = await http.get<ConfigFile[]>('/config-files')
  return data
}

export async function readConfigFile(id: string) {
  const { data } = await http.get<ConfigFile>(`/config-files/${id}`)
  return data
}

export async function saveConfigFile(id: string, content: string) {
  const { data } = await http.put<ActionResult>(`/config-files/${id}`, { content })
  return data
}

// ——— server 列表与状态（只读） ———

export async function getProxiesStatus() {
  const { data } = await http.get<ServerProxyStatus[]>('/proxies/status')
  return data
}

export async function getServers() {
  const { data } = await http.get<Server[]>('/servers')
  return data
}

export async function reloadViaAdmin(id: string) {
  const { data } = await http.post<ActionResult>(`/servers/${id}/reload`)
  return data
}

export async function getServerLogs(serverId: string, tail = 200) {
  const { data } = await http.get<LogLine[]>(`/servers/${serverId}/logs`, {
    params: { tail },
  })
  return data
}

// ——— frpc-web 自身更新 ———

export async function checkAppUpdate() {
  const { data } = await http.get<UpdateCheck>('/app/update/check')
  return data
}

export async function applyAppUpdate() {
  const { data } = await http.post<ActionResult>('/app/update/apply')
  return data
}
