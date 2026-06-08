# FRPC 配置管理器需求与技术方案报告

日期：2026-06-08

## 1. 项目背景

FRP 是常用的内网穿透工具，frpc 作为客户端通常需要用户手动下载二进制文件、编写 `frpc.toml`、执行命令启动、查看终端日志。对非运维用户来说，这些步骤门槛较高，也容易因为配置格式、端口冲突、进程残留、版本不一致等问题导致服务不可用。

本项目目标是开发一个浏览器 Web 管理器，让用户不接触命令行和配置文件，也能完成 frpc 的安装、配置、启动、停止、热重载、日志查看、版本升级和安全认证等完整管理流程。

## 2. 建设目标

1. 提供完整的 frpc 生命周期管理能力：安装、检测、升级、配置、启动、停止、重载、查看状态、查看日志。
2. 支持同时管理多个 FRP 服务器连接，每个连接独立配置、独立进程、独立日志。
3. 将 TCP、UDP、HTTP、HTTPS 代理规则表单化，自动生成符合 frpc 的 TOML 配置。
4. 优先使用 frpc 官方热重载能力更新代理规则，减少中断，不再把“杀进程重启”作为默认修改方式。
5. 增加安全认证和多用户能力，允许在公网映射 Web 管理页时具备基础防护。
6. 支持在线和离线两种 frpc 安装方式，适配网络受限场景。
7. 提供配置版本、差异对比、失败回滚和备份迁移能力，降低误操作风险。
8. 提供配置模板、现有配置导入、健康检查和异常告警能力，让长期运行更可靠。

## 3. 用户角色

| 角色 | 说明 | 主要操作 |
| --- | --- | --- |
| 管理员 | 系统拥有者，拥有全部权限 | 用户管理、全局设置、frpc 安装升级、所有服务器管理 |
| 运维用户 | 负责维护代理配置 | 添加服务器、编辑代理规则、启停服务、查看日志 |
| 只读用户 | 只需要查看运行情况 | 查看服务器状态、查看日志、查看配置摘要 |

MVP 阶段可以先实现单管理员模式，但数据库和接口设计应预留多用户、角色和审计字段，避免后期大改。

## 4. 功能需求

### 4.1 多服务器管理

系统需要支持用户创建多个 FRP 服务器连接配置。每个服务器配置对应一个独立的 `frpc.toml`、一个独立 frpc 进程和一份独立日志。

主要能力：

1. 添加服务器：填写名称、服务器地址、服务器端口、认证方式、认证 token、传输协议、备注等。
2. 编辑服务器：允许修改服务器基础信息。若修改的是非代理公共配置，需要提示用户该变更可能需要重启 frpc。
3. 删除服务器：删除前检查进程是否运行，运行中需要先停止或二次确认。
4. 状态展示：展示运行中、已停止、启动中、异常、需要重启、配置待重载等状态。
5. 自动启动：服务器可标记为自动启动，管理器启动后自动拉起对应 frpc。
6. 分组与搜索：当服务器数量增加时，可按名称、标签、状态搜索和筛选。

建议状态字段：

| 状态 | 含义 |
| --- | --- |
| stopped | 未运行 |
| starting | 正在启动 |
| running | 正常运行 |
| reloading | 正在热重载 |
| error | 启动失败或进程异常退出 |
| config_dirty | 配置已保存但尚未应用 |
| restart_required | 修改了无法热重载的配置，需要重启 |

### 4.2 代理规则可视化配置

系统需要支持 TCP、UDP、HTTP、HTTPS 四种代理类型。用户通过表单填写参数，系统自动转换为 TOML。

通用字段：

| 字段 | 说明 |
| --- | --- |
| 规则名称 | 对应 frpc proxy name，同一服务器下必须唯一 |
| 代理类型 | tcp、udp、http、https |
| 本地地址 | 默认 `127.0.0.1` |
| 本地端口 | 本地服务端口 |
| 启用状态 | 可临时禁用某条规则 |
| 备注 | 用于管理展示，不一定写入 frpc 配置 |

TCP/UDP 特有字段：

| 字段 | 说明 |
| --- | --- |
| 远程端口 | frps 侧暴露端口，必须校验端口范围 |

HTTP/HTTPS 特有字段：

| 字段 | 说明 |
| --- | --- |
| 自定义域名 | `customDomains`，可填写一个或多个域名 |
| 子域名 | 可选，适用于 frps 配置了 subdomainHost 的场景 |
| 路径匹配 | 可选，HTTP 场景可扩展 |

生成示例：

```toml
serverAddr = "example.frps.com"
serverPort = 7000

auth.method = "token"
auth.token = "******"

webServer.addr = "127.0.0.1"
webServer.port = 17400

[[proxies]]
name = "ssh"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000

[[proxies]]
name = "web"
type = "http"
localIP = "127.0.0.1"
localPort = 8080
customDomains = ["app.example.com"]
```

配置保存前应做校验：

