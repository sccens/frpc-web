# frpc-web 重构计划 v2.0

> **状态:已实施(v2.0.0)**。本计划已落地,frpc-web 重定位为「只读配置监控面板」。
> 一处与本文档最初的表述不同:**面板内可直接编辑配置文件原文**(非纯只读),
> 配置可写部署见 README 的 `ALLOW_CONFIG_EDIT`,迁移与破坏性变更见 CHANGELOG v2.0.0。
> 下方原文保留作为决策记录。

## 项目定位变更

### 当前版本 (v1.5.x)
```
frpc-web = 进程管理器 + 配置管理器 + 监控面板
- 管理 frpc 进程（启动/停止/重启）
- 管理配置文件
- 监控状态
```

### 目标版本 (v2.0)
```
frpc-web = 只读监控面板 + 拓扑可视化
- 不管理进程（由 systemd/supervisor 管理）
- 只读显示配置和状态
- 可视化拓扑图
- 通过 frpc admin API 热重载配置
```

---

## 核心理念

### 职责分离

```
┌─────────────────────────────────────┐
│      systemd (进程生命周期)         │
│  - start/stop/restart               │
│  - 开机自启                          │
│  - 崩溃重启                          │
└─────────────────────────────────────┘
              ↓
┌─────────────────────────────────────┐
│   frpc 进程 + admin API             │
│  - 运行代理服务                      │
│  - 提供 /api/reload 热重载接口      │
│  - 提供 /api/status 状态接口        │
└─────────────────────────────────────┘
              ↓
┌─────────────────────────────────────┐
│    frpc-web (只读监控)              │
│  - 读取配置文件显示服务器和规则     │
│  - 通过 admin API 获取实时状态      │
│  - 生成拓扑图                        │
│  - 调用 /api/reload 热重载配置      │
└─────────────────────────────────────┘
```

---

## 保留和删除的功能

### ✅ 保留功能

#### 1. 拓扑页面
- 可视化显示所有服务器节点
- 显示代理规则连接关系
- 实时状态监控（运行/停止）
- 节点健康状态

#### 2. 服务器页面（只读版）
- **只读显示**服务器节点列表
- **只读显示**代理规则列表
- 状态显示（运行时间、代理数量等）
- 查看日志（只读）

#### 3. 设置页面（精简版）
保留：
- 🔑 密钥更改（frpc-web 自身认证）
- 💾 备份 frpc 配置（导出配置文件）
- 🔄 系统更新（frpc-web 自身更新）

删除：
- ❌ GitHub 代理设置
- ❌ 自动备份设置
- ❌ frpc 版本管理（在线安装/离线安装）
- ❌ 审计日志

#### 4. 登录页面
- 保持不变

### ❌ 删除功能

#### 1. 总览页面
- 完全删除 `DashboardPage.vue`
- 删除相关 API 和后端逻辑

#### 2. 服务器管理功能
- ❌ "添加服务器"按钮
- ❌ "启动"按钮
- ❌ "停止"按钮
- ❌ "重启"按钮
- ❌ "重载配置"按钮（替换为"请求热重载"）
- ❌ "删除服务器"按钮
- ❌ "编辑服务器"功能
- ❌ "接管已有 frpc"功能

#### 3. 规则管理功能
- ❌ "新增规则"按钮
- ❌ "编辑规则"功能
- ❌ "删除规则"按钮
- ❌ 启用/禁用规则开关

#### 4. frpc 安装功能
- ❌ 在线安装
- ❌ 离线安装
- ❌ 版本切换
- ❌ 二进制发现

---

## 方案 C：通过 frpc admin API 热重载

### 原理

frpc 自带 admin API，提供热重载接口：

```toml
# frpc.toml 配置
webServer.addr = "127.0.0.1"
webServer.port = 7400
webServer.user = "admin"
webServer.password = "your-password"
```

### 热重载 API

```bash
# frpc 热重载接口
curl -u admin:password http://127.0.0.1:7400/api/reload
```

响应：
```json
{
  "status": "ok",
  "message": "reloaded successfully"
}
```

