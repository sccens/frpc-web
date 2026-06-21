<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { Handle, MarkerType, Position, VueFlow, type Edge, type Node } from '@vue-flow/core'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'
import { Box, Network, Route, Server } from 'lucide-vue-next'
import {
  getProxiesStatus,
  getServers,
  type ProxyRule,
  type ProxyStatus,
  type Server as ServerType,
  type ServerProxyStatus,
} from '../api/client'
import { errorMessage } from '../utils/errors'

const ALL_SERVERS = '__all__'
const MAX_DETAIL_RULES = 8
const MAX_OVERVIEW_RULES = 4
const OVERVIEW_ROW_GAP = 104
const OVERVIEW_GROUP_GAP = 72
const DETAIL_ROW_GAP = 112
const STATUS_POLL_INTERVAL = 3000

type TopologyNodeKind = 'local-service' | 'public-entry' | 'frpc' | 'frps' | 'more' | 'empty'

interface TopologyNodeData {
  kind: TopologyNodeKind
  label: string
  title: string
  subtitle?: string
  status?: string
  badge?: string
  // 实时状态查询键：节点数组保持静态（避免轮询打断拖拽布局），
  // 徽标在渲染时按键从 proxyStatuses 反查。
  serverId?: string
  ruleName?: string
}

type TopologyNode = Node<TopologyNodeData, any, 'topology'>
type TopologyEdge = Edge<Record<string, never>>

interface ServerCard {
  server: ServerType
  activeRules: ProxyRule[]
  previewRules: ProxyRule[]
  hiddenRules: number
  endpoint: string
  endpointKey: string
}

interface OverviewGroup {
  id: string
  endpointKey: string
  endpoint: string
  cards: ServerCard[]
  rowCount: number
  startY: number
  centerY: number
}

const loading = ref(false)
const error = ref('')
const servers = ref<ServerType[]>([])
const selectedServer = ref(ALL_SERVERS)
const proxyStatuses = ref<Map<string, ServerProxyStatus>>(new Map())
const statusLive = ref(false)
let statusTimer: number | undefined

onMounted(() => {
  void load()
  startStatusPolling()
  document.addEventListener('visibilitychange', handleVisibilityChange)
})

onBeforeUnmount(() => {
  stopStatusPolling()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})

// 仅在页面可见时轮询；切到后台暂停，回来立即刷新一次。
function handleVisibilityChange() {
  if (document.hidden) {
    stopStatusPolling()
  } else {
    startStatusPolling()
  }
}

function startStatusPolling() {
  if (statusTimer !== undefined) return
  void refreshProxyStatuses()
  statusTimer = window.setInterval(() => void refreshProxyStatuses(), STATUS_POLL_INTERVAL)
}

function stopStatusPolling() {
  if (statusTimer === undefined) return
  window.clearInterval(statusTimer)
  statusTimer = undefined
}

async function refreshProxyStatuses() {
  try {
    const list = await getProxiesStatus()
    proxyStatuses.value = new Map(list.map((item) => [item.serverId, item]))
    statusLive.value = true
  } catch {
    // 轮询失败不打扰用户，拓扑退化为静态配置视图
    statusLive.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    servers.value = await getServers()
    const selectedExists = servers.value.some((server) => server.id === selectedServer.value)
    if (selectedServer.value !== ALL_SERVERS && !selectedExists) {
      selectedServer.value = ALL_SERVERS
    }
  } catch (err) {
    error.value = errorMessage(err, '加载失败')
  } finally {
    loading.value = false
  }
}

const currentServer = computed(() => {
  if (selectedServer.value === ALL_SERVERS) return null
  return servers.value.find((server) => server.id === selectedServer.value) || null
})

const serverCards = computed<ServerCard[]>(() => servers.value.map((server) => {
  const rules = server.rules || []
  const activeRules = rules.filter((rule) => rule.enabled)
  return {
    server,
    activeRules,
    previewRules: activeRules.slice(0, MAX_OVERVIEW_RULES),
    hiddenRules: Math.max(0, activeRules.length - MAX_OVERVIEW_RULES),
    endpoint: getServerEndpoint(server),
    endpointKey: getServerEndpointKey(server),
  }
}))

const overviewGroups = computed(() => buildOverviewGroups())
const overviewGroupCount = computed(() => overviewGroups.value.length)

const overviewSummary = computed(() => {
  const activeRules = serverCards.value.reduce((total, card) => total + card.activeRules.length, 0)
  return {
    activeRules,
    runningServers: servers.value.filter((server) => server.status === 'running').length,
  }
})

const detailRules = computed(() => (currentServer.value?.rules || []).filter((rule) => rule.enabled))
const detailVisibleRules = computed(() => detailRules.value.slice(0, MAX_DETAIL_RULES))
const detailHiddenRules = computed(() => Math.max(0, detailRules.value.length - MAX_DETAIL_RULES))
const detailRowCount = computed(() => {
  const visibleRows = detailVisibleRules.value.length + (detailHiddenRules.value > 0 ? 1 : 0)
  return Math.max(1, visibleRows)
})