1. 同一服务器下规则名称不能重复。
2. 本地端口、远程端口必须在 1-65535。
3. TCP/UDP 必填远程端口。
4. HTTP/HTTPS 至少填写自定义域名或子域名。
5. 域名格式、端口格式需要前端和后端双重校验。
6. 禁用规则不写入最终 `frpc.toml`，但保留在数据库中。

### 4.3 进程管理

每个服务器配置对应一个 frpc 运行实例。管理器需要负责进程启动、停止、状态检测和异常恢复。

主要能力：

1. 一键启动：生成配置文件后执行 `frpc -c <config>`。
2. 一键停止：优先使用 frpc admin API 或正常信号停止；无法停止时再进入强制终止流程。
3. 一键重载：代理规则变更后执行 `frpc reload -c <config>`。
4. 一键重启：仅当公共配置变更、frpc 版本变更或热重载失败时使用。
5. 进程信息：展示 PID、启动时间、运行时长、frpc 版本、配置路径、日志路径。
6. 健康检查：定期检查进程是否存活，并通过 `frpc status -c <config>` 或本地 Admin API 获取代理状态。

热重载策略：

1. frpc 热重载要求启用本地 `webServer`，因此每个服务器配置需要分配一个仅监听 `127.0.0.1` 的本地管理端口。
2. 当用户只修改代理规则时，流程为：保存数据库 -> 渲染 TOML 到临时文件 -> 校验配置 -> 原子替换配置文件 -> 执行 `frpc reload -c <config>` -> 拉取状态 -> 写审计日志。
3. 当用户修改 `serverAddr`、`serverPort`、认证、传输协议、webServer 端口等公共配置时，官方热重载能力不能保证全部生效，应标记为 `restart_required` 并引导用户重启。
4. 热重载失败时应自动回滚到上一版配置文件，并在日志和界面中显示失败原因。

### 4.4 实时日志查看

系统需要为每个服务器维护独立日志，支持 Web 页面查看最近日志和实时刷新。

主要能力：

1. 每个服务器独立日志文件，例如 `logs/<serverId>/frpc.log`。
2. 日志页默认展示最近 200 行。
3. 支持自动刷新，可使用 WebSocket 或 SSE 推送。
4. 支持暂停滚动、清空视图、复制日志、按关键词过滤。
5. 后端需要做日志脱敏，避免 token、密码等敏感信息直接展示。
6. 需要日志轮转策略，避免长期运行后日志文件无限增长。

建议默认日志策略：

| 项目 | 建议值 |
| --- | --- |
| 页面默认行数 | 200 行 |
| 单文件最大大小 | 10 MB |
| 保留文件数 | 5 个 |
| 日志级别 | info |

### 4.5 frpc 版本管理

系统需要提供在线安装和离线安装。

在线安装：

1. 从 GitHub Releases 获取可用版本列表。
2. 根据当前操作系统和 CPU 架构自动选择安装包。
3. 下载压缩包后解压并提取 `frpc` 二进制。
4. 执行 `frpc --version` 验证安装结果。
5. 支持切换版本和回滚版本。

离线安装：

1. 用户在 Web 页面上传 frp 发布包或单独的 frpc 二进制文件。
2. 后端校验文件类型、大小和可执行权限。
3. 解压或保存到版本目录。
4. 执行版本检测并登记到数据库。

版本检测：

1. 显示当前安装版本。
2. 检测 GitHub Releases 最新版本。
3. 提示当前版本是否落后。
4. 对正在运行的实例，升级后不应立即强制重启，需要提示“新版本将在重启后生效”。

### 4.6 安全认证

由于 Web 管理页可能被 frp 映射到公网，必须默认具备认证和安全防护。认证方案建议采用 JWT 或 Session Cookie。

推荐方案：

1. 默认启用登录认证，禁止无密码访问。
2. 密码使用 Argon2id 或 bcrypt 哈希存储。
3. 登录成功后发放短期 Access Token 和长期 Refresh Token，或使用 HttpOnly Secure Cookie Session。
4. 管理接口必须校验权限，WebSocket/SSE 日志接口也必须鉴权。
5. 增加登录失败限速、IP 限速和账号锁定策略。
6. 增加 CSRF 防护，尤其是使用 Cookie Session 时。
7. 支持 HTTPS 部署，可内置自签证书或放在 Nginx/Caddy 后面。
8. 敏感字段加密落库，例如 frps token、管理页密钥、上传包临时路径。
9. 支持审计日志，记录用户、时间、IP、操作对象和结果。
10. 管理器自身默认只监听 `127.0.0.1`，如需公网访问，应由用户显式开启或通过反向代理暴露。
11. 可选支持 TOTP 二次验证，用于公网暴露或多人协作场景。
12. 支持 API Token，用于自动化脚本或第三方系统调用，并允许单独吊销。

权限建议：

