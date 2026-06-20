# 管理模式：区分完全托管和附着观察

## 问题背景

在 v1.5.3 之前，接管（adopt）功能有一个严重的权限混淆问题：

### 问题表现

```
用户选择 "attach 模式" 接管外部 frpc 进程
→ 期望：只能观察日志和状态
→ 实际：可以点击"停止/重启"按钮，面板会杀掉外部进程！

这导致：
1. 附着到 systemd 管理的进程后，用户以为只是"观察"
2. 点击"停止"按钮，面板直接 kill 进程
3. systemd 可能立即重启进程，导致冲突
4. 或者进程被停止，但用户以为面板无权控制
```

### 根本原因

**没有区分"附着观察"和"完全托管"两种不同的管理权限**：

- attach 模式：记录进程信息，设置状态为 "running"
- 但 Stop/Start/Reload 函数没有检查管理权限
- 结果：即使是 attach 模式，面板仍然可以控制进程

---

## 解决方案

### 核心设计：管理模式（Management Mode）

引入 `managementMode` 字段，明确区分管理权限：

```go
type Server struct {
    // ...
    // ManagementMode 表示面板对此服务器进程的管理权限：
    // - "managed": 完全托管，面板启动的进程，可以启动/停止/重启/重载
    // - "attached": 附着观察，外部进程，只能查看日志和状态，不能控制
    ManagementMode string `json:"managementMode"`
}
```

### 两种模式对比

| 特性 | managed（完全托管） | attached（附着观察） |
|------|---------------------|---------------------|
| 进程来源 | 面板启动 | 外部（systemd/手动） |
| 启动 | ✅ 可以 | ❌ 禁止 |
| 停止 | ✅ 可以 | ❌ 禁止 |
| 重启 | ✅ 可以 | ❌ 禁止 |
| 重载 | ✅ 可以 | ❌ 禁止 |
| 查看日志 | ✅ 可以 | ✅ 可以 |
| 查看状态 | ✅ 可以 | ✅ 可以 |
| 查看规则 | ✅ 可以 | ✅ 可以 |
| 编辑配置 | ✅ 可以 | ✅ 可以（但不生效） |

---

## 实现细节

### 1. 数据模型

#### Server 结构添加字段

```go
type Server struct {
    // ...
    ManagementMode string `json:"managementMode"`
}
```

#### 默认值

```go
// 创建服务器时默认为 "managed"
server := app.Server{
    // ...
    ManagementMode: "managed",
}
```

### 2. 接管（Adopt）流程

#### Attach 模式

```go
if mode == "attach" {
    // 设置为附着观察模式
    _ = s.store.SetServerManagementMode(ctx, server.ID, "attached")
    
    // 记录进程信息
    _ = s.store.UpsertProcess(ctx, ProcessInfo{
        ServerID: server.ID,
        PID:      input.PID,
        // ...
    })
    
    // 附着到进程（只观察）
    s.runtime.Adopt(server.ID, input.PID)
    
    return AdoptResult{
        Message: "✅ 已附着到运行中的 frpc 进程（只读观察模式）。\n\n⚠️ 面板无法控制此进程的启停操作，只能查看日志和状态。如需完全控制，请使用 restart 模式。",
    }, nil
}
```

#### Restart 模式

```go
// 停止外部进程
stopResult := s.runtime.Stop(ctx, server, ProcessInfo{
    ServerID: server.ID,
    PID:      input.PID,
})

// 设置为完全托管
_ = s.store.SetServerManagementMode(ctx, server.ID, "managed")

// 由面板启动新进程
startResult := s.Start(ctx, server.ID)
```

### 3. 操作权限检查

#### Start 函数

```go
func (s *Service) start(ctx context.Context, serverID string, ...) ActionResult {
    server, err := s.store.GetServer(ctx, serverID)
    if err != nil {
        return errorResult(err)
    }
    
    // 检查管理模式
    if server.ManagementMode == "attached" {
        return ActionResult{
            OK:      false,
            Message: "此服务器为附着观察模式，面板无法控制其启停。如需控制，请先使用 restart 模式重新接管。",
        }
    }
    
    // ... 正常启动逻辑
}
```

