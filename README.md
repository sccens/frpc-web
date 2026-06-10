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

### 一键安装（Linux / macOS）

```bash
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash
frpc-web
```

### 手动安装

从 [Releases](https://github.com/sccens/frpc-web/releases) 下载对应平台的二进制（Linux amd64/arm64、macOS Intel/Apple Silicon）：

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

```bash
# 下载预编译二进制（安装到 /usr/local/bin）
curl -fsSL https://raw.githubusercontent.com/sccens/frpc-web/main/install.sh | bash

# 获取仓库（安装脚本和 systemd 服务文件在仓库内）
git clone https://github.com/sccens/frpc-web.git
cd frpc-web
sudo SOURCE_BIN=/usr/local/bin/frpc-web ./scripts/install-linux.sh

# 按需编辑 /opt/frpc-web/frpc-web.env，然后启动
sudo systemctl start frpc-web
```

查看日志：`journalctl -u frpc-web -f`。更新版本：重跑 install.sh 后 `sudo systemctl restart frpc-web`。卸载：`sudo /opt/frpc-web/scripts/uninstall-linux.sh`（加 `--purge-data` 同时删除数据）。

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