| 权限 | 管理员 | 运维用户 | 只读用户 |
| --- | --- | --- | --- |
| 查看服务器 | 是 | 是 | 是 |
| 添加/编辑服务器 | 是 | 是 | 否 |
| 删除服务器 | 是 | 可选 | 否 |
| 启停/重载 | 是 | 是 | 否 |
| 查看日志 | 是 | 是 | 是 |
| 安装/升级 frpc | 是 | 否 | 否 |
| 用户管理 | 是 | 否 | 否 |

### 4.7 多用户模式

多用户模式用于多人共同维护 frpc 配置，重点是权限隔离和操作可追溯。

主要能力：

1. 用户管理：创建、禁用、重置密码。
2. 角色管理：管理员、运维、只读三类角色先满足大多数场景。
3. 资源权限：可按服务器维度授权用户。
4. 操作审计：记录配置修改、启动、停止、重载、安装、登录失败等事件。
5. 会话管理：管理员可以查看和撤销在线会话。

MVP 可先实现全局角色；后续再实现“某用户只能管理某几个服务器”的细粒度授权。

### 4.8 配置版本、预检查与回滚

frpc 配置一旦写错，可能导致代理全部不可用。因此系统需要把配置变更当成可追溯、可回滚的版本化操作。

主要能力：

1. 配置版本：每次保存服务器或代理规则后生成一条配置版本记录，记录变更人、变更时间、变更摘要和渲染后的 TOML 快照。
2. 差异对比：支持对比当前配置与上一版配置，展示新增、删除和修改的代理规则。
3. 启动前预检查：启动、重载前执行参数校验、端口冲突检查、配置渲染检查和 frpc 命令级校验。
4. 本地服务探测：可选检查本地 `localIP:localPort` 是否可连接，提前发现本地服务未启动的问题。
5. 原子写入：配置先写入临时文件，校验通过后再原子替换正式 `frpc.toml`。
6. 失败回滚：热重载失败时自动恢复上一版配置，并把服务器状态标记为 `error` 或 `config_dirty`。
7. 手动恢复：管理员可以从历史版本中选择任意一版恢复，并选择热重载或重启生效。

预检查建议项：

| 检查项 | 说明 |
| --- | --- |
| 规则名称唯一 | 同一服务器内 proxy name 不能重复 |
| 端口范围 | 本地端口、远程端口必须在 1-65535 |
| 本地 webServer 端口 | 多个 frpc 实例不能共用同一个本地管理端口 |
| HTTP/HTTPS 域名 | 必须填写 customDomains 或 subdomain |
| 本地服务可达性 | 可选探测 localIP/localPort 是否能连接 |
| TOML 渲染 | 生成结果必须能被 TOML parser 正确解析 |
| frpc 命令校验 | 调用当前 frpc 版本进行配置或版本兼容性检查 |

### 4.9 配置模板与现有配置导入

为了降低初次使用门槛，系统应提供常见代理场景模板，并支持导入用户已有的 frpc 配置。

配置模板：

1. SSH TCP 穿透：默认本地端口 22，填写远程端口即可。
2. 远程桌面 TCP：默认本地端口 3389 或 5900。
3. 本地 Web HTTP：默认本地端口 8080，填写域名。
4. HTTPS 站点代理：填写本地 HTTPS 服务端口和域名。
5. 游戏/语音 UDP：填写本地端口和远程端口。
6. 自定义模板：管理员可保存常用规则为模板，后续一键复用。

现有配置导入：

1. 支持上传或粘贴 `frpc.toml`。
2. 尽量兼容旧版 `frpc.ini`，导入后转换为 TOML 管理模型。
3. 导入前先生成预览，不直接覆盖现有配置。
4. 无法识别的配置项保留为“高级配置片段”或提示用户手动处理。
5. 导入完成后生成配置版本记录，支持回滚。

### 4.10 健康检查与告警通知

进程存在不代表代理可用。系统需要同时关注 frpc 进程、服务器连接和每条代理规则的实际状态。

健康检查维度：

1. frpc 进程是否存活。
2. frpc 与 frps 是否保持连接。
3. 每条代理是否处于 active/online 状态。
4. 最近一次启动、重载、停止是否成功。
5. 日志中是否出现认证失败、端口被占用、域名未绑定等关键错误。
6. 本地服务端口是否可达。

告警通知：

1. frpc 异常退出。
2. 自动启动失败。
3. 热重载失败并已回滚。
4. 某条代理离线或启动失败。
5. frpc 有新版本可用。
6. 登录失败次数过多或出现异常 IP 登录。

通知渠道可先支持 Webhook，再扩展邮件、企业微信、Telegram、Server 酱等方式。告警需要支持静默时间、重复告警抑制和恢复通知，避免频繁打扰。

### 4.11 备份迁移与部署模式

备份迁移：

1. 支持一键导出服务器、代理规则、用户、角色、审计摘要和已安装版本元数据。
2. 敏感字段导出时必须加密，导入时需要管理员提供备份密码。
3. 支持只导出配置，不导出用户和审计日志。
4. 支持导入前预览，显示将新增、覆盖或跳过的对象。
5. 支持定期自动备份，并设置备份保留数量。

