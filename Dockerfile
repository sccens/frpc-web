# syntax=docker/dockerfile:1

FROM node:24-bookworm-slim AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-bookworm AS go-build
# go.mod 钉了补丁版本；允许按需下载匹配的工具链，避免镜像标签落后时构建失败
ENV GOTOOLCHAIN=auto
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -tags embed -trimpath -ldflags "-s -w" -o /out/frpc-web ./cmd/frpc-web

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --system frpc-web \
  && useradd --system --gid frpc-web --home-dir /app --shell /usr/sbin/nologin frpc-web \
  && mkdir -p /app /data \
  && chown -R frpc-web:frpc-web /app /data
COPY --from=go-build /out/frpc-web /usr/local/bin/frpc-web
USER frpc-web
WORKDIR /app
ENV FRPC_WEB_ADDR=0.0.0.0:8080
ENV FRPC_WEB_DATA_DIR=/data
ENV FRPC_WEB_GITHUB_PROXY=
ENV FRPC_WEB_ACCESS_KEY=
ENV FRPC_WEB_TRUSTED_PROXY=
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD curl -fsS http://127.0.0.1:8080/api/health >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/frpc-web"]