const flowNodes = computed<TopologyNode[]>(() => (
  selectedServer.value === ALL_SERVERS ? buildOverviewNodes() : buildDetailNodes()
))

const flowEdges = computed<TopologyEdge[]>(() => (
  selectedServer.value === ALL_SERVERS ? buildOverviewEdges() : buildDetailEdges()
))

const flowKey = computed(() => {
  if (selectedServer.value !== ALL_SERVERS) {
    return `${selectedServer.value}-${detailRules.value.length}-${detailHiddenRules.value}`
  }

  const groupSignature = overviewGroups.value
    .map((group) => `${group.endpointKey}:${group.cards.length}:${group.rowCount}`)
    .join('|')
  return `${ALL_SERVERS}-${groupSignature}-${overviewSummary.value.activeRules}`
})
const flowRowCount = computed(() => (
  selectedServer.value === ALL_SERVERS
    ? Math.max(overviewGroups.value.reduce((total, group) => total + group.rowCount, 0), 1)
    : detailRowCount.value
))
const flowHeight = computed(() => {
  const groupGapHeight = selectedServer.value === ALL_SERVERS
    ? Math.max(0, overviewGroups.value.length - 1) * OVERVIEW_GROUP_GAP
    : 0
  return `${Math.min(Math.max(420, flowRowCount.value * 108 + groupGapHeight + 150), 920)}px`
})

const flowTitle = computed(() => selectedServer.value === ALL_SERVERS ? '全局连接视图' : currentServer.value?.name || '服务端拓扑')
const flowDescription = computed(() => {
  if (selectedServer.value === ALL_SERVERS) {
    return `1 台本机客户端连接 ${overviewGroupCount.value} 个服务端入口，${overviewSummary.value.runningServers}/${servers.value.length} 个服务端配置运行中。`
  }
  return `${detailRules.value.length} 条启用规则，从本地端口到公网入口一一对应。`
})

function buildOverviewGroups(): OverviewGroup[] {
  const grouped = new Map<string, { endpointKey: string; endpoint: string; cards: ServerCard[] }>()

  serverCards.value.forEach((card) => {
    const endpointKey = card.endpointKey || card.endpoint || 'unconfigured'
    const endpoint = card.endpoint || '服务端未配置'
    const existing = grouped.get(endpointKey)
    if (existing) {
      existing.cards.push(card)
    } else {
      grouped.set(endpointKey, { endpointKey, endpoint, cards: [card] })
    }
  })

  let offsetY = 0
  return Array.from(grouped.values()).map((group, index) => {
    const rowCount = Math.max(1, group.cards.reduce((total, card) => total + getOverviewCardRows(card), 0))
    const startY = offsetY
    const centerY = startY + (rowCount - 1) * OVERVIEW_ROW_GAP / 2
    offsetY += rowCount * OVERVIEW_ROW_GAP + OVERVIEW_GROUP_GAP

    return {
      id: `overview-frps-${index}-${safeId(group.endpointKey)}`,
      endpointKey: group.endpointKey,
      endpoint: group.endpoint,
      cards: group.cards,
      rowCount,
      startY,
      centerY,
    }
  })
}

function buildOverviewNodes() {
  const nodes: TopologyNode[] = []
  if (overviewGroups.value.length === 0) return nodes

  const frpcCenterY = getOverviewCenterY()

  nodes.push(createNode('overview-frpc', 'frpc', 330, frpcCenterY, {
    label: '客户端',
    title: '本机 FRPC',
    subtitle: `统一连接 ${overviewGroupCount.value} 个服务端入口`,
    badge: servers.value.length > 0 ? '本机' : '待配置',
  }, 230))

  overviewGroups.value.forEach((group) => {
    nodes.push(createNode(group.id, 'frps', 660, group.centerY, {
      label: '服务端入口',
      title: group.endpoint,
      subtitle: `${group.cards.length} 个服务端配置使用此入口`,
      badge: getGroupTransportBadge(group.cards),
    }, 300))

    let cardOffsetY = group.startY
    group.cards.forEach((card) => {
      const rows = getOverviewCardRows(card)
      const centerY = cardOffsetY + (rows - 1) * OVERVIEW_ROW_GAP / 2

      if (card.previewRules.length === 0) {
        nodes.push(createNode(`overview-empty-${card.server.id}`, 'empty', 0, centerY, {
          label: '本地服务',
          title: '还没有可转发的服务',
          subtitle: '启用代理规则后会出现在这里',
          badge: '空闲',
        }, 230))
        nodes.push(createNode(`overview-public-empty-${card.server.id}`, 'empty', 1030, centerY, {
          label: '公网访问',
          title: '公网入口待生成',
          subtitle: '配置远端端口或域名后显示',
          badge: '等待配置',
        }, 240))
      } else {
        card.previewRules.forEach((rule, index) => {
          const rowY = cardOffsetY + index * OVERVIEW_ROW_GAP
          nodes.push(createNode(getOverviewRuleNodeId('local', card, rule), 'local-service', 0, rowY, createLocalRuleData(rule), 230))
          nodes.push(createNode(getOverviewRuleNodeId('public', card, rule), 'public-entry', 1030, rowY, createPublicRuleData(rule), 250))
        })

        if (card.hiddenRules > 0) {
          const rowY = cardOffsetY + card.previewRules.length * OVERVIEW_ROW_GAP
          nodes.push(createNode(`overview-more-local-${card.server.id}`, 'more', 0, rowY, {
            label: '更多本地服务',
            title: `还有 ${card.hiddenRules} 条未展开`,
            subtitle: '进入该服务端配置查看完整链路',
            badge: '已折叠',
          }, 230))
          nodes.push(createNode(`overview-more-public-${card.server.id}`, 'more', 1030, rowY, {
            label: '更多公网入口',
            title: `还有 ${card.hiddenRules} 个入口`,
            subtitle: '与左侧折叠服务一一对应',
            badge: '已折叠',
          }, 250))
        }
      }

      cardOffsetY += rows * OVERVIEW_ROW_GAP
    })
  })

  return nodes
}