部署模式：

1. 普通进程模式：适合开发和桌面用户。
2. systemd 服务模式：适合 Linux 服务器长期运行。
3. Windows Service 模式：适合 Windows 主机长期运行。
4. Docker 模式：适合 NAS、轻量云主机和容器化环境。
5. 反向代理模式：推荐使用 Nginx/Caddy 负责 HTTPS 和公网入口。

## 5. 技术架构建议

推荐采用“单后端服务 + 前端 Web UI + SQLite + frpc 进程池”的架构。

### 5.1 推荐技术栈

| 层级 | 推荐方案 | 原因 |
| --- | --- | --- |
| 后端 | Go | 便于打包单二进制，跨平台，进程管理和文件操作能力强 |
| 前端 | Vue 3 + TypeScript + Vite | 表单、状态管理和组件组织清晰，适合配置管理后台 |
| UI 组件库 | Element Plus | 后台管理组件成熟，表单、表格、弹窗、菜单能力完整，中文资料多 |
| 状态管理 | Pinia | 适合管理登录状态、服务器状态、日志流状态和全局设置 |
| 路由 | Vue Router | 用于登录页、总览页、服务器页、日志页、版本页等页面切换 |
| 数据库 | SQLite | 单机管理器足够，部署简单 |
| 配置格式 | TOML | frp 官方推荐的新配置格式之一 |
| 实时通信 | SSE 优先，WebSocket 可选 | SSE 足够支撑日志推送和状态刷新，实现更简单 |
| 认证 | JWT 或 Cookie Session | 满足 Web 管理页认证需求 |

如果希望更快开发，也可以使用 Node.js/NestJS 后端，但从安装包、进程管理、跨平台分发角度看，Go 更适合作为最终交付形态。

### 5.2 前端架构建议

前端确定采用 Vue 3 技术栈，定位为管理后台，不做 SSR，也不做营销页。生产环境由 Vite 构建静态文件，再由 Go 后端 embed 到最终二进制中，对用户表现为“启动一个程序，打开浏览器即可使用”。

推荐前端技术组合：

```text
Vue 3
TypeScript
Vite
Vue Router
Pinia
Element Plus
Axios 或封装后的 fetch
SSE 日志流
Monaco Editor 可选，用于 TOML 预览和配置差异查看
```

推荐前端目录结构：

```text
web/
  src/
    api/
      auth.ts
      servers.ts
      rules.ts
      frpc.ts
      logs.ts
      backups.ts
    components/
      StatusBadge.vue
      ProxyRuleForm.vue
      LogViewer.vue
      ConfigPreview.vue
      VersionDiff.vue
    layouts/
      AppLayout.vue
      AuthLayout.vue
    pages/
      LoginPage.vue
      DashboardPage.vue
      ServersPage.vue
      ServerDetailPage.vue
      LogsPage.vue
      VersionsPage.vue
      UsersPage.vue
      SettingsPage.vue
    router/
      index.ts
    stores/
      auth.ts
      servers.ts
      logs.ts
      settings.ts
    styles/
      main.css
    main.ts
```

页面体验原则：

1. 默认进入总览页，直接展示服务器状态、异常事件和常用操作。
2. 服务器详情页采用标签页组织：基础信息、代理规则、日志、配置历史、健康检查。
3. 代理规则表单根据 TCP、UDP、HTTP、HTTPS 动态展示字段，隐藏无关配置。
4. 日志查看器使用固定高度和虚拟滚动，避免大量日志导致页面卡顿。
5. 长任务使用进度反馈，例如在线安装 frpc、上传离线包、备份导入。
6. 所有危险操作需要二次确认，例如删除服务器、停止运行中实例、恢复旧配置。
7. Token、密码、通知密钥等敏感字段默认脱敏展示。

### 5.3 模块划分

| 模块 | 职责 |
| --- | --- |
| Web UI | 服务器列表、代理规则表单、日志页、版本页、用户页 |
| API Server | REST API、认证鉴权、参数校验 |
| Config Renderer | 将数据库配置渲染为 `frpc.toml` |
| Config Versioning | 配置版本记录、差异对比、恢复和回滚 |
| Process Manager | 启动、停止、重载、状态检测 frpc 进程 |
| Installer | 在线下载、离线上传、版本检测、回滚 |
| Log Manager | 日志采集、tail、脱敏、轮转、实时推送 |
| Auth/RBAC | 登录、Token、角色权限、会话管理 |
| Audit | 操作审计和安全事件记录 |
| Health Checker | 进程、连接、代理规则和本地服务健康检查 |
| Notifier | Webhook、邮件等告警通知 |
| Backup Manager | 配置备份、加密导出和导入恢复 |
| Storage | SQLite 数据和本地文件目录管理 |

### 5.4 本地目录结构

建议使用应用数据目录保存运行文件：

