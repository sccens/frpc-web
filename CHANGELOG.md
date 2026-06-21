# Changelog

## [v2.1.0] - 2026-06-21

### Added
- **多 frps 流量监控**：新增「流量」页面，可配置多个 frps Dashboard/Prometheus 目标并聚合展示在线数、连接数、实时/累计流量、趋势曲线与 proxy 排行。
- **frps 目标管理 API**：新增 `/api/frps/targets` 与 `/api/frps/metrics` 系列接口；目标密码仅服务端保存，列表响应只返回 `hasPassword`，编辑时密码留空会保留原值。
- **frps 采样器**：后台按目标轮询 `/metrics`，解析 Prometheus 指标并计算总量与速率；离线、禁用、未采样目标均保持可展示状态。

### Changed
- **设置页布局**：将「安全」与「配置备份」合并为同一双列区域，减少桌面宽屏空白。
- **一键自更新签名要求**：缺少 ed25519 发布签名公钥时禁用一键更新；应用更新时必须校验 `SHA256SUMS.sig` 后再校验二进制 SHA256。
- **HTTP 服务超时**：主 HTTP 服务补充读头、读、写与 idle 超时，降低慢连接占用风险。

### Fixed
- 修复新增 frps 后、目标尚未采样或离线时流量页面因 `proxies/history` 为 `null` 而空白的问题。
- 修复添加 frps 弹窗在较矮窗口或页面滚动状态下底部按钮被挤出视口的问题。
- 升级前端间接依赖 `form-data` 至 `4.0.6`。

## [v2.0.0] - 2026-06-21

### 重大变更：从「进程管理器」重定位为「只读配置监控面板」

frpc-web 不再启动/停止/重启 frpc 进程，也不再管理 frpc 版本与代理规则。进程生命周期交由 systemd，面板专注于：扫描磁盘上的 frpc 配置文件并只读展示、连接拓扑可视化、通过 admin API 获取实时状态、面板内编辑配置文件原文并触发热重载。

### Removed（不再支持）
- **进程管理**：启动/停止/重启 frpc、崩溃自动重启、自更新原地接管遗留子进程。
- **接管已有 frpc**：adopt（restart/attach）、发现二进制与进程、登记系统二进制。
- **frpc 版本管理**：在线/离线安装、多版本切换、激活版本。
- **服务器与规则 CRUD**：创建/编辑/删除服务器与代理规则（改为只读展示配置文件解析结果）。
- **总览页（Dashboard）**：删除，统计信息并入拓扑页。
- **自动备份调度与 `backups/` 目录管理**：保留配置导出/导入（语义改为写回配置文件）。
- **前端审计日志卡片**：后端审计能力保留，前端入口移除。
- 旧 `state.json` 的服务器/版本/进程数据不再兼容（首启自动备份为 `state.json.v1.bak`）。

### Added
- **配置文件扫描**：后台扫描 `/etc/frpc`、`/usr/local/etc/frpc`、数据目录及 `FRPC_WEB_CONFIG_PATH` 指定路径下的 frpc 配置（TOML/INI），一文件一实例。
- **配置文件编辑**：面板内编辑配置文件原文，原子写入；不可写时降级为只读 + 下载。
- **admin API 热重载**：`POST /api/servers/{id}/reload` 调用 frpc 的 `GET /api/reload`。
- **实时状态缓存**：后台并发探测各实例 admin API `/api/status`，列表读取缓存避免逐次同步探活卡顿。
- **可写部署开关**：`install.sh` 新增 `ALLOW_CONFIG_EDIT=1`（配置目录加入 systemd `ReadWritePaths` 并授权组写）。
- **日志路径解析**：从配置的 `log.to` 解析日志文件路径。

### Changed
- 数据存储精简：`state.json` 仅保留设置/会话/健康事件/审计；server 列表不持久化，来自扫描。
- 默认登录后跳转拓扑页；导航精简为 拓扑 / 服务器 / 设置。
- 认证（初始密钥 + 强制改密）、配置导出、frpc-web 自更新机制保持不变。
- 依赖与构建方式不变（Go 1.26+、Node 20.19+、单二进制内嵌前端）。

### Migration
- 升级前用 v1.5.x 的「导出配置」备份现有 frpc 配置。
- 升级后把 frpc 配置放到扫描路径，用 systemd 管理 frpc 进程。
- 详见 README「部署 / 迁移」。

## [v1.5.5] - 2026-06-21

