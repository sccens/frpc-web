# FRPC Web

浏览器化的 `frpc` **配置监控面板**:扫描磁盘上已有的 frpc 配置文件,只读展示服务器与代理规则、连接拓扑、实时状态,并支持在面板内编辑配置文件原文、通过 frpc admin API 触发热重载。

**v2.0 起,frpc-web 不再管理 frpc 进程**(启动/停止/重启、版本安装、接管外部进程等能力已移除)。进程生命周期交由 systemd/supervisor 管理,面板专注于监控与可视化——职责单一,符合 Unix 哲学。

**功能一览**

- 配置文件扫描:自动发现 `/etc/frpc`、`/usr/local/etc/frpc`、数据目录及自定义路径下的 frpc 配置(TOML/INI),一文件一实例
- 只读展示:服务器节点、代理规则(TCP/UDP/HTTP/HTTPS/STCP/XTCP)
- 连接拓扑图:本地服务 → frpc → frps → 公网入口,带专属配色与实时状态徽标
- 实时状态:后台探测各实例 admin API `/api/status`,显示每条代理的运行相位
- 配置编辑:面板内编辑配置文件原文,原子写入(需可写部署);不可写时降级为只读 + 下载
- 热重载:通过 frpc admin API `GET /api/reload` 让 frpc 重读配置
- 日志查看:从配置的 `log.to` 解析日志路径并读取
- 配置导出/导入、frpc-web 自身一键更新
- 单管理员访问控制(Access Key + 会话 Cookie)、强制改密、登录限流、黑暗模式

**限制**:不内置 HTTPS(建议放反向代理后);单管理员,无多用户/RBAC;不支持 Windows;不管理 frpc 进程(需自行用 systemd 等管理)。

## 快速开始

发布产物是**单个静态二进制**(前端已内嵌,`CGO_ENABLED=0` 编译):Linux/macOS 机器上下载即可运行,无需安装 Go、Node、数据库或运行库。**frpc 本体需要你自行安装并用 systemd 管理**(见下文「部署 frpc」)。

### 一键安装 frpc-web(Linux / macOS)

需要 `bash` 和 `curl`(或 `wget`)。脚本自动识别系统和架构、校验 SHA256、安装到 `/usr/local/bin`;Linux 上检测到 systemd 时自动注册并启动 frpc-web 服务。

```bash
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash
```

macOS 或加了 `SKIP_SERVICE=1` 时只安装二进制,手动运行 `frpc-web` 启动。升级重跑安装命令(面板自身设置保留);卸载:

```bash
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash -s -- --uninstall
# 加 --purge-data 连数据一起删除
```

启动后访问 `http://127.0.0.1:8080`,用**初始密钥**登录(出厂默认 `FrpcWeb-Init-9527`,可用 `FRPC_WEB_ACCESS_KEY` 覆盖)。**首次登录后强制设置自己的密码**(8-20 位,含大写字母、小写字母和数字);设置完成初始密钥立即失效。

## 配置

全部通过环境变量:

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `FRPC_WEB_ADDR` | `127.0.0.1:8080` | Web 监听地址 |
| `FRPC_WEB_DATA_DIR` | `frpc-web-data` | 状态文件与日志目录 |
| `FRPC_WEB_CONFIG_PATH` | 空 | 要监控的 frpc 配置文件路径(PATH 分隔符分隔多个,可为文件或目录);留空则扫描默认路径 |
| `FRPC_WEB_ACCESS_KEY` | 空(回退内置默认 `FrpcWeb-Init-9527`) | 自定义**初始密钥**:仅用于首次登录,设密后即失效 |
| `FRPC_WEB_RESET_KEY` | 空 | 设为 `1` 启动时清空已设密码并退出(忘记密码时用) |
| `FRPC_WEB_GITHUB_PROXY` | 空 | 自更新下载代理 |
| `FRPC_WEB_TRUSTED_PROXY` | 空 | 信任代理转发的客户端 IP:`1/true/yes/on` |
| `FRPC_WEB_WEB_DIR` | 空 | 外部前端静态目录,仅开发排障用 |

面板默认扫描以下路径(找到的第一个非空配置即作为一个实例,支持多文件):