```text
frpc-web-data/
  app.db
  bin/
    frpc/
      v0.68.0/
        frpc
      current -> v0.68.0/frpc
  configs/
    <serverId>/
      frpc.toml
      frpc.toml.bak
      versions/
        20260608103000.toml
  logs/
    <serverId>/
      frpc.log
  backups/
  uploads/
  tmp/
```

## 6. 核心流程设计

### 6.1 添加并启动服务器

1. 用户填写服务器基础信息。
2. 用户添加至少一条代理规则。
3. 后端校验参数并写入数据库。
4. 后端渲染 `frpc.toml`。
5. 后端检查 frpc 是否已安装。
6. 用户点击启动。
7. 后端启动 frpc 进程并绑定日志文件。
8. 后端通过状态命令或 Admin API 获取运行状态。
9. 前端展示为 running。

### 6.2 修改代理规则并热重载

1. 用户编辑代理规则并保存。
2. 后端校验规则。
3. 后端生成新的 TOML 临时文件。
4. 后端执行配置校验。
5. 后端原子替换正式配置文件。
6. 后端执行 `frpc reload -c <config>`。
7. 后端刷新代理状态。
8. 成功则提示“已热重载”，失败则回滚配置并提示错误。

### 6.3 修改公共配置并重启

1. 用户修改服务器地址、端口、认证、传输协议等公共配置。
2. 后端保存配置并标记 `restart_required`。
3. 页面提示“此类修改需要重启 frpc 后生效”。
4. 用户确认重启。
5. 后端停止旧进程并启动新进程。
6. 成功后状态恢复为 running。

### 6.4 在线安装 frpc

1. 用户进入版本管理页。
2. 后端请求 GitHub Releases 获取版本列表。
3. 用户选择版本。
4. 后端下载对应平台压缩包。
5. 后端解压、校验、登记版本。
6. 用户可选择设为默认版本。

### 6.5 离线安装 frpc

1. 用户上传压缩包或二进制文件。
2. 后端保存到临时目录。
3. 后端解压或检测可执行文件。
4. 执行 `frpc --version`。
5. 验证通过后登记版本。
6. 清理临时文件。

### 6.6 导入现有配置

1. 用户上传或粘贴现有 `frpc.toml` 或 `frpc.ini`。
2. 后端解析配置并生成导入预览。
3. 用户确认服务器信息、代理规则和无法识别的高级配置项。
4. 后端写入数据库并生成首个配置版本。
5. 用户选择立即启动、保存待启动或合并到已有服务器。

### 6.7 配置回滚

1. 用户在配置历史中选择目标版本。
2. 后端展示当前版本与目标版本的差异。
3. 用户确认恢复。
4. 后端渲染目标版本并执行预检查。
5. 如果当前 frpc 正在运行且变更可热重载，则执行热重载。
6. 如果变更需要重启，则标记为 `restart_required` 并等待用户确认。

### 6.8 备份与恢复

1. 管理员选择备份范围，例如仅配置、配置加用户、完整备份。
2. 后端导出数据库记录和必要文件元数据。
3. 敏感字段使用备份密码派生密钥加密。
4. 导入时先解析备份包并展示变更预览。
5. 管理员确认后写入数据库，必要时重新生成所有 `frpc.toml`。

### 6.9 告警通知

1. 管理员配置通知渠道和告警规则。
2. Health Checker 定期检测进程、连接、代理规则和本地服务。
3. 检测到异常时生成健康事件。
4. Notifier 根据静默规则和重复抑制策略发送通知。
5. 状态恢复后发送恢复通知，并关闭对应健康事件。

## 7. 数据模型草案

### 7.1 users

| 字段 | 说明 |
| --- | --- |
| id | 用户 ID |
| username | 用户名 |
| password_hash | 密码哈希 |
| role | admin、operator、viewer |
| status | enabled、disabled |
| mfa_enabled | 是否启用 TOTP 二次验证 |
| created_at | 创建时间 |
| last_login_at | 最近登录时间 |
| last_login_ip | 最近登录 IP |

### 7.2 servers

| 字段 | 说明 |
| --- | --- |
| id | 服务器配置 ID |
| name | 显示名称 |
| server_addr | frps 地址 |
| server_port | frps 端口 |
| auth_method | token 等 |
| auth_token_enc | 加密后的 token |
| transport_protocol | tcp、kcp、quic 等可扩展 |
| admin_addr | frpc 本地 webServer 地址 |
| admin_port | frpc 本地 webServer 端口 |
| auto_start | 是否自动启动 |
| status | 运行状态 |
| last_health_status | 最近健康检查状态 |
| last_reload_at | 最近一次热重载时间 |
| restart_required | 是否需要重启 |
| created_by | 创建人 |
| updated_at | 更新时间 |

### 7.3 proxy_rules