### frpc-web 实现

#### 后端接口

```go
// internal/app/service.go
func (s *Service) ReloadViaAdminAPI(ctx context.Context, serverID string) ActionResult {
    server, err := s.store.GetServer(ctx, serverID)
    if err != nil {
        return errorResult(err)
    }
    
    // 调用 frpc admin API
    url := fmt.Sprintf("http://%s:%d/api/reload", 
        server.AdminAddr, server.AdminPort)
    
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    if server.AdminUser != "" {
        req.SetBasicAuth(server.AdminUser, server.AdminPassword)
    }
    
    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return ActionResult{OK: false, Message: err.Error()}
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return ActionResult{OK: false, Message: "reload failed"}
    }
    
    return ActionResult{OK: true, Message: "配置已热重载"}
}
```

#### 前端按钮

```vue
<!-- ServersPage.vue -->
<button 
  class="control-button warning" 
  @click="reloadConfig(server)"
>
  <RefreshCw :size="15" />
  热重载配置
</button>
```

---

## 配置文件监控

### 配置文件扫描

frpc-web 定期扫描配置文件，解析服务器和规则信息。

#### 扫描路径（优先级）

```
1. /etc/frpc/frpc.toml
2. /usr/local/etc/frpc/frpc.toml  
3. ~/.frpc-web/data/frpc.toml
4. 用户自定义路径
```

#### 实现

```go
// internal/app/configwatch.go
type ConfigFileScanner struct {
    paths []string
}

func (s *ConfigFileScanner) ScanAll(ctx context.Context) ([]Server, error) {
    var servers []Server
    
    for _, path := range s.paths {
        if !fileExists(path) {
            continue
        }
        
        // 解析 TOML 配置
        config, err := parseFrpcConfig(path)
        if err != nil {
            continue
        }
        
        // 转换为 Server 对象
        server := &Server{
            ID:            generateID(path),
            Name:          filepath.Base(path),
            ServerAddr:    config.ServerAddr,
            ServerPort:    config.ServerPort,
            AdminAddr:     config.WebServer.Addr,
            AdminPort:     config.WebServer.Port,
            AdminUser:     config.WebServer.User,
            AdminPassword: config.WebServer.Password,
            ConfigPath:    path,
            ReadOnly:      true, // 标记为只读
        }
        
        // 通过 admin API 获取运行状态
        server.Status = s.fetchStatus(ctx, server)
        
        // 解析代理规则
        server.Rules = parseProxyRules(config)
        
        servers = append(servers, *server)
    }
    
    return servers, nil
}
```

#### 定期刷新

```go
// 每 30 秒刷新一次
ticker := time.NewTicker(30 * time.Second)
go func() {
    for range ticker.C {
        servers, _ := scanner.ScanAll(ctx)
        for _, server := range servers {
            _ = store.UpsertServer(ctx, server)
        }
    }
}()
```

---

## 实施步骤

### Phase 1: 后端改造

#### 1.1 新增配置文件扫描器

文件：`internal/app/configwatch.go`

- [ ] 实现 `ConfigFileScanner`
- [ ] 支持多路径扫描
- [ ] 解析 TOML 配置
- [ ] 转换为 Server 和 Rule 对象

#### 1.2 实现热重载接口

文件：`internal/app/service.go`

- [ ] 添加 `ReloadViaAdminAPI()` 方法
- [ ] 调用 frpc admin API `/api/reload`
- [ ] 处理认证（Basic Auth）

#### 1.3 禁用控制操作

文件：`internal/app/service.go`

- [ ] `Start()` → 返回 "不支持" 错误
- [ ] `Stop()` → 返回 "不支持" 错误
- [ ] `Restart()` → 返回 "不支持" 错误
- [ ] `CreateServer()` → 返回 "不支持" 错误
- [ ] `UpdateServer()` → 返回 "不支持" 错误
- [ ] `DeleteServer()` → 返回 "不支持" 错误
- [ ] `CreateRule()` → 返回 "不支持" 错误
- [ ] `UpdateRule()` → 返回 "不支持" 错误
- [ ] `DeleteRule()` → 返回 "不支持" 错误