### Fixed
- **二进制发现功能**：修复无法扫描到面板安装的 frpc 的问题
  - 修复前：通过面板安装 frpc 后，在"发现二进制"列表中看不到
  - 修复后：优先扫描 frpc-web 管理的 bin 目录，显示所有已安装的 frpc
  - 面板安装的 frpc 会标记为 "Managed"，更容易识别

## [v1.5.4] - 2026-06-21

### Added
- **管理模式区分**：明确区分完全托管和附着观察两种管理权限
  - `managementMode` 字段：`"managed"`（完全托管）或 `"attached"`（附着观察）
  - 完全托管：面板启动的进程，可以启动/停止/重启/重载
  - 附着观察：外部进程，只能查看日志和状态，不能控制启停
  - 前端显示 "👁️ 只读" 标签标识附着模式服务器
  - 尝试控制附着服务器时显示明确的错误提示

### Fixed
- **接管权限混淆**：修复 attach 模式仍能控制外部进程的问题
  - 修复前：attach 模式名义上是"观察"，实际可以停止/重启外部进程
  - 修复后：严格权限检查，attached 模式服务器拒绝所有控制操作
  - 防止误操作 systemd 管理的进程，避免冲突

### Changed
- **接管提示信息**：更清晰地说明管理权限
  - attach 模式：明确提示"只读观察模式，面板无法控制启停"
  - 控制操作被拒：提示"如需控制，请先使用 restart 模式重新接管"

## [v1.5.3] - 2026-06-21

### Fixed
- **接管去重**：修复接管（adopt）功能重复创建服务器记录的问题
  - 接管同一 frpc 进程时，现在会检查是否已存在相同 `serverAddr:serverPort` 的服务器
  - 如果找到匹配的服务器，复用该记录而非创建新的（避免重复的代理规则）
  - 返回消息明确告知是"复用了现有服务器配置"还是"创建了新服务器"
  - 不同 frps 服务端入口仍会创建独立的服务器记录
  - 复用时不更新规则（保留用户自定义配置），如需更新规则请删除旧服务器后重新接管

## [v1.5.2] - 2026-06-21

### Added
- **systemd 管理检测**：接管进程前自动检测是否由 systemd 托管
  - 检查 `/proc/{pid}/cgroup` 和 `systemctl status` 提取服务单元名称
  - 在确认对话框中显示明确警告，提供具体的 `systemctl disable` 命令
  - 进程列表中显示 systemd 标签（黄色警告样式）
- **Admin API 配置检测**：解析配置文件检查是否启用了 admin API
  - 支持 TOML (`webServer`) 和 INI (`admin_addr/port`) 格式
  - 进程列表中显示 Admin API 状态标签（绿色=已配置，灰色=未配置）
  - 未配置时在警告中提示"面板将自动添加此配置"
- **增强的警告机制**：restart 模式接管前显示详细风险提示
  - systemd 托管警告：提示先停用服务避免冲突
  - Admin API 缺失警告：说明面板会自动添加配置
  - 操作完成后在成功消息中重申警告信息

### Changed
- **数据模型扩展**：`FRPCProcessCandidate` 新增字段
  - `systemdManaged`: 是否由 systemd 托管
  - `systemdUnit`: systemd 服务单元名称
  - `hasAdminApi`: 是否配置了 admin API
  - `adminApiAddress`: admin API 地址

## [v1.5.1] - 2026-06-21

### Fixed
- **拓扑页面文本截断**：为所有被截断的文本添加悬浮提示功能
  - 卡片标题（h3）：鼠标悬停显示完整内容
  - 卡片副标题（span）：鼠标悬停显示完整内容
  - 错误信息（.flow-live-err）：鼠标悬停显示完整错误
  - 添加 `cursor: help` 和悬停透明度变化提供视觉反馈
  - 使用原生 HTML `title` 属性，浏览器原生支持，无需额外依赖

## [v1.5.0] - 2026-06-21

### Added
- **发现和接管已有 frpc**：支持导入和纳管系统中已存在的 frpc
  - **二进制发现**：扫描 PATH 和常见安装目录，登记已安装的 frpc 无需重新下载
  - **进程发现**：扫描运行中的 frpc 进程，提取 PID、二进制路径和配置文件路径
  - **配置导入**：解析现有 frpc 配置文件（支持 TOML 和 INI 格式，v0.31~v0.70+）
  - **进程接管**：两种纳管模式
    - restart 模式：停止外部进程，由面板用导入的配置重新启动
    - attach 模式：附着到运行中的进程，退出后由面板接管自动重启
  - **前端 UI**：服务器页面顶部新增"接管已有 frpc"面板，扫描并显示发现的二进制和进程
  - **API 路由**：
    - `GET /api/frpc/discover` - 发现二进制和进程
    - `POST /api/frpc/register` - 登记系统二进制
    - `POST /api/servers/import-frpc` - 导入配置文件
    - `POST /api/servers/adopt` - 接管运行中进程
  - **安全机制**：
    - 纳管前重新扫描进程，以 PID 匹配取真实路径（不信任客户端传入）
    - restart 模式先 SIGTERM（5秒）再 SIGKILL，确保旧进程退出避免端口冲突
    - 配置解析支持常见字段名变体（如 INI 的 `authentication_token` / `token`）
  - **完整文档**：`ADOPT_EXISTING_FRPC.md` 详细说明使用方法和安全机制