| 字段 | 说明 |
| --- | --- |
| id | 规则 ID |
| server_id | 所属服务器 |
| name | 规则名称 |
| type | tcp、udp、http、https |
| local_ip | 本地地址 |
| local_port | 本地端口 |
| remote_port | 远程端口 |
| custom_domains | JSON 数组 |
| subdomain | 子域名 |
| enabled | 是否启用 |
| remark | 备注 |
| advanced_config | JSON，高级配置扩展字段 |
| sort_order | 排序 |

### 7.4 process_instances

| 字段 | 说明 |
| --- | --- |
| server_id | 服务器 ID |
| pid | 进程 ID |
| frpc_version | 使用的 frpc 版本 |
| config_path | 配置路径 |
| log_path | 日志路径 |
| started_at | 启动时间 |
| stopped_at | 停止时间 |
| exit_code | 退出码 |

### 7.5 audit_logs

| 字段 | 说明 |
| --- | --- |
| id | 日志 ID |
| user_id | 操作用户 |
| action | 操作类型 |
| resource_type | 资源类型 |
| resource_id | 资源 ID |
| ip | 来源 IP |
| result | success、failed |
| message | 详情 |
| created_at | 创建时间 |

### 7.6 server_permissions

| 字段 | 说明 |
| --- | --- |
| id | 权限记录 ID |
| server_id | 服务器 ID |
| user_id | 用户 ID |
| permission | read、write、operate、admin |
| created_at | 创建时间 |

### 7.7 config_versions

| 字段 | 说明 |
| --- | --- |
| id | 配置版本 ID |
| server_id | 所属服务器 |
| version_no | 递增版本号 |
| toml_snapshot | 渲染后的 TOML 快照 |
| change_summary | 变更摘要 |
| checksum | 配置内容校验值 |
| created_by | 创建人 |
| created_at | 创建时间 |
| applied_at | 实际应用时间 |
| apply_result | success、failed、pending |

### 7.8 proxy_templates

| 字段 | 说明 |
| --- | --- |
| id | 模板 ID |
| name | 模板名称 |
| type | tcp、udp、http、https |
| defaults | JSON 默认字段 |
| builtin | 是否内置模板 |
| created_by | 创建人 |
| updated_at | 更新时间 |

### 7.9 frpc_versions

| 字段 | 说明 |
| --- | --- |
| id | 版本记录 ID |
| version | frpc 版本号 |
| platform | 操作系统 |
| arch | CPU 架构 |
| binary_path | 二进制路径 |
| source | online、offline |
| active | 是否为默认版本 |
| installed_at | 安装时间 |

### 7.10 notification_channels

| 字段 | 说明 |
| --- | --- |
| id | 通知渠道 ID |
| name | 渠道名称 |
| type | webhook、email、wechat、telegram 等 |
| config_enc | 加密后的渠道配置 |
| enabled | 是否启用 |
| created_at | 创建时间 |

### 7.11 health_events

| 字段 | 说明 |
| --- | --- |
| id | 健康事件 ID |
| server_id | 服务器 ID |
| proxy_rule_id | 可选，关联代理规则 |
| level | info、warning、critical |
| status | open、recovered、ignored |
| message | 事件描述 |
| first_seen_at | 首次出现时间 |
| last_seen_at | 最近出现时间 |
| recovered_at | 恢复时间 |

### 7.12 backups

| 字段 | 说明 |
| --- | --- |
| id | 备份 ID |
| filename | 备份文件名 |
| scope | config_only、with_users、full |
| encrypted | 是否加密 |
| size_bytes | 文件大小 |
| created_by | 创建人 |
| created_at | 创建时间 |

