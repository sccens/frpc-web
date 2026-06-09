# FRPC Web

FRPC Web 是一个浏览器化的 `frpc` 管理器，用来管理 frpc 版本、服务器配置、代理规则、进程、日志、统计和配置备份。项目当前定位是 Linux first、单管理员、本机或内网部署优先。

支持 TCP、UDP、HTTP、HTTPS、STCP、XTCP 代理规则；默认通过生成 `frpc.toml` 和热重载来应用配置。

## 快速启动

```bash
git clone https://github.com/sccens/frpc-web.git
cd frpc-web
cd web && npm ci && npm run build
cd ..
go run ./cmd/frpc-web
```

打开 `http://127.0.0.1:8080`。如果没有设置 `FRPC_WEB_ACCESS_KEY`，首次打开会进入初始化页面。

## 配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `FRPC_WEB_ADDR` | `127.0.0.1:8080` | Web 监听地址 |
| `FRPC_WEB_DATA_DIR` | `frpc-web-data` | SQLite、frpc 二进制、配置和日志目录 |
| `FRPC_WEB_ACCESS_KEY` | 空 | 单管理员访问密钥 |
| `FRPC_WEB_GITHUB_PROXY` | 空 | 默认 GitHub 下载代理 |
| `FRPC_WEB_JWT_SECRET` | 自动生成 | JWT 签名密钥 |
| `FRPC_WEB_WEB_DIR` | 空 | 指定外部前端静态目录，通常只用于开发排障 |
| `FRPC_WEB_TRUSTED_PROXY` | 空 | 信任代理转发的客户端 IP：`1/true/yes/on` |

生产建议：

```bash
FRPC_WEB_ADDR=127.0.0.1:8080
FRPC_WEB_DATA_DIR=/opt/frpc-web/data
FRPC_WEB_ACCESS_KEY=change-me-to-a-long-random-key
FRPC_WEB_GITHUB_PROXY=
FRPC_WEB_JWT_SECRET=
FRPC_WEB_TRUSTED_PROXY=
```

需要让同一网络内的设备访问时，把监听地址改成 `0.0.0.0:8080`。公网暴露时建议放在 HTTPS 反向代理或隧道后面。

## systemd 部署

目标机需要 Go、Node.js、npm 和 make。

```bash
git clone https://github.com/sccens/frpc-web.git
cd frpc-web
cd web && npm ci
cd ..
make build
sudo scripts/install-linux.sh
```

安装脚本只复制已构建的二进制、创建 `frpc-web` 低权限用户、安装 systemd unit，并启用服务；不会拉取源码、安装构建工具或自动启动服务。

```bash
sudo nano /opt/frpc-web/frpc-web.env
sudo systemctl start frpc-web
sudo systemctl status frpc-web
```

保持 `127.0.0.1:8080` 时，远程访问可使用 SSH 隧道：

```bash
ssh -L 8080:127.0.0.1:8080 user@your-server
```

常用命令：

```bash
journalctl -u frpc-web -f
git pull && make build
sudo scripts/install-linux.sh
sudo systemctl restart frpc-web
sudo /opt/frpc-web/scripts/uninstall-linux.sh
sudo /opt/frpc-web/scripts/uninstall-linux.sh --purge-data
```

## Docker 部署

```bash
git clone https://github.com/sccens/frpc-web.git
cd frpc-web
docker compose up -d --build
```

默认 Compose 只绑定宿主机本地地址：

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

建议在 `docker-compose.yml` 中设置：

```yaml
environment:
  FRPC_WEB_ACCESS_KEY: "change-me-to-a-long-random-key"
```

Docker 中的 `127.0.0.1` 是容器自身。frpc 规则需要访问宿主机服务时，`localIP` 建议填 `host.docker.internal`。

## 开发检查

```bash
go test ./...
cd web && npm run build
make build
make release
```

## 限制

- 不内置 HTTPS
- 不支持多用户、RBAC 和服务器级授权
- 暂不支持 Windows
- Store API 模式仍是实验入口，默认建议使用 `toml_reload`
