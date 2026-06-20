# 接管已有 frpc 功能说明

本次改动为 frpc-web 增加了「接管已有 frpc」的三大能力，解决面板在「机器上已经跑着 frpc」场景下空白一片、无法利用现有配置与进程的问题。

---

## 三大能力

### 1. 识别已装的二进制
**场景**：机器上已通过 `apt`、官方安装脚本、或手动下载安装了 frpc，不想重复下载。

**功能**：
- 扫描 `PATH` 及常见安装目录（`/usr/local/bin`、`/usr/bin`、`/opt/frp` 等）
- 对每个候选执行 `frpc --version` 验证其可用性
- 一键「登记并启用」，面板后续启动服务器时直接 exec 该二进制

**位置**：服务器页面 → 「接管已有 frpc」面板 → 「扫描系统」→ 已安装的 frpc 二进制列表

---

### 2. 导入现有配置
**场景**：已有一份运行良好的 `frpc.toml`（或旧版 `.ini`），想直接导入到面板管理，而不是手动重新录入服务器地址、每条规则。

**功能**：
- 解析 frp v0.52+ 的 TOML 格式与旧版 INI 格式
- 自动提取：
  - 服务器配置：`serverAddr`、`serverPort`、`auth.token`、传输协议、admin 端口/凭据
  - 全部代理规则：TCP/UDP/HTTP/HTTPS/STCP/XTCP，含 visitor 规则
  - 高级选项：加密、压缩、带宽限制、自定义域名、请求头、locations 等
- 导入后生成一台新服务器（含其全部规则），不会自动启动进程

**位置**：服务器页面 → 「接管已有 frpc」面板 → 「导入配置」→ 粘贴配置原文或从文件载入

**容错**：
- 无法识别的 TOML 键一律忽略（而非报错），最终结果由 `validateServer`/`validateRule` 校验
- 支持跨版本（v0.31 ~ v0.70+）的常见字段名变体（如 INI 的 `authentication_token` / `token`）

---

### 3. 接管运行中的进程
**场景**：机器上已经有一个正在跑的 frpc（systemd/supervisor 托管、或手动启动），想把它纳入面板管理（能看日志、热重载、自动重启）。

**功能**：
- 扫描当前运行的 frpc 进程（Linux 读 `/proc`，其他平台用 `ps`）
- 提取其 PID、二进制路径、`-c` 指定的配置文件路径
- 两种纳管模式：
  - **重启接管**（默认）：停掉外部进程 → 面板读配置导入为服务器 → 面板用同一份配置重新拉起。纳管后日志、热重载、自动重启能力完整，代价是隧道会短暂重连（~2 秒）。
  - **直接附着**：不重启，直接把进程记进面板。隧道零中断，但面板拿不到原始 stdout 日志（进程退出后面板才接管自动重启）。

**位置**：服务器页面 → 「接管已有 frpc」面板 → 「扫描系统」→ 正在运行的 frpc 进程列表

**安全**：
- 纳管前会重新扫描进程，以用户提交的 PID 匹配，取其真实的二进制路径与配置路径（不信任客户端传入的路径），防止任意路径读取。
- 重启模式会先 `SIGTERM`（优雅退出 5 秒）再 `SIGKILL`，确保旧进程真正退出后才拉起新进程，避免 admin 端口冲突。

**注意事项**：
- 若原进程由 systemd/supervisor 托管，纳管后请停用其服务单元（`systemctl disable --now frpc`），否则会被重新拉起并与面板实例冲突。

---

## 实现架构

### 后端（Go）
**新增文件**：
- `internal/app/frpcimport.go`：配置解析器（TOML/INI → `ServerInput` + `[]ProxyRuleInput`），导入与纳管的服务层逻辑
- `internal/app/frpcimport_test.go`：解析器与导入/纳管的单元测试
- `internal/frpc/discover.go`：扫描系统二进制与运行中进程（Linux `/proc`、其他平台 `ps`），登记二进制

**修改文件**：
- `internal/app/models.go`：新增类型 `FRPCDiscovery`、`FRPCBinaryCandidate`、`FRPCProcessCandidate`、`ImportFrpcConfigInput`、`AdoptProcessInput`、`RegisterBinaryInput`、`AdoptResult`
- `internal/app/service.go`：`Runtime` 接口新增 `DiscoverBinaries()`、`DiscoverProcesses()`、`RegisterBinary()`；新增服务方法 `DiscoverFRPC()`、`ImportFrpcConfig()`、`AdoptProcess()`、`RegisterBinary()`
- `internal/app/service_test.go`：`fakeRuntime` 补齐新接口的桩实现
- `internal/server/server.go`：新增路由 `GET /api/frpc/discover`、`POST /api/frpc/register`、`POST /api/servers/import-frpc`、`POST /api/servers/adopt`
- `internal/server/server_test.go`：`serverFakeRuntime` 补齐新接口的桩实现

**特点**：
- **零外部依赖**：项目原本只依赖 `golang.org/x/crypto`，此次未引入 TOML 库，自己实现了针对 frpc 配置子集的解析器（~300 行，支持引号、数组、嵌套表）。
- **进程发现**：Linux 直接读 `/proc/<pid>/cmdline` 和 `/proc/<pid>/exe`（高效、可靠），其他平台退回 `ps -axww`。
- **配置解析宽松**：遇到无法识别的键直接跳过（而非报错），最终由已有的 `validateServer`/`validateRule` 校验，保证「部分识别」的配置也能以普通校验错误形式暴露，而不是 panic。