## 8. API 草案

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/auth/login` | 登录 |
| POST | `/api/auth/logout` | 退出登录 |
| GET | `/api/servers` | 获取服务器列表 |
| POST | `/api/servers` | 新增服务器 |
| PUT | `/api/servers/{id}` | 编辑服务器 |
| DELETE | `/api/servers/{id}` | 删除服务器 |
| POST | `/api/servers/{id}/start` | 启动 |
| POST | `/api/servers/{id}/stop` | 停止 |
| POST | `/api/servers/{id}/reload` | 热重载 |
| POST | `/api/servers/{id}/restart` | 重启 |
| GET | `/api/servers/{id}/status` | 查看状态 |
| GET | `/api/servers/{id}/logs?tail=200` | 获取最近日志 |
| GET | `/api/servers/{id}/logs/stream` | 实时日志流 |
| GET | `/api/servers/{id}/rules` | 获取代理规则 |
| POST | `/api/servers/{id}/rules` | 新增代理规则 |
| PUT | `/api/servers/{id}/rules/{ruleId}` | 编辑代理规则 |
| DELETE | `/api/servers/{id}/rules/{ruleId}` | 删除代理规则 |
| GET | `/api/servers/{id}/config/preview` | 预览渲染后的 TOML |
| POST | `/api/servers/{id}/config/check` | 执行配置预检查 |
| GET | `/api/servers/{id}/config/versions` | 查看配置版本历史 |
| GET | `/api/servers/{id}/config/versions/{versionId}/diff` | 查看配置版本差异 |
| POST | `/api/servers/{id}/config/versions/{versionId}/restore` | 恢复指定配置版本 |
| POST | `/api/config/import/preview` | 上传或粘贴配置并生成导入预览 |
| POST | `/api/config/import/apply` | 确认导入配置 |
| GET | `/api/proxy-templates` | 查看代理模板 |
| POST | `/api/proxy-templates` | 新增自定义代理模板 |
| PUT | `/api/proxy-templates/{id}` | 编辑代理模板 |
| DELETE | `/api/proxy-templates/{id}` | 删除代理模板 |
| GET | `/api/frpc/versions` | 查看已安装版本 |
| GET | `/api/frpc/releases` | 查看在线版本 |
| POST | `/api/frpc/install/online` | 在线安装 |
| POST | `/api/frpc/install/offline` | 离线安装 |
| GET | `/api/health/events` | 查看健康事件 |
| POST | `/api/health/events/{id}/ignore` | 忽略健康事件 |
| GET | `/api/notification-channels` | 查看通知渠道 |
| POST | `/api/notification-channels` | 新增通知渠道 |
| POST | `/api/notification-channels/{id}/test` | 测试通知渠道 |
| GET | `/api/backups` | 查看备份列表 |
| POST | `/api/backups/export` | 创建备份 |
| POST | `/api/backups/import/preview` | 预览备份导入 |
| POST | `/api/backups/import/apply` | 确认导入备份 |
| GET | `/api/audit-logs` | 查看审计日志 |

## 9. 前端页面规划

### 9.1 登录页

提供用户名、密码登录。支持首次启动初始化管理员账号。

### 9.2 总览页

展示服务器总数、运行中数量、异常数量、frpc 当前版本、最近操作记录和最新健康事件。

### 9.3 服务器管理页

左侧或表格展示所有服务器，包含名称、地址、状态、规则数量、自动启动、运行时长、操作按钮。

关键操作：

1. 添加服务器
2. 编辑服务器
3. 启动/停止/重载/重启
4. 查看日志
5. 查看配置预览
6. 执行预检查
7. 查看配置历史和回滚

### 9.4 代理规则页

按服务器展示规则列表，支持新增、编辑、复制、启用/禁用、删除、排序。

规则表单根据代理类型动态切换字段，降低用户理解成本。

规则页应提供模板入口，用户可以从 SSH、远程桌面、本地 Web、HTTPS、UDP 游戏等模板快速创建规则，也可以把当前规则保存为自定义模板。

### 9.5 日志页

展示最近 200 行日志，支持自动刷新、暂停、关键词过滤和复制。

### 9.6 版本管理页

展示当前 frpc 安装情况、在线最新版本、已安装版本、上传离线包入口。

### 9.7 用户与安全页

管理员可管理用户、角色、登录会话、安全策略和审计日志。

### 9.8 配置历史页

展示某个服务器的配置版本列表、变更人、变更摘要、应用结果，支持查看 TOML 快照、差异对比和恢复指定版本。

### 9.9 导入导出页

支持粘贴或上传 `frpc.toml`、`frpc.ini`，解析后展示导入预览。也支持创建加密备份、下载备份包和导入备份包。

### 9.10 健康与告警页

展示当前健康事件、历史异常、通知渠道和告警规则。管理员可以测试通知渠道、忽略单次告警或设置静默时间。

### 9.11 部署设置页

展示当前运行模式，支持普通进程、systemd、Windows Service、Docker 部署说明或安装向导。

## 10. 安全设计重点

1. 管理器首次启动必须要求初始化管理员密码。
2. 管理器监听公网时必须给出明显风险提示。
3. 所有修改配置、启停进程、安装版本的接口必须要求登录。
4. 前端不能展示完整 token，默认脱敏，只有编辑时允许重新填写。
5. 配置文件落盘不可避免包含 frps token，应限制文件权限。
6. 离线上传必须限制文件大小和类型，避免任意文件覆盖。
7. 解压安装包时必须防止 Zip Slip 路径穿越。
8. 启动 frpc 时参数必须由后端构造，不能拼接用户输入作为 shell 命令。
9. 审计日志不可由普通用户删除。
10. 日志推送接口不能绕过鉴权。
11. TOTP 密钥、API Token、通知渠道密钥和备份密码派生信息必须加密或哈希保存。
12. API Token 应支持过期时间、权限范围和单独吊销。
13. 备份包导入时必须校验格式和签名，禁止覆盖应用目录之外的文件。

## 11. 实现阶段规划

### 阶段一：MVP

目标是让单用户可以完整管理 frpc。

功能范围：

1. 管理员初始化和登录。
2. frpc 在线/离线安装。
3. 多服务器 CRUD。
4. TCP、UDP、HTTP、HTTPS 规则 CRUD。
5. TOML 生成。
6. 基础配置预检查。
7. 启动、停止、热重载、重启。
8. 状态展示。
9. 最近 200 行日志查看。
10. 配置预览。

### 阶段二：增强可用性

功能范围：

1. 自动启动。
2. 日志实时推送。
3. 配置预览和差异展示。
4. 版本检测、升级和回滚。
5. 操作审计。
6. 配置版本历史和手动回滚。
7. 现有 `frpc.toml` 导入。
8. 内置代理模板。
9. 配置备份和恢复。

### 阶段三：多用户与高级能力

功能范围：

1. 多用户和角色权限。
2. 按服务器授权。
3. 登录限速、会话管理。
4. 更完整的 frpc 参数支持。
5. 支持 frpc Store 动态代理管理能力。
6. 健康检查和告警通知。
7. 可选系统服务安装，例如 systemd、Windows Service。
8. Docker 部署模式和反向代理部署指引。

## 12. 风险与注意事项

| 风险 | 说明 | 应对 |
| --- | --- | --- |
| 热重载能力有限 | frpc 热重载主要用于代理配置，非代理公共配置不能完全动态修改 | 区分“可热重载”和“需重启”字段 |
| 端口冲突 | 多服务器本地 webServer 端口、代理远程端口可能冲突 | 自动分配本地端口，保存前校验 |
| 敏感信息泄露 | token 可能出现在配置文件、日志、备份中 | 加密落库、脱敏展示、限制文件权限 |
| 进程残留 | 异常退出或管理器重启后状态不一致 | 启动时扫描 PID 和端口，恢复状态 |
| 网络受限 | 无法访问 GitHub Releases | 提供离线安装 |
| 跨平台差异 | Linux、macOS、Windows 的进程和权限不同 | 抽象 Process Manager，分平台实现 |
| frp 版本差异 | 新版本配置项和命令可能变化 | 版本检测后适配，保留兼容层 |
| 配置导入不完整 | 旧版 ini 或高级配置可能无法完全映射到表单 | 导入预览、保留高级配置片段、提示人工确认 |
| 告警过多 | 网络抖动可能导致重复通知 | 静默时间、重复抑制、恢复通知 |
| 备份泄露 | 备份包可能包含服务器 token 和用户信息 | 敏感字段加密、备份密码、导出范围控制 |
| 服务化权限过高 | systemd 或 Windows Service 可能以高权限运行 | 默认最小权限运行，安装前提示权限边界 |

## 13. 验收标准

1. 用户可以通过 Web 页面完成 frpc 安装，无需命令行。
2. 用户可以添加至少两个 FRP 服务器，并分别启动、停止和查看状态。
3. 用户可以为同一服务器添加 TCP、UDP、HTTP、HTTPS 四类代理规则。
4. 系统可以正确生成 `frpc.toml`，并能启动 frpc。
5. 修改代理规则后，系统优先使用热重载生效，而不是直接杀进程。
6. 修改公共配置后，系统能明确提示需要重启。
7. 每个服务器可以查看独立日志，默认展示最近 200 行。
8. Web 管理页必须登录后才能访问。
9. 管理员可以查看当前 frpc 版本并安装/上传新版本。
10. 自动启动开启后，管理器重启能自动拉起对应 frpc 连接。
11. 每次配置保存后都能生成配置版本，并可查看差异。
12. 热重载失败时能自动回滚到上一版配置并显示失败原因。
13. 用户可以导入已有 `frpc.toml`，导入前能看到预览。
14. 用户可以通过模板快速创建常见 TCP、UDP、HTTP、HTTPS 规则。
15. 系统可以展示健康事件，并在 frpc 异常退出或重载失败时触发告警。
16. 管理员可以导出加密备份，并从备份包恢复配置。

## 14. 需要进一步确认的问题

1. 首个版本是否只做本机单节点管理，还是需要远程管理多台机器上的 frpc？
2. 是否必须支持 Windows？如果需要，停止进程、后台运行、服务化安装要单独设计。
3. 是否需要内置 HTTPS，还是推荐放在 Nginx/Caddy 后面？
4. 是否需要支持更多 frp 能力，例如 STCP、SUDP、XTCP、插件、访客配置？
5. 是否需要做 Docker 镜像部署？
6. 多用户权限是只按角色区分，还是要做到“用户只能管理指定服务器”？
7. 告警通知首批需要支持哪些渠道？
8. 备份是否需要自动定时上传到远程存储？
9. 导入旧版 `frpc.ini` 时，无法识别的配置项是否允许以高级配置方式保留？

## 15. 参考资料

1. frp 官方安装说明：<https://gofrp.org/en/docs/setup/>
2. frp 官方客户端动态配置更新说明：<https://gofrp.org/zh-cn/docs/features/common/client/>
3. frp 官方 Web 界面与 Client Admin UI 说明：<https://gofrp.org/zh-cn/docs/features/common/ui/>
4. frp GitHub Releases：<https://github.com/fatedier/frp/releases>
