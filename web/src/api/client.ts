import axios from 'axios'

export interface Summary {
  totalServers: number
  runningServers: number
  proxyRules: number
  openEvents: number
}

export type ProxyType = 'tcp' | 'udp' | 'http' | 'https' | 'stcp' | 'xtcp'
export type ServerStatus =
  | 'running'
  | 'stopped'
  | 'config_dirty'
  | 'error'
  | 'starting'
  | 'reloading'

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
  createdAt?: string
  updatedAt?: string
}

export interface ProxyRuleInput {
  name: string
  type: ProxyType
  localIp: string
  localPort: number
  remotePort: number
  customDomains: string[]
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
  serverAddr: string
  serverPort: number
  authToken?: string
  transportProtocol: string
  status: ServerStatus
  autoStart: boolean
  autoRestart: boolean
  maxRestarts: number
  proxyCount: number
  uptime: string
  lastReloadAt: string
  restartRequired: boolean
  adminAddr: string
  adminPort: number
  adminUser?: string
  adminPassword?: string
  managementMode?: string // "managed" | "attached"
  rules?: ProxyRule[]
  createdAt?: string
  updatedAt?: string
}

export interface ServerInput {
  name: string
  serverAddr: string
  serverPort: number
  authToken: string
  transportProtocol: string
  autoStart: boolean
  autoRestart: boolean
  maxRestarts: number
  adminPort: number
  adminUser?: string
  adminPassword?: string
}

export interface HealthEvent {
  id: string
  level: 'info' | 'warning' | 'critical'
  serverId: string
  server: string
  message: string
  status: string
  createdAt: string
}

export interface FrpcVersion {
  id: string
  installed: boolean
  version: string
  latest: string
  path: string
  platform: string
  arch: string
  source: string
  active: boolean
  createdAt: string
}

export interface FrpcBinaryCandidate {
  path: string
  version: string
  managed: boolean
}

export interface FrpcProcessCandidate {
  pid: number
  exe: string
  configPath: string
  managed: boolean
  serverId?: string
  systemdManaged: boolean
  systemdUnit?: string
  hasAdminApi: boolean
  adminApiAddress?: string
}

export interface FrpcDiscovery {
  binaries: FrpcBinaryCandidate[]
  processes: FrpcProcessCandidate[]
}

export interface RegisterBinaryInput {
  path: string
}

export interface ImportFrpcConfigInput {
  name: string
  content: string
  autoStart: boolean
}

export interface AdoptProcessInput {
  pid: number
  configPath: string
  name: string
  mode: 'restart' | 'attach'
}

export interface AdoptResult {
  server: Server
  started: boolean
  message: string
}

export interface Settings {
  addr: string
  githubProxy: string
  autoBackupEnabled: boolean
  autoBackupIntervalHours: number
  autoBackupMaxFiles: number
  lastAutoBackupAt?: string
}

export interface SettingsInput {
  githubProxy: string
  autoBackupEnabled?: boolean
  autoBackupIntervalHours?: number
  autoBackupMaxFiles?: number
}