#### 1.4 保留只读操作

- [x] `Servers()` - 列出所有服务器
- [x] `Rules()` - 列出所有规则
- [x] `Logs()` - 查看日志
- [x] `ProxyStatus()` - 查看代理状态

#### 1.5 API 路由改造

文件：`internal/server/server.go`

```go
// 删除或禁用这些路由：
- POST /api/servers
- PUT /api/servers/{id}
- DELETE /api/servers/{id}
- POST /api/servers/{id}/start
- POST /api/servers/{id}/stop
- POST /api/servers/{id}/restart
- POST /api/servers/{id}/reload  // 改为 ReloadViaAdminAPI
- POST /api/servers/{id}/rules
- PUT /api/servers/{id}/rules/{ruleId}
- DELETE /api/servers/{id}/rules/{ruleId}
- POST /api/servers/adopt
- POST /api/versions/install-online
- POST /api/versions/install-offline

// 新增路由：
+ POST /api/servers/{id}/reload-via-admin  // 调用 frpc admin API
+ GET /api/config-files                    // 列出扫描到的配置文件
```

### Phase 2: 前端改造

#### 2.1 删除页面

- [ ] 删除 `web/src/pages/DashboardPage.vue`

#### 2.2 修改路由

文件：`web/src/router/index.ts`

```typescript
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: LoginPage, meta: { public: true } },
    {
      path: '/',
      component: AppLayout,
      redirect: '/topology',  // 默认跳转到拓扑页面
      children: [
        { path: 'topology', name: 'topology', component: TopologyPage },
        { path: 'servers', name: 'servers', component: ServersPage },
        { path: 'settings', name: 'settings', component: SettingsPage },
      ],
    },
  ],
})
```

#### 2.3 精简 ServersPage.vue

移除元素：
- [ ] "添加服务器"按钮
- [ ] "启动"按钮
- [ ] "停止"按钮
- [ ] "重启"按钮
- [ ] "编辑服务器"按钮
- [ ] "删除服务器"按钮
- [ ] "新增规则"按钮
- [ ] "编辑规则"按钮
- [ ] "删除规则"按钮
- [ ] 规则启用/禁用开关

保留元素：
- [x] 服务器列表（只读）
- [x] 代理规则列表（只读）
- [x] 状态显示
- [x] 查看日志按钮

新增元素：
- [ ] "热重载配置"按钮（调用 frpc admin API）

#### 2.4 精简 SettingsPage.vue

保留：
- [x] 密钥更改
- [x] 备份配置
- [x] 系统更新

删除：
- [ ] GitHub 代理设置
- [ ] 自动备份设置
- [ ] frpc 版本管理卡片
- [ ] 在线安装
- [ ] 离线安装
- [ ] 审计日志

#### 2.5 修改导航栏

文件：`web/src/layouts/AppLayout.vue`

```vue
<nav>
  <NavItem to="/topology" icon="Network">拓扑图</NavItem>
  <NavItem to="/servers" icon="Server">服务器</NavItem>
  <NavItem to="/settings" icon="Settings">设置</NavItem>
</nav>
```

删除：
- [ ] "总览"导航项
- [ ] "日志"导航项
- [ ] "审计"导航项

### Phase 3: 数据层改造

#### 3.1 数据模型变更

文件：`internal/app/models.go`

```go
type Server struct {
    // ... 现有字段
    
    // 新增字段
    ConfigPath string `json:"configPath"` // 配置文件路径
    ReadOnly   bool   `json:"readOnly"`   // 是否只读
    External   bool   `json:"external"`   // 是否外部管理
}
```

#### 3.2 存储层适配

文件：`internal/storage/store.go`

- [ ] 支持 `ConfigPath` 字段
- [ ] 支持 `ReadOnly` 标记
- [ ] 禁用创建/更新/删除操作

### Phase 4: 文档更新

- [ ] 更新 README.md
- [ ] 更新 CHANGELOG.md
- [ ] 编写迁移指南 MIGRATION_v2.md
- [ ] 更新 API 文档

---