1. `FRPC_WEB_CONFIG_PATH` 指定的路径(优先)
2. `/etc/frpc/*.toml`、`/etc/frpc/*.ini`
3. `/usr/local/etc/frpc/*.toml`、`*.ini`
4. 数据目录下的 `*.toml`、`*.ini`

## 部署 frpc(systemd 管理)

面板只监控,frpc 进程要你自己装并管理。典型流程:

```bash
# 1. 安装 frpc
wget https://github.com/fatedier/frp/releases/download/v0.60.0/frp_0.60.0_linux_amd64.tar.gz
tar -xzf frp_0.60.0_linux_amd64.tar.gz
sudo mv frp_0.60.0_linux_amd64/frpc /usr/local/bin/

# 2. 写配置(务必启用 admin API,否则面板拿不到实时状态、也无法热重载)
sudo mkdir -p /etc/frpc
sudo vim /etc/frpc/frpc.toml
```

配置示例(关键是 `webServer` 段):

```toml
serverAddr = "frps.example.com"
serverPort = 7000
auth.method = "token"
auth.token = "your-token"

# admin API(面板依赖它获取状态与热重载)
webServer.addr = "127.0.0.1"
webServer.port = 7400
webServer.user = "admin"
webServer.password = "secure-password"

# 日志写到文件,面板才能读取
log.to = "/var/log/frpc.log"

[[proxies]]
name = "ssh"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000
```

创建 systemd 服务:

```ini
# /etc/systemd/system/frpc.service
[Unit]
Description=frpc Client Service
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/frpc -c /etc/frpc/frpc.toml
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now frpc
```

frpc-web 默认监听 `127.0.0.1:8080`。远程访问见下文「远程访问」。

## frps 流量监控

frpc-web 可同时接入多个 frps 的 Prometheus 指标。先在每台 frps 启用 Dashboard 与 Prometheus:

```toml
webServer.addr = "127.0.0.1"
webServer.port = 7500
webServer.user = "admin"
webServer.password = "secure-password"
enablePrometheus = true
```

然后在 Web 控制台「流量」页添加 frps Dashboard 地址,例如 `http://127.0.0.1:7500`。frpc-web 会定时请求 `/metrics`,展示在线状态、实时入站/出站速率、累计流量、连接数与 proxy 流量排行。多个 frps 会按目标分别采集并在总览中聚合。

## 在面板内编辑配置(可选)

默认部署下,frpc-web 进程对 `/etc/frpc` **不可写**(systemd `ProtectSystem=strict`)。若想在面板内直接编辑配置文件并保存,安装时加开关:

```bash
ALLOW_CONFIG_EDIT=1 curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash
```

该开关会把配置目录(默认 `/etc/frpc`)加入 systemd `ReadWritePaths` 并授权 `frpc-web` 组写。保存配置后,点卡片上的「热重载」让 frpc 重读(需启用 admin API),或重启 frpc 服务。未开启可写时,编辑器降级为只读 + 下载。

> **热重载语义**:frpc 的 admin `/api/reload` 重载的是 **frpc 启动时 `-c` 指定的配置文件**。面板编辑的路径需与之一致,否则 reload 不生效。用上面的 systemd 单元(`-c /etc/frpc/frpc.toml`)与默认扫描路径一致,无此问题。

## 忘记密码

使用 `FRPC_WEB_RESET_KEY=1` 启动可清空访问密钥:

```bash
sudo systemctl stop frpc-web
FRPC_WEB_RESET_KEY=1 /usr/local/bin/frpc-web
sudo systemctl start frpc-web
```

重启后用初始密钥登录并重新设置密码。

## 安全须知

- **内置初始密钥 `FrpcWeb-Init-9527` 是公开的**,仅为方便首次本地登录。任何非 localhost 暴露必须设自定义 `FRPC_WEB_ACCESS_KEY` 或在首登后立即完成强制改密。
- 不内置 HTTPS:公网部署务必放在 HTTPS 反向代理之后,并设 `FRPC_WEB_TRUSTED_PROXY=1`。
- 配置导出/备份文件含**明文**的 frp token、admin 与 HTTP 密码,请妥善保管。

## 远程访问