function buildOverviewEdges() {
  const edges: TopologyEdge[] = []
  let ruleColorIndex = 0

  overviewGroups.value.forEach((group) => {
    edges.push(createEdge(
      `overview-frpc-frps-${group.id}`,
      'overview-frpc',
      group.id,
      '加密隧道',
      'success',
    ))

    group.cards.forEach((card) => {
      if (card.previewRules.length === 0) {
        edges.push(createEdge(`overview-empty-frpc-${card.server.id}`, `overview-empty-${card.server.id}`, 'overview-frpc', card.server.name, 'muted'))
        edges.push(createEdge(`overview-frps-public-empty-${card.server.id}`, group.id, `overview-public-empty-${card.server.id}`, '等待入口', 'muted'))
      } else {
        card.previewRules.forEach((rule) => {
          const localNodeId = getOverviewRuleNodeId('local', card, rule)
          const publicNodeId = getOverviewRuleNodeId('public', card, rule)
          const color = ruleEdgeColor(ruleColorIndex++)
          edges.push(createEdge(
            `overview-local-frpc-${card.server.id}-${rule.id}`,
            localNodeId,
            'overview-frpc',
            getLocalPortLabel(rule),
            'primary',
            color,
          ))
          edges.push(createEdge(
            `overview-frps-public-${card.server.id}-${rule.id}`,
            group.id,
            publicNodeId,
            getPublicPortLabel(rule),
            'primary',
            color,
          ))
        })

        if (card.hiddenRules > 0) {
          edges.push(createEdge(`overview-more-local-frpc-${card.server.id}`, `overview-more-local-${card.server.id}`, 'overview-frpc', '更多服务', 'muted'))
          edges.push(createEdge(`overview-frps-more-public-${card.server.id}`, group.id, `overview-more-public-${card.server.id}`, '更多入口', 'muted'))
        }
      }
    })
  })

  return edges
}

function buildDetailNodes() {
  if (!currentServer.value) return []

  const centerY = (detailRowCount.value - 1) * DETAIL_ROW_GAP / 2
  const nodes: TopologyNode[] = []

  if (detailVisibleRules.value.length === 0) {
    nodes.push(createNode('detail-empty', 'empty', 0, centerY, {
      label: '本地服务',
      title: '还没有可转发的服务',
      subtitle: '创建并启用代理后，这里会显示连接路径。',
      badge: '空闲',
    }, 240))
    nodes.push(createNode('detail-public-empty', 'empty', 1030, centerY, {
      label: '公网访问',
      title: '公网入口待生成',
      subtitle: '配置远端端口或域名后显示',
      badge: '等待配置',
    }, 240))
  } else {
    detailVisibleRules.value.forEach((rule, index) => {
      nodes.push(createNode(`detail-local-${rule.id}`, 'local-service', 0, index * DETAIL_ROW_GAP, createLocalRuleData(rule), 250))
      nodes.push(createNode(`detail-public-${rule.id}`, 'public-entry', 1030, index * DETAIL_ROW_GAP, createPublicRuleData(rule), 250))
    })

    if (detailHiddenRules.value > 0) {
      nodes.push(createNode('detail-more-local', 'more', 0, detailVisibleRules.value.length * DETAIL_ROW_GAP, {
        label: '更多服务',
        title: `还有 ${detailHiddenRules.value} 条未展开`,
        subtitle: '已折叠，避免拓扑过密',
        badge: '已折叠',
      }, 250))
      nodes.push(createNode('detail-more-public', 'more', 1030, detailVisibleRules.value.length * DETAIL_ROW_GAP, {
        label: '更多公网入口',
        title: `还有 ${detailHiddenRules.value} 个入口`,
        subtitle: '与左侧折叠服务一一对应',
        badge: '已折叠',
      }, 250))
    }
  }

  nodes.push(createNode('detail-frpc', 'frpc', 330, centerY, {
    label: '客户端',
    title: '本机 FRPC',
    subtitle: `连接到 ${getServerEndpoint(currentServer.value)}`,
    status: currentServer.value.status,
    badge: getStatusText(currentServer.value.status),
    serverId: currentServer.value.id,
  }, 220))

  nodes.push(createNode('detail-frps', 'frps', 660, centerY, {
    label: '服务端入口',
    title: getServerEndpoint(currentServer.value),
    subtitle: currentServer.value.name,
    badge: getTransportText(currentServer.value.transportProtocol),
  }, 300))

  return nodes
}

