# 接管功能安全改进

## 问题背景

在实际使用中发现，`adopt` 功能在 restart 模式下可能导致问题：

1. **systemd 冲突**：杀掉 systemd 管理的 frpc 进程后，systemd 可能立即重启它，导致与面板进程冲突
2. **无 Admin API**：如果原进程配置中未启用 admin API，面板重启后无法完全控制进程
3. **警告不足**：操作前没有充分检测和警告用户风险

## 改进内容

### 1. 进程检测增强

**新增检测能力**：

- ✅ **systemd 管理检测**
  - 检查 `/proc/{pid}/cgroup` 是否包含 systemd
  - 提取 systemd 服务单元名称（如 `frpc.service`）
  - 回退到 `systemctl status {pid}` 命令

- ✅ **Admin API 配置检测**
  - 解析配置文件检查是否配置了 `webServer` (TOML) 或 `admin_addr/admin_port` (INI)
  - 提取 Admin API 地址（如 `127.0.0.1:7400`）

**数据模型**：

```go
type FRPCProcessCandidate struct {
    PID             int    `json:"pid"`
    Exe             string `json:"exe"`
    ConfigPath      string `json:"configPath"`
    Managed         bool   `json:"managed"`
    ServerID        string `json:"serverId,omitempty"`
    SystemdManaged  bool   `json:"systemdManaged"`         // 新增
    SystemdUnit     string `json:"systemdUnit,omitempty"`  // 新增
    HasAdminAPI     bool   `json:"hasAdminApi"`            // 新增
    AdminAPIAddress string `json:"adminApiAddress,omitempty"` // 新增
}
```

### 2. 后端警告机制

**AdoptProcess 改进**：

- restart 模式前检查 systemd 管理状态和 admin API 配置
- 在返回消息中包含明确的警告和操作指引
- 提示用户先停用 systemd 服务：`sudo systemctl disable --now frpc`

**示例警告信息**：

```
⚠️ 该进程由 systemd 托管（frpc.service）。停止后 systemd 可能立即重启它，导致与面板冲突。
建议先手动停用服务：sudo systemctl disable --now frpc.service

⚠️ 配置文件中未启用 admin API（webServer），面板重启后可能无法完全控制进程。
面板会自动添加 admin API 配置。
```

### 3. 前端体验优化

**进程列表显示**：

- 🔖 **systemd 标签**：显示黄色警告标签，提示由 systemd 托管
- 🔖 **Admin API 标签**：
  - 绿色：已配置 Admin API
  - 灰色：未配置 Admin API

**确认对话框增强**：

- 检测到 systemd 管理时，自动显示详细警告
- 检测到无 Admin API 时，提示将自动添加配置
- 提供具体的 systemctl 命令供用户复制

### 4. 推荐操作流程

#### 对于 systemd 管理的进程

**推荐流程**：
1. 先停用 systemd 服务：`sudo systemctl disable --now frpc`
2. 确认进程已停止：`systemctl status frpc`
3. 使用「导入配置」功能（而非 adopt）
4. 由面板启动新进程

#### 对于手动启动的进程

- **attach 模式**：适用于临时观察，不重启进程
- **restart 模式**：停止并完全接管，适合长期管理

## 文件变更

### 后端

- `internal/app/models.go` - 扩展 `FRPCProcessCandidate` 结构
- `internal/frpc/discover.go` - 添加 `isSystemdManaged()` 和 `checkAdminAPI()` 函数
- `internal/app/frpcimport.go` - 改进 `AdoptProcess()` 的警告逻辑

### 前端

- `web/src/api/client.ts` - 更新 `FrpcProcessCandidate` 接口
- `web/src/pages/ServersPage.vue` - 改进确认对话框和进程列表显示

## 兼容性

- ✅ 向后兼容：新字段为可选，旧数据不受影响
- ✅ 跨平台：systemd 检测仅在 Linux 上启用，其他平台返回 false
- ✅ 降级友好：检测失败不影响主流程，仅缺少警告信息

## 测试验证

- ✅ 所有单元测试通过（包括 TestAdoptProcessRestart）
- ✅ 编译通过，无类型错误
- ✅ systemd 检测逻辑已测试（/proc/cgroup 和 systemctl 两种方式）
- ✅ Admin API 解析支持 TOML 和 INI 两种格式

## 后续建议

1. 【可选】添加一键停用 systemd 服务功能（需要 sudo 权限，安全风险较大）
2. 【可选】在纳管前显示配置 diff，让用户确认变更
3. 【可选】支持批量纳管多个进程