export interface BackupFile {
  name: string
  size: number
  createdAt: string
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

export interface Dashboard {
  summary: Summary
  servers: Server[]
  health: HealthEvent[]
  currentFrpc: FrpcVersion
  settings: Settings
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

export interface ConfigBundle {
  version: number
  exportedAt: string
  includeSensitive: boolean
  servers: Array<{ server: Server; rules: ProxyRule[] }>
  versions?: FrpcVersion[]
  githubProxy?: string
}

export interface ConfigImportInput {
  mode: 'merge' | 'replace'
  bundle: ConfigBundle
}

export interface OnlineInstallInput {
  version: string
  platform: string
  arch: string
  githubProxy: string
}

export interface LatestVersionInput {
  githubProxy: string
}

export interface LatestVersionResult {
  latest: string
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

export async function getAuditLogs(query: AuditLogQuery = {}) {
  const { data } = await http.get<AuditLogPage>('/audit-logs', { params: query })
  return data
}

export async function clearAuditLogs() {
  const { data } = await http.delete<{ ok: boolean }>('/audit-logs')
  return data
}

export async function getDashboard() {
  const { data } = await http.get<Dashboard>('/dashboard')
  return data
}

export async function getSettings() {
  const { data } = await http.get<Settings>('/settings')
  return data
}

export interface UpdateCheck {
  current: string
  latest: string
  hasUpdate: boolean
  notesUrl: string
  canApply: boolean
  applyHint?: string
}

export async function checkAppUpdate() {
  const { data } = await http.get<UpdateCheck>('/app/update/check')
  return data
}

export async function applyAppUpdate() {
  const { data } = await http.post<ActionResult>('/app/update/apply')
  return data
}

export async function updateSettings(input: SettingsInput) {
  const { data } = await http.put<Settings>('/settings', input)
  return data
}

export async function exportConfig() {
  const { data } = await http.get<ConfigBundle>('/config/export')
  return data
}

export async function importConfig(input: ConfigImportInput) {
  const { data } = await http.post<ActionResult>('/config/import', input)
  return data
}

export async function getBackups() {
  const { data } = await http.get<BackupFile[]>('/backups')
  return data
}

export async function createBackup() {
  const { data } = await http.post<BackupFile>('/backups')
  return data
}

export async function downloadBackup(name: string) {
  const { data } = await http.get<Blob>(`/backups/${encodeURIComponent(name)}`, {
    responseType: 'blob',
  })
  return data
}

export async function restoreBackup(name: string, mode: 'merge' | 'replace') {
  const { data } = await http.post<ActionResult>(`/backups/${encodeURIComponent(name)}/restore`, { mode })
  return data
}

export async function getProxiesStatus() {
  const { data } = await http.get<ServerProxyStatus[]>('/proxies/status')
  return data
}

export async function getServers() {
  const { data } = await http.get<Server[]>('/servers')
  return data
}

export async function createServer(input: ServerInput) {
  const { data } = await http.post<Server>('/servers', input)
  return data
}

export async function updateServer(id: string, input: ServerInput) {
  const { data } = await http.put<Server>(`/servers/${id}`, input)
  return data
}

export async function deleteServer(id: string) {
  const { data } = await http.delete<{ ok: boolean }>(`/servers/${id}`)
  return data
}

export async function startServer(id: string) {
  const { data } = await http.post<ActionResult>(`/servers/${id}/start`)
  return data
}

export async function stopServer(id: string) {
  const { data } = await http.post<ActionResult>(`/servers/${id}/stop`)
  return data
}

export async function restartServer(id: string) {
  const { data } = await http.post<ActionResult>(`/servers/${id}/restart`)
  return data
}

export async function reloadServer(id: string) {
  const { data } = await http.post<ActionResult>(`/servers/${id}/reload`)
  return data
}

export async function checkServer(id: string) {
  const { data } = await http.post<ActionResult>(`/servers/${id}/check`)
  return data
}

export async function createRule(serverId: string, input: ProxyRuleInput) {
  const { data } = await http.post<ProxyRule>(`/servers/${serverId}/rules`, input)
  return data
}

export async function updateRule(serverId: string, ruleId: string, input: ProxyRuleInput) {
  const { data } = await http.put<ProxyRule>(`/servers/${serverId}/rules/${ruleId}`, input)
  return data
}

export async function deleteRule(serverId: string, ruleId: string) {
  const { data } = await http.delete<{ ok: boolean }>(`/servers/${serverId}/rules/${ruleId}`)
  return data
}

export async function getServerLogs(serverId: string, tail = 200) {
  const { data } = await http.get<LogLine[]>(`/servers/${serverId}/logs`, {
    params: { tail },
  })
  return data
}

export async function getFrpcVersion() {
  const { data } = await http.get<FrpcVersion>('/frpc/version')
  return data
}

export async function getFrpcVersions() {
  const { data } = await http.get<FrpcVersion[]>('/frpc/versions')
  return data
}

export async function activateFrpcVersion(id: string) {
  const { data } = await http.post<FrpcVersion>(`/frpc/versions/${id}/activate`)
  return data
}

export async function checkLatestFrpc(input: LatestVersionInput) {
  const { data } = await http.post<LatestVersionResult>('/frpc/check-latest', input)
  return data
}

export async function installFrpcOnline(input: OnlineInstallInput) {
  const { data } = await http.post<FrpcVersion>('/frpc/install/online', input)
  return data
}

export async function installFrpcOffline(file: File) {
  const body = new FormData()
  body.append('file', file)
  const { data } = await http.post<FrpcVersion>('/frpc/install/offline', body)
  return data
}

export async function discoverFrpc() {
  const { data } = await http.get<FrpcDiscovery>('/frpc/discover')
  return data
}

export async function registerFrpcBinary(input: RegisterBinaryInput) {
  const { data } = await http.post<FrpcVersion>('/frpc/register', input)
  return data
}

export async function importFrpcConfig(input: ImportFrpcConfigInput) {
  const { data } = await http.post<Server>('/servers/import-frpc', input)
  return data
}

export async function adoptFrpcProcess(input: AdoptProcessInput) {
  const { data } = await http.post<AdoptResult>('/servers/adopt', input)
  return data
}