function buildDetailEdges() {
  if (!currentServer.value) return []

  const edges: TopologyEdge[] = []
  if (detailVisibleRules.value.length === 0) {
    edges.push(createEdge('detail-empty-frpc', 'detail-empty', 'detail-frpc', '等待启用', 'muted'))
    edges.push(createEdge('detail-frps-public-empty', 'detail-frps', 'detail-public-empty', '等待入口', 'muted'))
  } else {
    detailVisibleRules.value.forEach((rule, index) => {
      const color = ruleEdgeColor(index)
      edges.push(createEdge(`detail-local-frpc-${rule.id}`, `detail-local-${rule.id}`, 'detail-frpc', getLocalPortLabel(rule), 'primary', color))
      edges.push(createEdge(`detail-frps-public-${rule.id}`, 'detail-frps', `detail-public-${rule.id}`, getPublicPortLabel(rule), 'primary', color))
    })
    if (detailHiddenRules.value > 0) {
      edges.push(createEdge('detail-more-local-frpc', 'detail-more-local', 'detail-frpc', '更多服务', 'muted'))
      edges.push(createEdge('detail-frps-more-public', 'detail-frps', 'detail-more-public', '更多入口', 'muted'))
    }
  }

  edges.push(createEdge(
    'detail-frpc-frps',
    'detail-frpc',
    'detail-frps',
    '加密隧道',
    'success',
  ))

  return edges
}

function createNode(
  id: string,
  kind: TopologyNodeKind,
  x: number,
  y: number,
  data: Omit<TopologyNodeData, 'kind'>,
  width: number,
): TopologyNode {
  return {
    id,
    type: 'topology',
    position: { x, y },
    sourcePosition: Position.Right,
    targetPosition: Position.Left,
    draggable: true,
    selectable: true,
    connectable: false,
    dragHandle: '.flow-card-drag',
    width,
    data: {
      kind,
      ...data,
    },
  }
}

// 每条规则分配一种专属颜色：本地服务→客户端、服务端→公网入口两段连线同色，便于对应
const RULE_EDGE_COLORS = ['#2563eb', '#f59e0b', '#8b5cf6', '#ec4899', '#06b6d4', '#f97316', '#84cc16', '#14b8a6']

function ruleEdgeColor(index: number) {
  return RULE_EDGE_COLORS[index % RULE_EDGE_COLORS.length]
}

function createEdge(id: string, source: string, target: string, label: string, tone: 'primary' | 'success' | 'muted', color?: string): TopologyEdge {
  const markerColor = color ?? (tone === 'success' ? '#10b981' : tone === 'primary' ? '#2563eb' : '#a1a1aa')
  return {
    id,
    source,
    target,
    label,
    type: 'smoothstep',
    animated: tone !== 'muted',
    selectable: false,
    focusable: false,
    updatable: false,
    interactionWidth: 18,
    markerEnd: {
      type: MarkerType.ArrowClosed,
      color: markerColor,
      width: 16,
      height: 16,
    },
    class: ['flow-edge', `flow-edge-${tone}`],
    style: color ? { stroke: color } : undefined,
    labelBgPadding: [7, 4],
    labelBgBorderRadius: 9,
    labelStyle: {
      fill: color ?? 'var(--muted)',
      fontSize: 11,
      fontWeight: 700,
    },
    labelBgStyle: {
      fill: 'var(--panel-solid)',
      fillOpacity: 0.92,
    },
    pathOptions: {
      borderRadius: 18,
      offset: 24,
    },
  }
}

function getOverviewCardRows(card: ServerCard) {
  return Math.max(1, card.previewRules.length + (card.hiddenRules > 0 ? 1 : 0))
}

function getOverviewCenterY() {
  const groups = overviewGroups.value
  if (groups.length === 0) return 0

  const firstGroup = groups[0]
  const lastGroup = groups[groups.length - 1]
  const bottomY = lastGroup.startY + (lastGroup.rowCount - 1) * OVERVIEW_ROW_GAP
  return (firstGroup.startY + bottomY) / 2
}

function getOverviewRuleNodeId(side: 'local' | 'public', card: ServerCard, rule: ProxyRule) {
  return `overview-${side}-${card.server.id}-${rule.id}`
}

function getGroupTransportBadge(cards: ServerCard[]) {
  const transports = Array.from(new Set(cards.map((card) => getTransportText(card.server.transportProtocol))))
  return transports.length === 1 ? transports[0] : `${transports.length} 类通道`
}

function safeId(value: string) {
  const normalized = value.trim().replace(/[^a-zA-Z0-9_-]+/g, '_').replace(/^_+|_+$/g, '')
  return normalized || 'endpoint'
}

function createLocalRuleData(rule: ProxyRule): Omit<TopologyNodeData, 'kind'> {
  return {
    label: '本地服务',
    title: rule.name,
    subtitle: `${localEndpoint(rule)}`,
    badge: getRuleTypeText(rule),
    serverId: rule.serverId,
    ruleName: rule.name,
  }
}

