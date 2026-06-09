# FRPC Web

FRPC Web 是一个浏览器化的 `frpc` 管理器，目标是让个人用户在 Web 控制台里完成 frpc 安装、服务器配置、代理规则管理、进程启停、日志查看和基础运维。

当前定位是 **Linux first / 单管理员 / 本机部署优先**。项目默认使用单个 Access Key 登录，不提供多用户和 RBAC 管理页。

## 功能概览

- 多服务器管理：新增、编辑、删除 FRP 服务器配置，支持自动启动、状态展示和崩溃自愈。
- 代理规则：支持 TCP、UDP、HTTP、HTTPS、STCP、XTCP，表单配置后生成 `frpc.toml`。
- 高级参数：支持加密、压缩、带宽限制；HTTP/HTTPS 支持 locations、Host Header Rewrite、请求头和 Basic Auth。
- frpc 版本管理：设置页内支持检查最新版本、在线安装、离线上传、切换默认版本；在线安装会校验官方 checksum。
- 进程管理：由 `frpc-web` 托管 frpc 子进程，支持启动、停止、重启、配置检查和热重载。
- 日志查看：按服务器读取最近日志，默认最近 200 行，支持自动刷新；frpc 日志自动轮转。
- 统计页：通过 frpc 本地 Admin API 聚合服务器、代理状态、错误和可用流量字段。
- 配置备份：支持导出和导入服务器、规则、基础设置和版本引用，默认脱敏敏感字段。
- 审计日志：记录登录、登出、密钥修改、设置修改、配置导入导出、服务器/规则变更和 frpc 版本操作。
- 部署方式：支持单二进制、systemd、Docker 和 Docker Compose。

## 快速开始

开发环境直接运行：

```bash
git clone git@github.com:sccens/frpc-web.git
cd frpc-web
cd web && npm install && npm run build
cd ..
go run ./cmd/frpc-web
```

打开：

```text
http://127.0.0.1:8080
```

如果没有配置 `FRPC_WEB_ACCESS_KEY`，首次打开会进入初始化页面，创建一个 Access Key。之后登录只需要输入这个 Access Key。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `FRPC_WEB_ADDR` | `127.0.0.1:8080` | Web 监听地址。生产默认建议只监听本机。 |
| `FRPC_WEB_DATA_DIR` | `frpc-web-data` | SQLite、frpc 二进制、生成配置、日志等数据目录。 |
| `FRPC_WEB_ACCESS_KEY` | 空 | 单管理员访问密钥。设置后页面无需初始化，环境变量优先于数据库。 |
| `FRPC_WEB_GITHUB_PROXY` | 空 | 默认 GitHub 下载代理，设置页保存值和本次安装代理会覆盖它。 |
| `FRPC_WEB_JWT_SECRET` | 自动生成 | JWT 签名密钥；为空时写入 SQLite 设置表。 |
| `FRPC_WEB_WEB_DIR` | 空 | 开发或排障时指定外部前端静态目录；生产默认使用内嵌前端。 |
| `FRPC_WEB_TRUSTED_PROXY` | 空 | 设置为 `1/true/yes/on` 后信任 `X-Forwarded-For` / `X-Real-IP`。 |

GitHub 代理优先级：

```text
本次在线安装输入 > 设置页默认代理 > FRPC_WEB_GITHUB_PROXY > 直连 GitHub
```

如果把 `FRPC_WEB_ADDR` 改成 `0.0.0.0:8080`，登录认证仍然生效，但生产环境仍建议叠加 HTTPS、反向代理认证或网络访问控制。

## Linux 生产部署

生产部署默认使用 `/opt/frpc-web` 单目录布局：

```text
/opt/frpc-web/bin/frpc-web
/opt/frpc-web/data
/opt/frpc-web/frpc-web.env
/opt/frpc-web/scripts
```

在构建机或目标机安装 Go、Node.js 和 make 后执行：

```bash
git clone git@github.com:sccens/frpc-web.git
cd frpc-web
make build
sudo scripts/install-linux.sh
```

按需编辑环境变量：

```bash
sudo nano /opt/frpc-web/frpc-web.env
```

常用生产配置示例：

```bash
FRPC_WEB_ADDR=127.0.0.1:8080
FRPC_WEB_DATA_DIR=/opt/frpc-web/data
FRPC_WEB_ACCESS_KEY=change-me-to-a-long-random-key
FRPC_WEB_GITHUB_PROXY=https://proxyd.sccens.eu.cc/
FRPC_WEB_JWT_SECRET=
FRPC_WEB_TRUSTED_PROXY=
```

启动并设置开机自启：

```bash
sudo systemctl start frpc-web
sudo systemctl status frpc-web
```

安装脚本会创建专用低权限用户 `frpc-web`，安装 systemd unit，并执行 `systemctl enable frpc-web.service`。

查看日志：

```bash
journalctl -u frpc-web -f
```

升级：

```bash
git pull
make build
sudo scripts/install-linux.sh
sudo systemctl restart frpc-web
```

卸载但保留数据：

```bash
sudo /opt/frpc-web/scripts/uninstall-linux.sh
```

卸载并删除数据：

```bash
sudo /opt/frpc-web/scripts/uninstall-linux.sh --purge-data
```

## Docker 部署

Docker 构建不要求宿主机安装 Go 或 Node.js：

```bash
git clone git@github.com:sccens/frpc-web.git
cd frpc-web
docker compose up -d --build
```

默认 Compose 只绑定宿主机本地地址：

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

