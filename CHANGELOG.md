# Changelog

## [v1.1.4]

### Fixed
- **控制台健康事件排版**：时间戳由原始 RFC3339（`2026-06-11T14:22:38+08:00`）改为紧凑格式
  （今天 `14:22`，今年 `06-10 09:41`，更早带年份；悬停显示完整时间），不再把侧栏消息列挤成细条；
  长英文 token（如 `dial tcp ...`）自动断行；新增空状态提示；节点已删除时名称兜底为「系统」
- **设置页审计日志**：时间列同样改为紧凑格式（悬停显示完整时间戳）

## [v1.1.3]

### Fixed
- **设置页版本列表排版**：v1.1.1 的布局修复因 CSS 级联顺序失效（`.version-row` 被文件后方的
  `.session-row` 三列网格覆盖），版本信息被挤进 36px 图标列竖向堆叠；现以 `.session-row.version-row`
  提高特异性根治，版本行改为「眉标 + 版本号 + 平台/路径胶囊」布局，路径超长自动截断（悬停显示全文），
  移动端折叠为单列
- **静态资源缓存策略**：此前所有静态文件不带任何缓存头（embed 文件无 Last-Modified）。
  现 `index.html`（含 SPA 路由回退）返回 `Cache-Control: no-cache`，杜绝自更新后浏览器渲染旧页面；
  带内容哈希的 `assets/*` 返回 `public, max-age=31536000, immutable`，不再每次访问全量重下

### Changed
- **设置页**：frpc 版本面板副标题由完整安装路径改为「当前使用 x.y.z」，未安装时提示可在线安装或上传

## [v1.1.2]

### Added
- **忘记密码自助重置**：新增 `FRPC_WEB_RESET_KEY` 环境变量，设为 `1` 启动会清空访问密钥并退出，
  之后正常启动即可重新设置；README 补充 systemd 与手动运行的操作步骤

## [v1.1.1]

### Changed
- **设置页**：系统更新与 Active 版本卡片排版调整（其中版本行布局因 CSS 级联问题未实际生效，
  v1.1.3 已根治）

## [v1.1.0]

### Added
- **Web 一键自更新**：设置页新增「系统更新」卡片，检查 GitHub 最新版本并一键升级
  - 下载后校验 SHA256SUMS，原子替换二进制，`exec` 原地重启（PID 不变，systemd 无感知）
  - 运行中的 frpc 隧道不中断，重启后自动接管遗留子进程（退出回收 + 自动重启语义保留）
  - 进程无权限替换二进制时给出 `curl | bash` 升级命令提示
  - systemd 安装布局调整：二进制移至 `/opt/frpc-web/bin`（服务用户可写），`/usr/local/bin` 保留软链接

### Changed
- **服务器表单**：移除 Admin 端口/用户/密码三个输入项，全部由系统自动管理
  （端口自动分配、凭据自动生成，仅监听 127.0.0.1；编辑时自动保留现有值）
- **登录限流提示**：锁定时返回中文提示并标明剩余等待分钟数
  （防爆破机制：同 IP 连续失败 5 次锁定 5 分钟，逐次递增至 30 分钟，密钥 bcrypt 哈希存储）
- **拓扑图**：每条规则的连线使用专属颜色（左右两段同色），公网卡片只显示端口并标注规则名，
  服务端入口卡片加宽且地址可换行

### Removed
- **流量看板**：frp 客户端接口不提供流量统计（流量数据只存在于 frps 服务端），图表永远无数据，
  整页移除；同时移除 `/api/stats`、Admin API 状态采集和 echarts 依赖（前端体积 -529KB）

## [v1.0.0]

### Changed
- **存储引擎**：SQLite → JSON 状态文件（`state.json`）
  - 个人单机使用场景下数据量极小，JSON 状态文件更简单：人类可读、天然备份、零迁移负担
  - 移除 `modernc.org/sqlite` 依赖（及其 7 个间接依赖），二进制体积从 17MB 降至 11MB
  - 启动时检测到旧 `app.db` 会提示用户导出配置后导入新版本
  - 审计日志与健康事件自动裁剪到最近 500/200 条，防止状态文件无限增长
- **会话管理**：移除 JWT 层，Cookie 直接存储 session token（SHA-256 哈希存储）
  - 删除 `signToken`/`verifyToken` 及 `jwt_secret` 设置项
  - 简化认证流程，减少约 150 行代码