## 部署模式

### 典型部署流程

#### 1. 安装 frpc（systemd 管理）

```bash
# 下载 frpc
wget https://github.com/fatedier/frp/releases/download/v0.60.0/frp_0.60.0_linux_amd64.tar.gz
tar -xzf frp_0.60.0_linux_amd64.tar.gz
sudo mv frp_0.60.0_linux_amd64/frpc /usr/local/bin/

# 创建配置文件
sudo mkdir -p /etc/frpc
sudo vim /etc/frpc/frpc.toml
```

配置示例：
```toml
serverAddr = "frps.example.com"
serverPort = 7000
auth.method = "token"
auth.token = "your-token"

# 启用 admin API（必需）
webServer.addr = "127.0.0.1"
webServer.port = 7400
webServer.user = "admin"
webServer.password = "secure-password"

[[proxies]]
name = "ssh"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000
```

创建 systemd 服务：
```ini
# /etc/systemd/system/frpc.service
[Unit]
Description=frpc Client Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/frpc -c /etc/frpc/frpc.toml
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

启动服务：
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now frpc
sudo systemctl status frpc
```

#### 2. 安装 frpc-web（监控面板）

```bash
# 下载 frpc-web v2.0
wget https://github.com/sccens/frpc-web/releases/download/v2.0.0/frpc-web_linux_amd64
sudo mv frpc-web_linux_amd64 /usr/local/bin/frpc-web
sudo chmod +x /usr/local/bin/frpc-web

# 创建配置目录
sudo mkdir -p /etc/frpc-web

# 创建 systemd 服务
sudo vim /etc/systemd/system/frpc-web.service
```

```ini
[Unit]
Description=frpc Web Panel
After=network.target frpc.service

[Service]
Type=simple
User=frpc-web
ExecStart=/usr/local/bin/frpc-web --addr :8080
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

启动面板：
```bash
sudo useradd -r -s /bin/false frpc-web
sudo systemctl daemon-reload
sudo systemctl enable --now frpc-web
sudo systemctl status frpc-web
```

#### 3. 访问面板

```bash
# 打开浏览器
http://your-server:8080

# 登录后查看：
- 拓扑图：显示服务器和代理规则
- 服务器页面：查看运行状态和日志
```

---

## 配置文件管理

### 方式 1：手动编辑 + 热重载（推荐）

```bash
# 1. 编辑配置文件
sudo vim /etc/frpc/frpc.toml

# 2. 在 frpc-web 面板点击"热重载配置"
# 或通过 API：
curl -X POST http://localhost:8080/api/servers/{id}/reload-via-admin
```

### 方式 2：手动编辑 + systemd 重载

```bash
# 1. 编辑配置文件
sudo vim /etc/frpc/frpc.toml

# 2. 重载 frpc 服务
sudo systemctl reload frpc  # 热重载
# 或
sudo systemctl restart frpc  # 完全重启
```

### 方式 3：通过 frpc admin API

```bash
# 直接调用 frpc admin API
curl -u admin:password http://127.0.0.1:7400/api/reload
```

---

## 迁移指南

### 从 v1.5.x 升级到 v2.0

#### ⚠️ 重大变更

1. **不再管理 frpc 进程**
   - v1.5.x: frpc-web 启动和管理 frpc 进程
   - v2.0: 仅监控外部管理的 frpc 进程

2. **所有控制功能移除**
   - 启动/停止/重启按钮删除
   - 创建/编辑/删除服务器功能删除
   - 创建/编辑/删除规则功能删除

3. **数据不兼容**
   - v1.5.x 的 state.json 不能直接用于 v2.0
   - 需要重新扫描配置文件

#### 迁移步骤

##### 选项 A：全新安装（推荐）

```bash
# 1. 备份 v1.5.x 配置
cp ~/.frpc-web/data/state.json ~/state.json.backup

# 2. 停止 v1.5.x 管理的 frpc
# （假设 v1.5.x 有一个运行中的服务器）

# 3. 导出配置到标准位置
sudo mkdir -p /etc/frpc
# 手动复制配置或使用备份导出功能