- **SSH 隧道(推荐)**:`ssh -L 8080:127.0.0.1:8080 user@your-server`
- **局域网**:`export FRPC_WEB_ADDR=0.0.0.0:8080`(务必设置 Access Key)
- **公网**:放 HTTPS 反向代理后并设 `FRPC_WEB_TRUSTED_PROXY=1`:

```nginx
server {
    listen 443 ssl http2;
    server_name frpc.example.com;
    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

## Docker

```bash
git clone https://github.com/sccens/frpc-web.git
cd frpc-web
docker compose up -d --build
```

v2.0 的容器只运行 frpc-web 面板,**不再在容器内跑 frpc**。若要监控宿主机上的 frpc,把配置目录挂入容器(`docker-compose.yml` 里有注释示例),并注意:frpc 的 admin API 默认绑 `127.0.0.1`,容器无法访问宿主机回环——需把 frpc 的 `webServer.addr` 改为容器可达地址,否则面板只能展示配置、拿不到实时状态。

## 脚本一览

| 操作 | 命令 |
| --- | --- |
| 在线安装(自动配 systemd) | `curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh \| bash` |
| 安装并启用配置可写 | 前面加 `ALLOW_CONFIG_EDIT=1` |
| 只装二进制(不配服务) | 同上,前面加 `SKIP_SERVICE=1` |
| 安装本地构建的二进制 | `SOURCE_BIN=bin/frpc-web bash install.sh`(即 `make install-linux`) |
| 升级 | 重跑安装命令(设置保留,服务自动重启) |
| 卸载(保留数据) | 加 `-s -- --uninstall` |
| 卸载(删除数据) | 再加 `--purge-data` |

开发用脚本:`scripts/build-release.sh` 构建全平台发布产物(linux/darwin × amd64/arm64)到 `dist/` 并生成 SHA256SUMS,通过 `make release` 调用,可用 `VERSION=` 注入版本号。

### 发布签名(维护者)

一键自更新要求发布产物同时提供 `SHA256SUMS.sig`,并在构建中注入 ed25519 公钥；未配置公钥时,Web 控制台会禁用一键更新并提示使用安装脚本手动升级。这可以抵御恶意下载代理同时替换二进制与校验和。

1. 本地生成密钥对:`go run ./cmd/release-sign keygen`
2. 把输出的 `PRIVATE`(seed)存为 GitHub 仓库 Secret `RELEASE_SIGNING_KEY`(**切勿提交进仓库**)。
3. 把 `PUBLIC` 填入 `internal/app/selfupdate.go` 的 `releaseSigningPublicKey`(或构建时用 `-ldflags "-X github.com/sccens/frpc-web/internal/app.releaseSigningPublicKey=<base64>"` 注入),提交。
4. 之后打 tag 发布时,工作流自动用该私钥生成并上传 `SHA256SUMS.sig`。

## 开发

要求 Go 1.26+、Node.js 20.19+(或 22+)。

```bash
# 构建（前端 + 后端单二进制）
cd web && npm ci && npm run build && cd ..
make build && ./bin/frpc-web

# 开发模式：两个终端
go run ./cmd/frpc-web      # 后端
cd web && npm run dev      # 前端，自动代理 /api 到后端

# 测试与发布
go test ./...
make test
make release
```

## 从 v1.5.x 迁移到 v2.0

v2.0 是**破坏性变更**,不管理进程、不兼容旧 `state.json` 的托管数据。

1. 升级前,在 v1.5.x 的「设置 → 导出配置」备份现有 frpc 配置。
2. 用 systemd 部署 frpc(见上文),配置里启用 `webServer` admin API 段。
3. 升级 frpc-web(重跑安装命令)。旧 `state.json` 会自动备份为 `state.json.v1.bak`。
4. 把 frpc 配置放到扫描路径(`/etc/frpc/frpc.toml` 等),面板自动发现。

如果你仍需要进程管理、版本安装、接管外部 frpc 等 v1.x 能力,**请继续使用 v1.5.x LTS**——它仍会收到 bug 修复。

## 许可证

MIT。基于 [fatedier/frp](https://github.com/fatedier/frp) 与 [Element Plus](https://element-plus.org/) 构建,欢迎提交 [Issue](https://github.com/sccens/frpc-web/issues) 和 Pull Request。

本项目在 [Claude](https://www.claude.com/product/claude-code) 与 [Codex](https://openai.com/codex/) 的协助下开发。
