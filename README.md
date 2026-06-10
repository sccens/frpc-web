# FRPC Web

浏览器化的 `frpc` 管理器：管理 frpc 版本、服务端配置、代理规则、进程、日志与流量统计，通过生成 `frpc.toml` 并热重载来应用配置。

**功能一览**

- frpc 版本管理（在线/离线安装、多版本切换）
- 服务器与代理规则管理（TCP / UDP / HTTP / HTTPS / STCP / XTCP）
- 实时日志、连接拓扑图、流量监控看板
- 配置导入导出、审计日志、进程自动重启
- 单管理员访问控制（Access Key + 会话 Cookie）、黑暗模式

**限制**：不内置 HTTPS（建议放反向代理后）；单管理员，无多用户/RBAC；不支持 Windows。

## 快速开始

发布产物是**单个静态二进制**（前端已内嵌，`CGO_ENABLED=0` 编译）：全新的 Linux/macOS 机器上不需要安装 Go、Node、数据库或任何运行库，下载即可运行。frpc 本体也无需预装——首次使用时在网页里在线下载，或离线上传 frp 的 tar.gz 包。

### 一键安装（Linux / macOS）

需要 `bash` 和 `curl`（或 `wget`）。脚本会自动识别系统和架构、校验 SHA256、安装到 `/usr/local/bin`；**Linux 上检测到 systemd 时会自动注册并启动系统服务（开机自启）**，装完即可用：

```bash
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash
```

macOS 或加了 `SKIP_SERVICE=1` 时只安装二进制，手动运行 `frpc-web` 启动。

升级直接重跑安装命令（配置会保留）；卸载：

```bash
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash -s -- --uninstall
# 加 --purge-data 连数据一起删除
```

### 手动安装

极简系统（如无 bash 的容器环境）可直接手动安装。从 [Releases](https://github.com/sccens/frpc-web/releases) 下载对应平台的二进制（Linux amd64/arm64、macOS Intel/Apple Silicon）：

```bash
chmod +x frpc-web_*
sudo mv frpc-web_* /usr/local/bin/frpc-web
frpc-web
```

### Docker Compose（本地构建镜像）

```bash
git clone https://github.com/sccens/frpc-web.git
cd frpc-web
docker compose up -d
```

启动后访问 `http://127.0.0.1:8080`，首次访问会进入初始化页面设置访问密钥（Access Key），之后即可登录管理。

## 配置

全部通过环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `FRPC_WEB_ADDR` | `127.0.0.1:8080` | Web 监听地址 |
| `FRPC_WEB_DATA_DIR` | `frpc-web-data` | 状态文件、frpc 二进制、配置和日志目录 |
| `FRPC_WEB_ACCESS_KEY` | 空 | 单管理员访问密钥 |
| `FRPC_WEB_GITHUB_PROXY` | 空 | 默认 GitHub 下载代理 |
| `FRPC_WEB_TRUSTED_PROXY` | 空 | 信任代理转发的客户端 IP：`1/true/yes/on` |
| `FRPC_WEB_WEB_DIR` | 空 | 外部前端静态目录，仅开发排障用 |

生产环境建议：

```bash
export FRPC_WEB_ADDR=127.0.0.1:8080
export FRPC_WEB_DATA_DIR=/opt/frpc-web/data
export FRPC_WEB_ACCESS_KEY=change-me-to-a-long-random-key
```

## 部署

### systemd（Linux）

一键安装已自动完成 systemd 配置（创建 `frpc-web` 系统用户、`/opt/frpc-web/frpc-web.env` 配置文件、注册并启动服务），无需额外步骤：

```bash
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash
```

常用命令：

```bash
systemctl status frpc-web        # 查看状态
journalctl -u frpc-web -f        # 查看日志
sudo nano /opt/frpc-web/frpc-web.env && sudo systemctl restart frpc-web  # 改配置并生效
```

从源码构建并安装为服务：`make install-linux`（等价于 `SOURCE_BIN=bin/frpc-web bash install.sh`）。更新版本：重跑安装命令即可，配置保留、服务自动重启。卸载：`make uninstall-linux` 或安装命令加 `--uninstall`。

### Docker 模式说明

frpc-web 会把 frpc **下载到容器内**并作为自己的子进程运行（持久化在 `/data` 卷，容器重启后自动拉起 `autoStart` 的服务器），不需要也不会管理宿主机上的 frpc：

- 穿透**宿主机**服务时，规则的 `localIP` 填 `host.docker.internal`（compose 已配置 host-gateway）；`127.0.0.1` 指容器自身。
- STCP/XTCP **visitor** 规则在容器内监听 `bindPort`，宿主机要访问需把 `bindAddr` 设为 `0.0.0.0` 并在 compose 中追加端口映射（或 Linux 下改用 `network_mode: host`）。
- 默认只绑定 `127.0.0.1:8080`，建议在 compose 的 `environment` 中设置 `FRPC_WEB_ACCESS_KEY`。

### 远程访问

- **SSH 隧道（推荐）**：`ssh -L 8080:127.0.0.1:8080 user@your-server`，本地访问 `http://localhost:8080`。
- **局域网**：`export FRPC_WEB_ADDR=0.0.0.0:8080`（务必设置 Access Key）。
- **公网**：放在 HTTPS 反向代理后，并设置 `FRPC_WEB_TRUSTED_PROXY=1` 信任转发头：

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

## 脚本一览

所有安装、升级、卸载操作统一由根目录的 `install.sh` 完成：

| 操作 | 命令 |
| --- | --- |
| 在线安装（自动配 systemd） | `curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh \| bash` |
| 只装二进制（不配服务） | 同上，前面加 `SKIP_SERVICE=1` |
| 安装本地构建的二进制 | `SOURCE_BIN=bin/frpc-web bash install.sh`（即 `make install-linux`） |
| 升级 | 重跑安装命令（配置保留，服务自动重启） |
| 卸载（保留数据） | 安装命令末尾加 `-s -- --uninstall`，本地执行则 `bash install.sh --uninstall` |
| 卸载（删除数据） | 再加 `--purge-data` |
| 自定义安装目录 | 前面加 `INSTALL_DIR=/path` |

开发用脚本：`scripts/build-release.sh` 构建全平台发布产物（linux/darwin × amd64/arm64）到 `dist/` 并生成 SHA256SUMS，通过 `make release` 调用，可用 `VERSION=` 注入版本号。

## 开发

要求 Go 1.26+、Node.js 20.19+（或 22+）。

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

## 许可证

MIT。基于 [fatedier/frp](https://github.com/fatedier/frp) 与 [Element Plus](https://element-plus.org/) 构建，欢迎提交 [Issue](https://github.com/sccens/frpc-web/issues) 和 Pull Request。

本项目在 [Claude](https://www.claude.com/product/claude-code) 与 [Codex](https://openai.com/codex/) 的协助下开发。