#### Stop 函数

```go
func (s *Service) stopInner(ctx context.Context, serverID string) ActionResult {
    server, err := s.store.GetServer(ctx, serverID)
    if err != nil {
        return errorResult(err)
    }
    
    // 检查管理模式
    if server.ManagementMode == "attached" {
        return ActionResult{
            OK:      false,
            Message: "此服务器为附着观察模式，面板无法控制其启停。如需控制，请先使用 restart 模式重新接管。",
        }
    }
    
    // ... 正常停止逻辑
}
```

#### Reload 函数

```go
func (s *Service) Reload(ctx context.Context, serverID string) ActionResult {
    // ... 获取 server
    
    // 检查管理模式
    if server.ManagementMode == "attached" {
        return ActionResult{
            OK:      false,
            Message: "此服务器为附着观察模式，面板无法控制其重载。如需控制，请先使用 restart 模式重新接管。",
        }
    }
    
    // ... 正常重载逻辑
}
```

### 4. 前端 UI

#### 类型定义

```typescript
export interface Server {
  // ...
  managementMode?: string // "managed" | "attached"
}
```

#### 视觉标识

在服务器卡片标题旁显示标签：

```vue
<div class="server-title-row">
  <StatusBadge :status="server.status" />
  <h3>{{ server.name }}</h3>
  <span 
    v-if="server.managementMode === 'attached'" 
    class="mode-chip attached-chip" 
    title="只读观察模式：面板无法控制此进程的启停"
  >
    👁️ 只读
  </span>
</div>
```

#### 样式

```css
.mode-chip {
  display: inline-flex;
  align-items: center;
  padding: 3px 10px;
  font-size: 12px;
  font-weight: 500;
  border-radius: 12px;
  white-space: nowrap;
}

.attached-chip {
  background: rgba(250, 173, 20, 0.12);
  color: #d87607;
  border: 1px solid rgba(250, 173, 20, 0.3);
}
```

---

## 使用场景

### 场景 1：观察原有的 systemd 管理的 frpc

```bash
# 原有进程由 systemd 管理
sudo systemctl status frpc
# 状态：active (running)

# 在面板中：
1. 点击"接管已有 frpc"
2. 扫描发现运行中的进程
3. 选择 "attach 模式"
4. 确认接管

# 结果：
- 面板显示 "👁️ 只读" 标签
- 可以查看日志和状态
- 点击"停止/重启"按钮会提示权限不足
- systemd 继续管理进程
```

### 场景 2：完全接管外部 frpc

```bash
# 原有进程由 systemd 管理
sudo systemctl disable --now frpc

# 在面板中：
1. 点击"接管已有 frpc"
2. 扫描发现进程（或配置文件）
3. 选择 "restart 模式"
4. 确认接管

# 结果：
- 原进程被停止
- 面板启动新进程（完全托管）
- 无 "只读" 标签
- 可以自由启停/重启/重载
```

### 场景 3：从 attached 转为 managed

```bash
# 当前是 attached 模式，想要完全控制

# 方法 1：手动停止外部进程后重启
1. SSH 登录服务器
2. sudo systemctl stop frpc  # 或 kill 进程
3. 在面板点击"启动"
   → 提示：附着模式不能启动
   
# 方法 2：重新接管（推荐）
1. 点击"接管已有 frpc"
2. 选择 "restart 模式"
3. 确认接管
   → 自动停止外部进程并由面板接管
```

---

## 边界情况

### 1. attached 模式的进程退出