interface LiveBadge {
  tone: 'ok' | 'warn' | 'error' | 'muted'
  text: string
  err?: string
}

// 返回 0 或 1 个元素的数组，模板里用 v-for 解构，避免重复调用。
function liveBadgesFor(data: TopologyNodeData): LiveBadge[] {
  if (!data.serverId) return []
  const serverStatus = proxyStatuses.value.get(data.serverId)
  if (!serverStatus) return []

  if (data.kind === 'frpc') {
    if (!serverStatus.running) return [{ tone: 'muted', text: '进程未运行' }]
    if (serverStatus.error) return [{ tone: 'warn', text: '实时状态不可用', err: serverStatus.error }]
    const total = serverStatus.proxies.length
    const running = serverStatus.proxies.filter((proxy) => proxy.phase === 'running').length
    if (total === 0) return [{ tone: 'muted', text: '无活动代理' }]
    return [{ tone: running === total ? 'ok' : 'warn', text: `${running}/${total} 代理运行中` }]
  }

  if (data.kind !== 'local-service' || !data.ruleName) return []
  if (!serverStatus.running) return [{ tone: 'muted', text: '进程未运行' }]
  if (serverStatus.error) return [{ tone: 'warn', text: '实时状态不可用', err: serverStatus.error }]
  const proxy = serverStatus.proxies.find((item) => item.name === data.ruleName)
  // visitor 等类型不会出现在 frpc /api/status 中，保持纯配置展示。
  if (!proxy) return []
  return [livePhaseBadge(proxy)]
}

function livePhaseBadge(proxy: ProxyStatus): LiveBadge {
  switch (proxy.phase) {
    case 'running':
      return { tone: 'ok', text: '运行中' }
    case 'start error':
      return { tone: 'error', text: '启动失败', err: proxy.err }
    case 'check failed':
      return { tone: 'error', text: '健康检查未通过', err: proxy.err }
    case 'new':
    case 'wait start':
      return { tone: 'warn', text: '等待启动', err: proxy.err }
    case 'closed':
      return { tone: 'muted', text: '已关闭', err: proxy.err }
    default:
      return { tone: 'warn', text: proxy.phase, err: proxy.err }
  }
}

function createPublicRuleData(rule: ProxyRule): Omit<TopologyNodeData, 'kind'> {
  return {
    label: '公网访问',
    title: publicEndpoint(rule),
    subtitle: rule.name,
    badge: '入口',
  }
}

function getStatusText(status?: string) {
  const labels: Record<string, string> = {
    running: '运行中',
    stopped: '已停止',
    error: '异常',
    starting: '启动中',
    reloading: '重载中',
    config_dirty: '待重载',
  }
  return labels[status ?? ''] || status || '-'
}

function getStatusClass(status?: string) {
  return `status-${status || 'unknown'}`
}

function localEndpoint(rule: ProxyRule) {
  return `${rule.localIp || '127.0.0.1'}:${rule.localPort || '-'}`
}

// 公网访问卡片只展示端口/域名；服务端地址统一在“服务端入口”卡片上显示
function publicEndpoint(rule: ProxyRule) {
  if (rule.type === 'http' || rule.type === 'https') {
    const domains = rule.customDomains?.map((domain) => domain.trim()).filter(Boolean) ?? []
    if (domains.length > 0) return domains.join(', ')
    return `${rule.type.toUpperCase()} 域名待配置`
  }

  const port = rule.remotePort || rule.bindPort
  return port ? `端口 ${port}` : '端口待配置'
}

function getLocalPortLabel(rule: ProxyRule) {
  return rule.localPort ? `${rule.localPort} 端口` : '本地端口'
}

function getPublicPortLabel(rule: ProxyRule) {
  if (rule.type === 'http' || rule.type === 'https') {
    return (rule.customDomains ?? []).length > 0 ? '域名访问' : '等待域名'
  }
  if (rule.remotePort) return `${rule.remotePort} 端口`
  if (rule.bindPort) return `${rule.bindPort} 端口`
  return '等待入口'
}

function getTransportText(protocol?: string) {
  const labels: Record<string, string> = {
    tcp: '直连通道',
    kcp: '快速通道',
    quic: 'QUIC 通道',
    websocket: 'WebSocket 通道',
    wss: '加密 WebSocket',
  }
  return labels[(protocol || 'tcp').toLowerCase()] || `${protocol?.toUpperCase() || 'TCP'} 通道`
}

function getRuleTypeText(rule: ProxyRule) {
  const labels: Record<string, string> = {
    tcp: '端口转发',
    udp: 'UDP 转发',
    http: 'HTTP 站点',
    https: 'HTTPS 站点',
    stcp: '安全隧道',
    xtcp: '点对点隧道',
  }
  return labels[rule.type] || `${rule.type.toUpperCase()} 代理`
}

function getServerEndpoint(server: ServerType) {
  return formatEndpoint(normalizeEndpointAddr(server.serverAddr), server.serverPort || '待配置')
}

