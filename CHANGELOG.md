# Changelog

## [Unreleased]

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