```go
// runtime 的 onExit 处理器
func (s *Service) handleProcessExit(serverID string, err error) {
    server, _ := s.Server(ctx, serverID)
    
    if server.ManagementMode == "attached" {
        // attached 模式：不自动重启
        _ = s.store.SetServerStatus(ctx, serverID, "stopped")
        _ = s.store.AddHealth(ctx, serverID, "warning", 
            "附着的外部进程已退出。如需面板自动重启，请使用 restart 模式重新接管。")
        return
    }
    
    // managed 模式：自动重启（如果启用）
    if server.AutoRestart {
        s.scheduleRestart(ctx, serverID, err)
    }
}
```

### 2. 编辑 attached 服务器的配置

- ✅ 允许在面板中编辑配置
- ⚠️ 但配置不会生效（因为无法重启/重载）
- 💡 提示用户：配置已保存，但需要外部重启进程才能生效

### 3. 删除 attached 服务器

- ✅ 允许删除
- ⚠️ 只删除面板记录，不停止外部进程
- 💡 外部进程继续运行（由 systemd 或其他方式管理）

---

## 兼容性

### 向后兼容

- ✅ 已有服务器（没有 `managementMode` 字段）
  - 自动默认为 `"managed"`
  - 保持原有行为不变

### 数据迁移

- ✅ 不需要迁移脚本
- ✅ 字段为空时默认 `"managed"`
- ✅ 现有功能不受影响

---

## 测试验证

### 单元测试

```bash
go test ./internal/app -run TestAdopt -v
# ✅ TestAdoptProcessRestart - PASS
# ✅ TestAdoptProcessMissingPID - PASS
```

### 集成测试

#### attached 模式

```bash
# 1. 接管为 attached
POST /api/servers/adopt
{
  "pid": 12345,
  "mode": "attach"
}
# → managementMode: "attached"

# 2. 尝试停止
POST /api/servers/{id}/stop
# → 400 Bad Request
# → "此服务器为附着观察模式，面板无法控制其启停"

# 3. 查看日志（允许）
GET /api/servers/{id}/logs
# → 200 OK
```

#### managed 模式

```bash
# 1. 正常创建服务器
POST /api/servers
# → managementMode: "managed"

# 2. 停止（允许）
POST /api/servers/{id}/stop
# → 200 OK

# 3. 启动（允许）
POST /api/servers/{id}/start
# → 200 OK
```

---

## 文件变更

- `internal/app/models.go` - 添加 `ManagementMode` 字段
- `internal/app/service.go` - 添加 `SetServerManagementMode` 方法
- `internal/app/service.go` - 修改 `Start/Stop/Reload` 添加权限检查
- `internal/app/frpcimport.go` - 接管时设置管理模式
- `internal/storage/store.go` - 实现 `SetServerManagementMode`
- `internal/storage/store.go` - 创建服务器时设置默认值
- `web/src/api/client.ts` - 添加 `managementMode` 字段
- `web/src/components/ServerTable.vue` - 显示管理模式标签
- `web/src/styles/main.css` - 添加标签样式

---

## 后续改进建议

### 1. 前端按钮状态

```vue
<!-- 根据管理模式禁用按钮 -->
<button 
  :disabled="server.managementMode === 'attached'"
  @click="stop(server)"
>
  停止
</button>
```

### 2. 批量操作过滤

```typescript
// 批量启动时自动跳过 attached 服务器
function batchStart(servers: Server[]) {
  const managed = servers.filter(s => s.managementMode !== 'attached')
  // 只对 managed 服务器执行操作
}
```

### 3. 转换功能

```
添加"转为完全托管"按钮：
- 点击后自动执行 restart 模式接管
- 一键从 attached → managed
```

---

## 总结

这个改进解决了一个**严重的权限混淆问题**：

**修复前**：
- attach 模式名义上是"观察"
- 实际可以停止/重启外部进程
- 用户困惑、systemd 冲突

**修复后**：
- 明确的管理模式标识
- 严格的权限检查
- 清晰的用户提示
- 完全托管和附着观察职责分明

这确保了：
1. ✅ systemd 管理的进程不会被误操作
2. ✅ 用户清楚知道哪些操作被允许
3. ✅ 面板和外部管理工具不冲突