function getServerEndpointKey(server: ServerType) {
  const addr = normalizeEndpointAddr(server.serverAddr)
    .replace(/^\[(.*)\]$/, '$1')
    .toLowerCase()
  return `${addr || 'unconfigured'}:${Number(server.serverPort) || 0}`
}

function normalizeEndpointAddr(value?: string) {
  return String(value || '')
    .normalize('NFKC')
    .replace(/[\u200B-\u200D\uFEFF]/g, '')
    .trim()
    .replace(/\s+/g, '')
}

function formatEndpoint(addr: string, port: number | string) {
  const cleanAddr = normalizeEndpointAddr(addr)
  const displayAddr = cleanAddr.includes(':') && !cleanAddr.startsWith('[') ? `[${cleanAddr}]` : cleanAddr
  return `${displayAddr || '服务端未配置'}:${port || '待配置'}`
}
</script>

<template>
  <div class="page-stack animate-enter" v-loading="loading">
    <el-alert v-if="error" type="error" :title="error" show-icon />

    <section class="ops-panel">
      <div class="ops-copy">
        <p class="overline">网络</p>
        <h1>网络拓扑</h1>
        <div class="ops-meta">
          <span><i class="live-dot" /> 可拖拽拓扑图</span>
          <code>{{ servers.length }} 服务端配置 · {{ overviewSummary.activeRules }} 启用规则</code>
        </div>
      </div>
      <div class="ops-rail">
        <el-select v-model="selectedServer" class="server-picker" placeholder="选择服务端配置">
          <el-option label="全部服务端配置" :value="ALL_SERVERS" />
          <el-option v-for="server in servers" :key="server.id" :label="server.name" :value="server.id" />
        </el-select>
      </div>
    </section>

    <section v-if="flowNodes.length > 0" class="topology-canvas">
      <div class="topology-overview-head">
        <div>
          <p class="overline">拓扑视图</p>
          <h2>{{ flowTitle }}</h2>
          <span>{{ flowDescription }}</span>
        </div>
        <span class="fleet-badge">
          {{ flowNodes.length }} 个节点 · {{ flowEdges.length }} 条连线
          <em class="fleet-live" :class="{ off: !statusLive }">{{ statusLive ? '实时' : '离线' }}</em>
        </span>
      </div>

      <div class="topology-flow-shell" :style="{ height: flowHeight }">
        <VueFlow
          :key="flowKey"
          class="topology-flow"
          :nodes="flowNodes"
          :edges="flowEdges"
          fit-view-on-init
          :min-zoom="0.42"
          :max-zoom="1.18"
          :nodes-draggable="true"
          :nodes-connectable="false"
          :elements-selectable="true"
          :zoom-on-scroll="false"
          :zoom-on-double-click="false"
          :pan-on-drag="true"
          :zoom-on-pinch="true"
          :prevent-scrolling="false"
        >
          <Controls position="bottom-right" />

          <template #node-topology="{ data }">
            <article class="flow-card" :class="[`flow-card-${data.kind}`, data.status ? getStatusClass(data.status) : '']">
              <Handle type="target" :position="Position.Left" class="flow-handle" />
              <Handle type="source" :position="Position.Right" class="flow-handle" />

              <div class="flow-card-top flow-card-drag">
                <span class="flow-avatar" :class="`avatar-${data.kind}`">
                  <Box v-if="data.kind === 'local-service'" :size="15" :stroke-width="1.8" />
                  <Network v-else-if="data.kind === 'frpc'" :size="16" :stroke-width="1.9" />
                  <Server v-else-if="data.kind === 'frps'" :size="16" :stroke-width="1.9" />
                  <Route v-else-if="data.kind === 'public-entry'" :size="15" :stroke-width="1.8" />
                  <Route v-else :size="15" :stroke-width="1.8" />
                </span>
                <div class="flow-card-copy">
                  <p class="flow-label">{{ data.label }}</p>
                  <h3 :title="data.title">{{ data.title }}</h3>
                  <span v-if="data.subtitle" :title="data.subtitle">{{ data.subtitle }}</span>
                </div>
                <span v-if="data.badge" class="status-chip compact">{{ data.badge }}</span>
              </div>

              <div
                v-for="live in liveBadgesFor(data)"
                :key="live.text"
                class="flow-live"
                :class="`flow-live-${live.tone}`"
              >
                <span class="flow-live-dot" />
                <span class="flow-live-text">{{ live.text }}</span>
                <span v-if="live.err" class="flow-live-err" :title="live.err">{{ live.err }}</span>
              </div>

            </article>
          </template>
        </VueFlow>
      </div>
    </section>

    <section v-if="servers.length === 0 && !loading" class="security-band compact">
      <Network :size="18" :stroke-width="1.8" />
      <div>
        <strong>暂无服务端配置</strong>
        <p>请先创建服务端入口和代理规则</p>
      </div>
    </section>
  </div>
</template>

<style scoped>
.topology-canvas {
  position: relative;
  overflow: hidden;
  padding: 22px;
  border: 1px solid var(--line);
  border-radius: var(--radius-lg);
  background:
    radial-gradient(circle at 12% 14%, rgba(37, 99, 235, 0.14), transparent 30%),
    radial-gradient(circle at 84% 18%, rgba(16, 185, 129, 0.13), transparent 28%),
    var(--panel);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.78),
    var(--shadow-soft);
  backdrop-filter: blur(12px);
}