## [v1.4.0]

### Security
- **导入配置校验二进制路径**：导入/恢复备份时，frpc 版本的 `path` 必须位于受管目录
  `<data>/bin/` 内且文件存在，否则跳过——防止恶意/共享的配置 bundle 把 `version.path`
  指向任意可执行文件、在启动时被执行。
- **安装脚本校验改为 fail-closed**：`install.sh` 无法下载 `SHA256SUMS`、清单中无对应记录、
  或缺少 `sha256sum`/`shasum` 时直接终止安装（不再 warn 后继续）；确需跳过可设
  `INSECURE_SKIP_CHECKSUM=1`。
- **发布签名**：一键自更新现在要求校验 `SHA256SUMS` 的 ed25519 分离签名，抵御恶意下载代理
  同时替换二进制与校验和；构建中未注入公钥时，控制台会禁用一键更新并提示使用安装脚本手动升级。
  新增 `cmd/release-sign` 辅助工具。
- **5xx 错误不再回传内部细节**：服务器内部错误只记入服务端日志，对客户端返回笼统文案，
  避免向已登录用户泄露文件路径等实现细节。

### Changed
- **同一服务器的启动/停止/重启/重载串行化**：新增按服务器维度的操作锁，
  防止并发请求拉起重复 frpc 进程或竞争配置文件。
- **状态文件落盘更安全**：`state.json` 原子写在 rename 前先 fsync，避免崩溃时
  把只写一半的内容 rename 成正式状态文件。
- **日志接口 `tail` 参数加上限**（≤5000 行），防止异常大的取值。

## [v1.3.0]

### Changed
- **认证流程改版**：移除「首次访问初始化页面」（bootstrap），改为出厂内置**初始密钥 + 首次登录强制改密**。
  系统始终存在初始密钥（默认 `FrpcWeb-Init-9527`，可用 `FRPC_WEB_ACCESS_KEY` 覆盖为自定义初始密钥），
  消除「未初始化实例在公网被抢先接管」的窗口。用初始密钥登录后必须设置自己的密码
  （8-20 位，须同时包含大写字母、小写字母和数字），设密后初始密钥立即失效，只能用新密码登录。
  `FRPC_WEB_ACCESS_KEY` 由「永久覆盖」改为「初始/一次性」语义，设密后即失效；改密不再被该环境变量锁定。
- 移除 `POST /api/auth/bootstrap` 接口与前端初始化页面。

### Security
- 仍持「初始密钥会话」的请求在改密前被服务端限制为只能调用改密接口（访问其余业务接口返回 403），
  让「强制改密」成为真正的服务端约束而非仅前端弹窗。
- 公网监听且仍在使用出厂默认密钥时，启动日志输出醒目安全告警，提示尽快改密或绑定回环地址。

## [v1.2.0]

### Added
- **自动备份**：设置页新增「自动备份」卡片，按设定频率（6 小时～每周）把完整配置快照写入数据目录
  `backups/`；内容与上次备份一致时自动跳过（哈希去重），超出保留份数（3～30）自动清理最旧的。
  支持立即备份、下载备份文件、一键恢复（替换语义，恢复前建议先备份当前状态）；
  创建/下载/恢复均计入审计日志。新增 API：`GET/POST /api/backups`、
  `GET /api/backups/{name}`、`POST /api/backups/{name}/restore`
- **拓扑图实时状态**：后端新增 `GET /api/proxies/status`，并发调用各运行中 frpc 实例的管理接口
  （`/api/status`，本机回环，3 秒超时）汇总每条代理的真实运行相位。拓扑页在页面可见时每 3 秒轮询：
  本地服务节点显示 运行中 / 启动失败 / 健康检查未通过 / 等待启动 徽标与错误原因，
  客户端节点显示「x/y 代理运行中」，右上角标记实时连接状态；轮询只刷新徽标不重建节点，
  手动拖拽的布局不受影响。frpc 管理接口不提供连接数与流量数据，相关展示暂不支持

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