容器内默认环境变量：

```bash
FRPC_WEB_ADDR=0.0.0.0:8080
FRPC_WEB_DATA_DIR=/data
FRPC_WEB_GITHUB_PROXY=
FRPC_WEB_ACCESS_KEY=
FRPC_WEB_JWT_SECRET=
FRPC_WEB_TRUSTED_PROXY=
```

建议在 `docker-compose.yml` 中设置 Access Key：

```yaml
environment:
  FRPC_WEB_ACCESS_KEY: "change-me-to-a-long-random-key"
  FRPC_WEB_GITHUB_PROXY: "https://proxyd.sccens.eu.cc/"
```

数据保存在命名卷 `frpc-web-data` 的 `/data` 中。查看状态和日志：

```bash
docker compose ps
docker compose logs -f
```

升级：

```bash
git pull
docker compose up -d --build
```

停止：

```bash
docker compose down
```

如果 Docker 内的 frpc 要访问宿主机服务，不要把代理规则的 `localIP` 写成 `127.0.0.1`。在 Docker 中，`127.0.0.1` 是容器自身。Compose 已配置：

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

因此宿主机服务建议这样填写：

```text
localIP = host.docker.internal
localPort = 3000
```

## 数据目录

默认数据目录结构大致如下：

```text
<dataDir>/app.db
<dataDir>/bin/frpc/<version>/frpc
<dataDir>/servers/<serverID>/frpc.toml
<dataDir>/logs/<serverID>/frpc.log
```

权限约束：

- 数据目录：`0700`
- SQLite 数据库：`0600`
- 生成的 `frpc.toml`：`0600`
- frpc 子进程日志：`0600`

systemd 部署时，数据目录归 `frpc-web:frpc-web` 所有，服务使用 `UMask=0077`。

## 认证与安全

- 当前是单管理员模型，只区分“已登录 / 未登录”。
- Access Key 最小长度为 8，建议使用长随机字符串。
- 登录 Cookie 使用 `HttpOnly` 和 `SameSite=Lax`，有效期默认 12 小时。
- JWT 内包含服务端 session id，后端会同时校验 JWT 签名和 SQLite 中的活跃会话。
- 修改 Access Key 会撤销全部会话。
- 登录和初始化接口有内存级失败限流，同一来源连续失败 5 次后会临时锁定。
- 审计中的 `owner/admin` 是内部单管理员身份标记，不代表当前支持多用户。

公网暴露建议：

- 优先保持 `FRPC_WEB_ADDR=127.0.0.1:8080`。
- 通过 Nginx、Caddy、Cloudflare Tunnel 等提供 HTTPS。
- 如果启用 `FRPC_WEB_TRUSTED_PROXY=1`，必须确保 frpc-web 只接收可信反向代理流量。

## frpc 配置模式

默认模式是 `toml_reload`：

- 数据库是主数据源。
- 保存配置后生成 `frpc.toml`。
- 配置检查使用 `frpc verify -c`。
- 运行中代理规则变更使用 `frpc reload -c`。

`store_api` 已降级为实验入口：

- 依赖 frpc 本地 Admin API 和 Store API。
- STCP/XTCP 不支持 Store API 模式。
- 普通用户建议继续使用默认的 `toml_reload`。

修改公共配置，例如 frps 地址、端口、token、传输协议、Admin 端口，会标记为需要重启。

## 配置导入导出

设置页提供配置导出和导入：

- 默认导出会脱敏 frps token、frpc Admin 密码、STCP/XTCP `secretKey` 和 HTTP Basic Auth 密码。
- 勾选“包含敏感信息”后会二次确认，适合完整迁移。
- 导入支持 `merge` 和 `replace`。
- 导出内容包含服务器、规则、基础设置和版本引用，不包含数据库文件、日志和已安装 frpc 二进制本体。

## 开发与检查

安装前端依赖：

```bash
cd web
npm install
```

构建前端：

```bash
cd web
npm run build
```

运行后端：

```bash
go run ./cmd/frpc-web
```

常用提交前检查：

```bash
go test ./...
cd web && npm run build
make build
```

生成 Linux amd64/arm64 release 二进制：

```bash
make release
```

## 当前限制

- 不内置 HTTPS，推荐使用反向代理或 Cloudflare Tunnel。
- 不支持多用户、RBAC、服务器级授权。
- 暂不支持 Windows。
- Docker 模式下 `127.0.0.1` 指向容器自身，不是宿主机。
- Store API 是实验功能，不建议作为默认配置模式。
- 统计流量只读取 frpc Admin API 实际返回字段，不伪造流量数据。

## 已完成与待补齐

已完成：

- SQLite 持久化、单 Access Key 初始化、JWT + 服务端会话、登录失败限流、可信代理 IP 开关。
- 多服务器、TCP/UDP/HTTP/HTTPS/STCP/XTCP、代理高级参数、配置预览、TOML 热重载。
- frpc 在线 checksum 校验安装、离线上传安装、GitHub 代理、版本检查和默认版本切换。
- frpc Admin API 自动凭据、子进程启停、崩溃自愈、日志自动刷新、日志轮转、反向 tail 读取。
- 统计页、审计日志、配置导入导出、Vue 控制台、单二进制构建、systemd 脚本、Dockerfile 和 Compose。

待补齐：

- 内置 HTTPS 或更完整的反向代理示例。
- 审计日志 CSV/JSON 导出。
- Docker 镜像发布流程。
- 更多 frp 能力，例如 SUDP、插件和复杂 visitor 场景。