.server-picker {
  width: 240px;
}

.topology-overview-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.topology-overview-head h2 {
  margin: 0;
  color: var(--text);
  font-size: 20px;
}

.topology-overview-head span {
  display: block;
  margin-top: 4px;
  color: var(--muted);
  font-size: 13px;
}

.fleet-badge {
  flex: 0 0 auto;
  padding: 7px 11px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--code-bg);
  color: var(--text) !important;
  font-size: 12px !important;
  font-weight: 700;
}

.topology-flow-shell {
  min-height: 360px;
  overflow: hidden;
  border: 1px solid var(--line-soft);
  border-radius: 22px;
  background:
    linear-gradient(to right, rgba(212, 212, 216, 0.34) 1px, transparent 1px),
    linear-gradient(to bottom, rgba(212, 212, 216, 0.34) 1px, transparent 1px),
    rgba(250, 250, 250, 0.46);
  background-size: 30px 30px;
}

.topology-flow {
  --vf-node-bg: transparent;
  --vf-node-text: var(--text);
  --vf-handle: transparent;
  --vf-connection-path: var(--line);
}

:deep(.vue-flow__pane) {
  cursor: grab;
}

:deep(.vue-flow__pane.dragging) {
  cursor: grabbing;
}

:deep(.vue-flow__node) {
  cursor: grab;
}

:deep(.vue-flow__node.dragging) {
  cursor: grabbing;
}

:deep(.vue-flow__node.selected .flow-card) {
  border-color: color-mix(in srgb, var(--node-accent) 58%, var(--line));
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.84),
    0 0 0 3px var(--node-accent-soft),
    var(--shadow-subtle);
}

:deep(.vue-flow__controls) {
  overflow: hidden;
  border: 1px solid var(--line);
  border-radius: 14px;
  background: var(--panel-solid);
  box-shadow: var(--shadow-subtle);
}

:deep(.vue-flow__controls-button) {
  width: 30px;
  height: 30px;
  color: var(--text);
  border-bottom-color: var(--line-soft);
  background: var(--panel-solid);
}

:deep(.vue-flow__controls-button:hover) {
  background: var(--code-bg);
}

:deep(.vue-flow__controls-button svg) {
  max-width: 14px;
  max-height: 14px;
}

:deep(.vue-flow__handle) {
  opacity: 0;
  pointer-events: none;
}

:deep(.vue-flow__edge-path) {
  stroke-width: 2.4;
  filter: drop-shadow(0 6px 10px rgba(24, 24, 27, 0.08));
}

:deep(.flow-edge-primary .vue-flow__edge-path) {
  stroke: var(--blue);
}

:deep(.flow-edge-success .vue-flow__edge-path) {
  stroke: var(--green);
}

:deep(.flow-edge-muted .vue-flow__edge-path) {
  stroke: var(--faint);
  stroke-dasharray: 6 7;
}

:deep(.vue-flow__edge.animated .vue-flow__edge-path) {
  stroke-dasharray: 8 8;
  animation: flow-dash 0.9s linear infinite;
}

:deep(.vue-flow__edge-text) {
  fill: var(--muted);
  font-family: inherit;
  letter-spacing: 0.02em;
}

:deep(.vue-flow__edge-textbg) {
  fill: var(--panel-solid);
  stroke: var(--line);
  stroke-width: 1;
}

.flow-card {
  --node-accent: var(--blue);
  --node-accent-soft: rgba(37, 99, 235, 0.13);
  position: relative;
  min-width: 0;
  padding: 12px;
  border: 1px solid color-mix(in srgb, var(--node-accent) 24%, var(--line));
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.78);
  box-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.8),
    var(--shadow-subtle);
  backdrop-filter: blur(10px);
}

.flow-card-frpc.status-running,
.flow-card-frpc.status-reloading {
  --node-accent: var(--green);
  --node-accent-soft: rgba(16, 185, 129, 0.14);
}

.flow-card-frpc.status-starting,
.flow-card-frpc.status-config_dirty {
  --node-accent: var(--amber);
  --node-accent-soft: rgba(245, 158, 11, 0.16);
}

.flow-card-frpc.status-error {
  --node-accent: var(--red);
  --node-accent-soft: rgba(239, 68, 68, 0.13);
}

.flow-card-frps {
  --node-accent: var(--red);
  --node-accent-soft: rgba(239, 68, 68, 0.13);
}

.flow-card-public-entry {
  --node-accent: var(--green);
  --node-accent-soft: rgba(16, 185, 129, 0.13);
}

.flow-card-more,
.flow-card-empty {
  --node-accent: var(--faint);
  --node-accent-soft: rgba(161, 161, 170, 0.12);
}

.flow-card-top {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  gap: 10px;
  align-items: start;
}

.flow-card-drag {
  cursor: grab;
  user-select: none;
}