# 4. 用 systemd 启动 frpc
sudo systemctl start frpc

# 5. 升级 frpc-web 到 v2.0
sudo systemctl stop frpc-web
sudo mv /usr/local/bin/frpc-web /usr/local/bin/frpc-web.v1.5.bak
sudo wget -O /usr/local/bin/frpc-web https://...v2.0.0...
sudo systemctl start frpc-web

# 6. 访问面板，配置会自动扫描
```

##### 选项 B：继续使用 v1.5.x

如果你需要进程管理功能，**不要升级**到 v2.0。

---

## 向后兼容性

### API 兼容性

所有删除的 API 返回明确错误：

```json
{
  "error": "此功能在 v2.0 中已移除。frpc-web v2.0 仅用于监控，不支持进程管理。",
  "hint": "请使用 systemctl 管理 frpc 进程，或降级到 v1.5.x"
}
```

### 数据兼容性

- ❌ v1.5.x 的 state.json **不兼容**
- ✅ 可通过配置文件扫描重新导入

---

## 优势与劣势

### ✅ 优势

1. **更稳定**
   - systemd 管理进程更可靠
   - 开机自启、崩溃重启由系统保证
   - frpc-web 崩溃不影响 frpc 运行

2. **更安全**
   - 面板无权控制进程，降低风险
   - 权限分离：运维管理进程，开发查看状态

3. **更简单**
   - 代码量大幅减少
   - 维护成本降低
   - 不再需要处理进程管理的复杂逻辑

4. **职责单一**
   - 面板专注于展示和监控
   - 符合 Unix 哲学：一个工具做好一件事

### ❌ 劣势

1. **功能减少**
   - 不能在面板中启动/停止服务
   - 不能在面板中添加/删除服务器
   - 不能在面板中编辑规则

2. **部署复杂**
   - 需要单独配置 systemd
   - 需要手动管理配置文件

3. **热重载依赖 admin API**
   - 必须启用 frpc admin API
   - 需要配置认证信息

---

## 替代方案

如果你需要更强大的功能，考虑：

### 方案 1：保持 v1.5.x

继续使用当前版本，不升级到 v2.0。

### 方案 2：混合架构

- 生产环境：systemd 管理 + frpc-web v2.0 监控
- 开发环境：frpc-web v1.5.x 完全管理

### 方案 3：v2.0 + 配置管理工具

- frpc-web v2.0：监控和可视化
- Ansible/SaltStack：配置管理和部署

---

## 时间表

### 里程碑

- [ ] **M1**: 完成设计文档（当前）
- [ ] **M2**: 后端改造完成（预计 3 天）
- [ ] **M3**: 前端改造完成（预计 2 天）
- [ ] **M4**: 测试和修复（预计 2 天）
- [ ] **M5**: 文档更新（预计 1 天）
- [ ] **M6**: 发布 v2.0.0-beta1（预计 1 周后）
- [ ] **M7**: 用户反馈和修复（预计 2 周）
- [ ] **M8**: 发布 v2.0.0（预计 1 个月后）

---

## 问题与答案

### Q1: 为什么要做这个改造？

**A**: 职责分离。进程管理是操作系统的事，面板应该专注于监控和可视化。

### Q2: v1.5.x 还会维护吗？

**A**: 会。v1.5.x 作为 LTS 版本继续维护，修复 bug，但不再添加新功能。

### Q3: 能否在 v2.0 中保留部分管理功能？

**A**: 可以考虑保留"热重载配置"功能（通过 admin API），其他管理功能不保留。

### Q4: 如果 frpc 没有启用 admin API 怎么办？

**A**: frpc-web 仍然可以显示配置和拓扑，但无法获取实时状态和热重载。

### Q5: 多机部署怎么办？

**A**: v2.0 仍然是单机监控。多机监控考虑在 v2.1 实现。

---

## 反馈与讨论

如有问题或建议，请在 GitHub Issues 讨论：
https://github.com/sccens/frpc-web/issues

---

**文档版本**: v1.0  
**创建日期**: 2026-06-21  
**作者**: frpc-web team