### 前端（Vue 3 + TypeScript）
**修改文件**：
- `web/src/api/client.ts`：新增类型与 API 函数 `discoverFrpc()`、`registerFrpcBinary()`、`importFrpcConfig()`、`adoptFrpcProcess()`
- `web/src/pages/ServersPage.vue`：
  - 新增「接管已有 frpc」面板（页面顶部）：扫描按钮 → 二进制列表 + 进程列表 + 纳管模式选择器
  - 新增「导入配置」抽屉：textarea 粘贴配置 / 从文件载入，设置节点名称与自动启动选项
  - 复用已有 CSS 类（`surface-panel`、`version-registry`、`session-row`、`rule-drawer` 等），未引入新样式

**构建**：
- `npm run build` 重建 `web/dist`（ServersPage bundle 从 ~22KB 涨到 25.5KB）
- Go `-tags embed` 编译后二进制大小 11MB（内嵌前端）

---

## 测试覆盖

### 单元测试
- **配置解析器**：
  - `TestImportFrpcConfigTOML`：TOML 格式（含嵌套表、数组、多 proxy/visitor）
  - `TestImportFrpcConfigINI`：旧版 INI 格式（含 `[common]`、`header_` 前缀）
  - `TestImportFrpcConfigRejectsGarbage`：拒绝无效输入
- **发现与纳管**：
  - `TestDiscoverFRPCPassesThrough`：扫描结果透传，已纳管进程标注 `Managed`
  - `TestAdoptProcessRestart`：重启模式纳管（需先植入激活版本）
  - `TestAdoptProcessMissingPID`：拒绝不存在的 PID

### 集成测试
- 编译带 embed 的二进制，启动服务，验证：
  - `/api/health` 200
  - `/api/frpc/discover` 401（正确走认证）
  - `/` 加载前端 `index.html`

---

## 使用流程示例

### 场景 A：已有 frpc 二进制，想导入配置
1. 服务器页面 → 「接管已有 frpc」→ 点击「扫描系统」
2. 在「已安装的 frpc 二进制」列表中找到 `/usr/local/bin/frpc`，点击「登记并启用」
3. 点击「导入配置」，粘贴现有的 `frpc.toml` 内容，点击「导入为服务器」
4. 面板生成一台新服务器（含全部规则），点击「启动」即用刚登记的二进制拉起

### 场景 B：已有运行中的 frpc，零中断纳管
1. 服务器页面 → 「接管已有 frpc」→ 点击「扫描系统」
2. 在「正在运行的 frpc 进程」列表中找到目标 PID
3. 纳管模式选择「直接附着（零中断）」，点击「纳管」
4. 面板读取其配置文件并导入为服务器，附着到现有进程（隧道不中断）
5. 进程退出后面板会按导入的配置自动重启

### 场景 C：完整托管已有进程（推荐）
1. 服务器页面 → 「接管已有 frpc」→ 点击「扫描系统」
2. 在「正在运行的 frpc 进程」列表中找到目标 PID
3. 纳管模式选择「重启接管（完整托管）」，点击「纳管」
4. 面板停掉旧进程 → 导入配置 → 重新拉起（隧道短暂重连 ~2 秒）
5. 纳管后可在面板内看日志、热重载、自动重启

---

## 代码统计

- **新增**：~800 行 Go（解析器 + 发现 + 纳管逻辑）、~150 行 TypeScript、~120 行 Vue 模板
- **测试**：9 个新增测试用例（全绿）
- **编译验证**：`go build`、`go vet`、`go test ./...`、`npm run build` 全部通过

---

## 兼容性

- **frp 版本**：支持 v0.31 ~ v0.70+（旧版 INI 与新版 TOML）
- **操作系统**：
  - 进程发现：Linux（`/proc`）、macOS/BSD（`ps`）
  - 二进制扫描：跨平台（PATH + 常见目录）
- **向后兼容**：未改动已有 API 签名，旧客户端不受影响

---

## 安全考虑

1. **路径校验**：
   - 纳管进程时，重新扫描并以 PID 匹配取真实路径，不信任客户端传入的路径
   - 登记二进制时，先 `os.Stat` 验证文件存在、可执行、能跑 `--version`

2. **配置解析**：
   - 解析器只提取已知字段，无法识别的键一律忽略
   - 最终由 `validateServer`/`validateRule` 校验，防止恶意配置绕过业务规则

3. **进程操作**：
   - 停止进程用 `SIGTERM`（优雅）→ `SIGKILL`（兜底），等待真正退出后才拉起新进程
   - `Adopt()` 机制保证进程退出后触发自动重启（与手动 Start 一致）

---

## 后续改进方向（可选）

1. **批量导入**：支持一次导入多个配置文件（如 `frpc-*.toml`）
2. **systemd 集成**：检测进程是否由 systemd 托管，纳管时自动 `systemctl disable`
3. **配置 diff**：纳管前对比「面板将生成的配置」与「当前运行的配置」，显示差异
4. **导入预览**：解析配置后先显示即将创建的服务器与规则，确认后再保存

---

**功能已全部实现并通过测试，可直接使用。**