.flow-avatar {
  display: grid;
  place-items: center;
  width: 34px;
  height: 34px;
  color: #ffffff;
  border-radius: 12px;
  background: linear-gradient(135deg, var(--node-accent), #18181b);
  box-shadow: 0 12px 24px var(--node-accent-soft);
}

.avatar-frpc {
  background: linear-gradient(135deg, #4a90e2, #2e5c8a);
}

.avatar-frps {
  background: linear-gradient(135deg, #e24a4a, #8a2e2e);
}

.avatar-local-service {
  background: linear-gradient(135deg, #2563eb, #38bdf8);
}

.avatar-public-entry {
  background: linear-gradient(135deg, #10b981, #047857);
}

.avatar-more,
.avatar-empty {
  background: linear-gradient(135deg, #71717a, #27272a);
}

.flow-card-copy {
  min-width: 0;
}

.flow-card h3 {
  margin: 0;
  overflow: hidden;
  color: var(--text);
  font-size: 14px;
  line-height: 1.2;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: help;
  transition: opacity 0.15s ease;
}

.flow-card h3:hover {
  opacity: 0.8;
}

/* 服务端入口显示完整地址：允许换行而不是截断 */
.flow-card-frps h3 {
  white-space: normal;
  word-break: break-all;
}

.flow-label {
  display: inline-flex;
  width: fit-content;
  margin: 0 0 3px;
  padding: 2px 7px;
  color: var(--node-accent);
  border: 1px solid color-mix(in srgb, var(--node-accent) 26%, transparent);
  border-radius: 999px;
  background: var(--node-accent-soft);
  font-size: 11px;
  font-weight: 760;
  line-height: 1.35;
}

.flow-card-copy span {
  display: block;
  margin-top: 3px;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.36;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: help;
  transition: opacity 0.15s ease;
}

.flow-card-copy span:hover {
  opacity: 0.8;
}

.status-chip {
  display: inline-flex;
  margin-top: 12px;
  padding: 5px 10px;
  color: var(--node-accent);
  border: 1px solid color-mix(in srgb, var(--node-accent) 30%, transparent);
  border-radius: 999px;
  background: var(--node-accent-soft);
  font-size: 12px;
  font-weight: 700;
}

.status-chip.compact {
  margin-top: 0;
  padding: 4px 8px;
  font-size: 11px;
}

.fleet-live {
  margin-left: 7px;
  padding: 2px 7px;
  border-radius: 999px;
  background: rgba(16, 185, 129, 0.14);
  color: #047857;
  font-size: 11px;
  font-style: normal;
  font-weight: 760;
}

.fleet-live.off {
  background: rgba(161, 161, 170, 0.16);
  color: var(--muted);
}

.flow-live {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 9px;
  padding: 5px 9px;
  border: 1px solid transparent;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 700;
}

.flow-live-dot {
  flex: 0 0 auto;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: currentColor;
}

.flow-live-text {
  flex: 0 0 auto;
}

.flow-live-err {
  flex: 1 1 auto;
  overflow: hidden;
  font-weight: 600;
  opacity: 0.82;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: help;
  transition: opacity 0.15s ease;
}

.flow-live-err:hover {
  opacity: 1;
}

.flow-live-ok {
  color: #047857;
  border-color: rgba(16, 185, 129, 0.26);
  background: rgba(16, 185, 129, 0.12);
}

.flow-live-ok .flow-live-dot {
  animation: live-pulse 2s ease-out infinite;
}

.flow-live-error {
  color: #b91c1c;
  border-color: rgba(239, 68, 68, 0.3);
  background: rgba(239, 68, 68, 0.12);
}

.flow-live-warn {
  color: #b45309;
  border-color: rgba(245, 158, 11, 0.32);
  background: rgba(245, 158, 11, 0.13);
}

.flow-live-muted {
  color: var(--muted);
  border-color: rgba(161, 161, 170, 0.28);
  background: rgba(161, 161, 170, 0.12);
}

@keyframes live-pulse {
  0% {
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.45);
  }
  70% {
    box-shadow: 0 0 0 5px rgba(16, 185, 129, 0);
  }
  100% {
    box-shadow: 0 0 0 0 rgba(16, 185, 129, 0);
  }
}

@keyframes flow-dash {
  from {
    stroke-dashoffset: 16;
  }
  to {
    stroke-dashoffset: 0;
  }
}

html[data-theme="dark"] .topology-flow-shell {
  background:
    linear-gradient(to right, rgba(255, 255, 255, 0.07) 1px, transparent 1px),
    linear-gradient(to bottom, rgba(255, 255, 255, 0.07) 1px, transparent 1px),
    rgba(24, 24, 27, 0.48);
}

html[data-theme="dark"] .flow-card {
  background: rgba(24, 24, 27, 0.76);
}

html[data-theme="dark"] .flow-live-ok,
html[data-theme="dark"] .fleet-live:not(.off) {
  color: #34d399;
}

html[data-theme="dark"] .flow-live-error {
  color: #f87171;
}

html[data-theme="dark"] .flow-live-warn {
  color: #fbbf24;
}

@media (max-width: 720px) {
  .topology-canvas {
    padding: 16px;
  }

  .topology-overview-head {
    display: grid;
  }

  .server-picker {
    width: 100%;
  }
}
</style>