- **用户模型**：移除多用户/角色系统，塌缩为单管理员模式
  - 删除 `User`/`ownerUser`/`RoleAdmin` 概念，`AuthStatus` 仅保留 `bootstrapped`/`authenticated` 布尔值
  - 删除 `/api/auth/me`、`/api/auth/sessions` 列表及按 ID 撤销、`/api/auth/sessions/revoke-others`
  - 前端移除会话列表 UI 与 `canOperate` 权限判断，保留退出登录与修改密钥全失效
- **审计日志**：简化为操作历史
  - `AuditLog` 移除 `userId`/`username`/`role` 字段，查询移除 `user` 筛选
  - 保留 `action`/`result` 筛选、分页、清理按钮；定容最近 500 条（自动裁剪）
- **配置模式**：移除 `store_api` 实验模式
  - 删除 `Server.ConfigMode`/`Server.FRPCVersionID` 字段
  - 移除 `runtime.go` 中 `syncStoreAPI`/`writeStoreFile`/`ruleToStore*` 等约 170 行实现
  - 前端服务器配置抽屉删除配置模式选择器与实验提示横幅
  - STCP/XTCP 规则校验移除 store_api 特判
- **版本绑定**：移除 per-server frpc 版本固定
  - 全局统一使用当前激活版本（`ActiveVersion`），保留多版本安装与一键切换
  - 删除 `versionForServer` 双路逻辑，替换为 `requireActiveVersion`
  - 前端服务器表单删除版本选择器
- **配置导出**：移除脱敏选项，导出永远包含完整敏感字段
  - 删除 `maskServerSecrets`/`maskRuleSecrets`/`looksMaskedSecret` 全家桶
  - 前端设置页移除「包含敏感信息」勾选框，导出前弹窗提醒用户妥善保管
  - 简化原则：个人备份不需要脱敏，UI 表单里"留空保留原密码"的逻辑保留即可
- **前端打包**：Element Plus 全量引入 → 按需导入（unplugin-vue-components + unplugin-auto-import）
  - 首屏 JS 从 911KB 降至 185KB，CSS 从 392KB 降至 54KB
  - `ElMessage`/`ElMessageBox`/`ElTooltip` 等组件自动注入，移除显式 `import`
  - `v-loading` 指令需手动注册（`main.ts` 中单独引入）
- **文案澄清**：服务器卡片与操作按钮说明"节点=本机 frpc 进程"
  - 副标题改为「每个节点是一个本机 frpc 进程，连接到对应的 frps；启停操作均作用于本机进程」
  - Tooltip 明确「启动本机 frpc 进程」「停止本机 frpc 进程」「热重载：frpc 重读配置并向 frps 重新注册代理」

### Removed
- SQLite 依赖（`modernc.org/sqlite` 及其 7 个间接依赖）
- JWT 层（约 150 行）
- 用户/角色模型与会话管理 API（约 200 行）
- `store_api` 配置模式实现（约 170 行）
- Per-server frpc 版本绑定字段与逻辑
- 配置导出脱敏机制（`maskSecret` 全套约 60 行）
- 登录限流器对 `FRPC_WEB_JWT_SECRET` 环境变量的依赖
- 前端 `/api/auth/me`、`/api/auth/sessions` 相关 UI 与逻辑

### Fixed
- 修复 `errorResult` 对 `sql.ErrNoRows` 的判断（改用统一的 `app.ErrNotFound`）
- 修复前端 `installFrpcOnline` 硬编码 `platform: 'linux'`（改为空字符串由后端自动检测）

### Technical
- 新增 `internal/storage/store.go` 约 850 行（JSON 状态文件存储实现）
- 新增 `app.ErrNotFound` 错误，替代 `sql.ErrNoRows`
- `Store` 接口移除 `ListSessions`/`RevokeSession`/`RevokeOtherSessions` 方法
- `Service` 接口移除 `JWTSecret`/`Sessions`/`RevokeSession`/`RevokeOtherSessions` 方法
- `AuthSession` 类型合并到 `Session`，移除 `User` 嵌套
- 测试全面更新：`service_test.go`/`server_test.go` 移除会话列表与 JWT 相关断言
- 前端类型定义移除 `User`/`Session` 导出，`AuthStatus` 简化为两个布尔值

## [Previous releases]

(保留既有版本记录)
