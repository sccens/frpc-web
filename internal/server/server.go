package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sccens/frpc-web/internal/app"
)

type Options struct {
	Service           *app.Service
	Logger            *slog.Logger
	WebDir            string
	WebFS             http.FileSystem
	TrustProxyHeaders bool
}

func New(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	mux := http.NewServeMux()
	api := apiHandler{
		service:           opts.Service,
		logger:            opts.Logger,
		limiter:           newLoginLimiter(),
		trustProxyHeaders: opts.TrustProxyHeaders,
	}

	mux.HandleFunc("GET /api/health", api.health)
	mux.HandleFunc("GET /api/auth/status", api.authStatus)
	mux.HandleFunc("POST /api/auth/login", api.login)
	mux.HandleFunc("POST /api/auth/logout", api.logout)
	mux.HandleFunc("POST /api/auth/access-key", api.changeAccessKey)
	mux.HandleFunc("GET /api/audit-logs", api.auditLogs)
	mux.HandleFunc("DELETE /api/audit-logs", api.clearAuditLogs)
	mux.HandleFunc("GET /api/dashboard", api.dashboard)
	mux.HandleFunc("GET /api/settings", api.settings)
	mux.HandleFunc("PUT /api/settings", api.updateSettings)
	mux.HandleFunc("GET /api/config/export", api.exportConfig)
	mux.HandleFunc("POST /api/config/import", api.importConfig)
	mux.HandleFunc("GET /api/backups", api.listBackups)
	mux.HandleFunc("POST /api/backups", api.createBackup)
	mux.HandleFunc("GET /api/backups/{name}", api.downloadBackup)
	mux.HandleFunc("POST /api/backups/{name}/restore", api.restoreBackup)
	mux.HandleFunc("GET /api/proxies/status", api.proxiesStatus)
	mux.HandleFunc("GET /api/servers", api.listServers)
	mux.HandleFunc("POST /api/servers", api.createServer)
	mux.HandleFunc("POST /api/servers/import-frpc", api.importFrpcConfig)
	mux.HandleFunc("POST /api/servers/adopt", api.adoptProcess)
	mux.HandleFunc("GET /api/servers/{id}", api.getServer)
	mux.HandleFunc("PUT /api/servers/{id}", api.updateServer)
	mux.HandleFunc("DELETE /api/servers/{id}", api.deleteServer)
	mux.HandleFunc("POST /api/servers/{id}/start", api.startServer)
	mux.HandleFunc("POST /api/servers/{id}/stop", api.stopServer)
	mux.HandleFunc("POST /api/servers/{id}/restart", api.restartServer)
	mux.HandleFunc("POST /api/servers/{id}/reload", api.reloadServer)
	mux.HandleFunc("POST /api/servers/{id}/check", api.checkServer)
	mux.HandleFunc("GET /api/servers/{id}/rules", api.listRules)
	mux.HandleFunc("POST /api/servers/{id}/rules", api.createRule)
	mux.HandleFunc("PUT /api/servers/{id}/rules/{ruleId}", api.updateRule)
	mux.HandleFunc("DELETE /api/servers/{id}/rules/{ruleId}", api.deleteRule)
	mux.HandleFunc("GET /api/servers/{id}/logs", api.logs)
	mux.HandleFunc("GET /api/servers/{id}/config/preview", api.configPreview)
	mux.HandleFunc("GET /api/frpc/version", api.currentVersion)
	mux.HandleFunc("GET /api/frpc/versions", api.versions)
	mux.HandleFunc("GET /api/frpc/discover", api.discoverFRPC)
	mux.HandleFunc("POST /api/frpc/register", api.registerBinary)
	mux.HandleFunc("POST /api/frpc/versions/{id}/activate", api.activateVersion)
	mux.HandleFunc("POST /api/frpc/check-latest", api.checkLatest)
	mux.HandleFunc("POST /api/frpc/install/online", api.installOnline)
	mux.HandleFunc("POST /api/frpc/install/offline", api.installOffline)
	mux.HandleFunc("GET /api/app/update/check", api.updateCheck)
	mux.HandleFunc("POST /api/app/update/apply", api.updateApply)
	mux.Handle("/", staticHandler(opts.WebDir, opts.WebFS))

	return loggingMiddleware(opts.Logger, authMiddleware(opts.Service, auditMiddleware(opts.Service, opts.TrustProxyHeaders, mux)))
}

type apiHandler struct {
	service           *app.Service
	logger            *slog.Logger
	limiter           *loginLimiter
	trustProxyHeaders bool
}

func (h apiHandler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"app":    "frpc-web",
	})
}

func (h apiHandler) authStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.service.AuthStatus(r.Context())
	if err != nil {
		writeResult(w, status, err)
		return
	}
	authed := false
	if _, err := sessionFromRequest(r, h.service); err == nil {
		authed = true
	}
	status.Authenticated = authed
	if !authed {
		// 不向匿名访问者暴露“仍在使用默认密钥”这一事实。
		status.MustChangePassword = false
	}
	writeJSON(w, http.StatusOK, status)
}

func (h apiHandler) login(w http.ResponseWriter, r *http.Request) {
	var input app.AuthInput
	if !decodeJSON(w, r, &input) {
		return
	}
	ip := clientIP(r, h.trustProxyHeaders)
	if retryAfter, ok := h.limiter.Allow(ip); !ok {
		writeRateLimited(w, retryAfter)
		return
	}
	meta := app.AuthMeta{IP: ip, UserAgent: r.UserAgent()}
	session, err := h.service.Login(r.Context(), input, meta)
	if err != nil {
		h.limiter.Fail(ip)
		h.auditAuth(r, "auth.login", "failure", err.Error())
		writeResult(w, session, err)
		return
	}
	writeSessionCookie(w, r, session, h.trustProxyHeaders)
	h.limiter.Reset(ip)
	h.auditAuth(r, "auth.login", "success", "")
	// mustChangePassword 为 true 时，前端弹出强制改密窗口，后端中间件也会
	// 拦截该会话对其他业务接口的访问，直到设置好自己的密码。
	writeJSON(w, http.StatusOK, map[string]bool{
		"mustChangePassword": h.service.RequiresPasswordChange(r.Context()),
	})
}

func (h apiHandler) logout(w http.ResponseWriter, r *http.Request) {
	if session, err := sessionFromRequest(r, h.service); err == nil {
		_ = h.service.RevokeCurrentSession(r.Context(), session.Token)
		h.auditAuth(r, "auth.logout", "success", "")
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h apiHandler) changeAccessKey(w http.ResponseWriter, r *http.Request) {
	var input app.AccessKeyInput
	if !decodeJSON(w, r, &input) {
		return
	}
	err := h.service.ChangeAccessKey(r.Context(), input)
	if err == nil {
		clearSessionCookie(w)
	}
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h apiHandler) auditLogs(w http.ResponseWriter, r *http.Request) {
	query := app.AuditLogQuery{
		Page:     queryInt(r, "page", 1),
		PageSize: queryInt(r, "pageSize", 50),
		Action:   r.URL.Query().Get("action"),
		Result:   r.URL.Query().Get("result"),
	}
	payload, err := h.service.AuditLogs(r.Context(), query)
	writeResult(w, payload, err)
}

func (h apiHandler) clearAuditLogs(w http.ResponseWriter, r *http.Request) {
	err := h.service.ClearAuditLogs(r.Context())
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h apiHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Dashboard(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) settings(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Settings(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var input app.SettingsInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.UpdateSettings(r.Context(), input)
	writeResult(w, payload, err)
}

func (h apiHandler) exportConfig(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.ExportConfig(r.Context())
	if err != nil {
		writeResult(w, payload, err)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=frpc-web-config.json")
	writeJSON(w, http.StatusOK, payload)
}

func (h apiHandler) importConfig(w http.ResponseWriter, r *http.Request) {
	var input app.ConfigImportInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.ImportConfig(r.Context(), input)
	writeResult(w, payload, err)
}

func (h apiHandler) listBackups(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.ListBackups(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) createBackup(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.BackupNow(r.Context())
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	data, err := h.service.ReadBackup(r.Context(), name)
	if err != nil {
		writeResult(w, nil, err)
		return
	}
	// name 已通过 ReadBackup 的白名单校验，可安全拼入响应头。
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h apiHandler) restoreBackup(w http.ResponseWriter, r *http.Request) {
	var input app.BackupRestoreInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.RestoreBackup(r.Context(), r.PathValue("name"), input.Mode)
	writeResult(w, payload, err)
}

func (h apiHandler) proxiesStatus(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.ProxiesStatus(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) listServers(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Servers(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) createServer(w http.ResponseWriter, r *http.Request) {
	var input app.ServerInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.CreateServer(r.Context(), input)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) importFrpcConfig(w http.ResponseWriter, r *http.Request) {
	var input app.ImportFrpcConfigInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.ImportFrpcConfig(r.Context(), input)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) adoptProcess(w http.ResponseWriter, r *http.Request) {
	var input app.AdoptProcessInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.AdoptProcess(r.Context(), input)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) getServer(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Server(r.Context(), r.PathValue("id"))
	writeResult(w, payload, err)
}

func (h apiHandler) updateServer(w http.ResponseWriter, r *http.Request) {
	var input app.ServerInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.UpdateServer(r.Context(), r.PathValue("id"), input)
	writeResult(w, payload, err)
}

func (h apiHandler) deleteServer(w http.ResponseWriter, r *http.Request) {
	err := h.service.DeleteServer(r.Context(), r.PathValue("id"))
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h apiHandler) startServer(w http.ResponseWriter, r *http.Request) {
	writeAction(w, h.service.Start(r.Context(), r.PathValue("id")))
}

func (h apiHandler) stopServer(w http.ResponseWriter, r *http.Request) {
	writeAction(w, h.service.Stop(r.Context(), r.PathValue("id")))
}

func (h apiHandler) restartServer(w http.ResponseWriter, r *http.Request) {
	writeAction(w, h.service.Restart(r.Context(), r.PathValue("id")))
}

func (h apiHandler) reloadServer(w http.ResponseWriter, r *http.Request) {
	writeAction(w, h.service.Reload(r.Context(), r.PathValue("id")))
}

func (h apiHandler) checkServer(w http.ResponseWriter, r *http.Request) {
	writeAction(w, h.service.Check(r.Context(), r.PathValue("id")))
}

func (h apiHandler) listRules(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Rules(r.Context(), r.PathValue("id"))
	writeResult(w, payload, err)
}

func (h apiHandler) createRule(w http.ResponseWriter, r *http.Request) {
	var input app.ProxyRuleInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.CreateRule(r.Context(), r.PathValue("id"), input)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) updateRule(w http.ResponseWriter, r *http.Request) {
	var input app.ProxyRuleInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.UpdateRule(r.Context(), r.PathValue("id"), r.PathValue("ruleId"), input)
	writeResult(w, payload, err)
}

func (h apiHandler) deleteRule(w http.ResponseWriter, r *http.Request) {
	err := h.service.DeleteRule(r.Context(), r.PathValue("id"), r.PathValue("ruleId"))
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h apiHandler) logs(w http.ResponseWriter, r *http.Request) {
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			tail = parsed
		}
	}
	payload, err := h.service.Logs(r.Context(), r.PathValue("id"), tail)
	writeResult(w, payload, err)
}

func (h apiHandler) configPreview(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.ConfigPreview(r.Context(), r.PathValue("id"))
	writeResult(w, payload, err)
}

func (h apiHandler) currentVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.service.CurrentVersion(r.Context()))
}

func (h apiHandler) versions(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Versions(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) discoverFRPC(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.DiscoverFRPC(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) registerBinary(w http.ResponseWriter, r *http.Request) {
	var input app.RegisterBinaryInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.RegisterBinary(r.Context(), input)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) activateVersion(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.ActivateVersion(r.Context(), r.PathValue("id"))
	writeResult(w, payload, err)
}

func (h apiHandler) checkLatest(w http.ResponseWriter, r *http.Request) {
	var input app.LatestVersionInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.CheckLatest(r.Context(), input)
	writeResult(w, payload, err)
}

func (h apiHandler) installOnline(w http.ResponseWriter, r *http.Request) {
	var input app.FRPCInstallOnlineInput
	if !decodeJSON(w, r, &input) {
		return
	}
	payload, err := h.service.InstallOnline(r.Context(), input)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) installOffline(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("parse multipart form failed: %v", err))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()
	payload, err := h.service.InstallOffline(r.Context(), header.Filename, file)
	writeResultStatus(w, http.StatusCreated, payload, err)
}

func (h apiHandler) updateCheck(w http.ResponseWriter, r *http.Request) {
	check, err := h.service.CheckUpdate(r.Context())
	writeResult(w, check, err)
}

func (h apiHandler) updateApply(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ApplyUpdate(r.Context())
	if err != nil {
		writeResult(w, result, err)
		return
	}
	writeAction(w, result)
}

func (h apiHandler) auditAuth(r *http.Request, action, result, errorText string) {
	h.service.AddAudit(r.Context(), app.AuditLogInput{
		IP:           clientIP(r, h.trustProxyHeaders),
		UserAgent:    r.UserAgent(),
		Action:       action,
		ResourceType: "session",
		Result:       result,
		Error:        errorText,
	})
}

// maxJSONBody 限制 JSON 请求体大小，防止异常大的请求耗尽内存。
const maxJSONBody = 1 << 20 // 1MB

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid json: %v", err))
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, payload any, err error) {
	writeResultStatus(w, http.StatusOK, payload, err)
}

func writeResultStatus(w http.ResponseWriter, okStatus int, payload any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, app.ErrInvalidInput) {
			status = http.StatusBadRequest
		} else if errors.Is(err, app.ErrInvalidCredentials) || errors.Is(err, app.ErrUnauthorized) {
			status = http.StatusUnauthorized
		} else if errors.Is(err, app.ErrPasswordChangeRequired) {
			status = http.StatusForbidden
		} else if errors.Is(err, app.ErrNotFound) {
			status = http.StatusNotFound
		}
		// 5xx 往往携带内部细节（文件路径、底层错误），只记服务端日志，
		// 对客户端返回笼统文案，避免向已登录用户泄露实现细节。
		if status >= http.StatusInternalServerError {
			slog.Error("request handler error", "error", err.Error())
			writeError(w, status, "服务器内部错误，请稍后重试或查看服务端日志")
			return
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, okStatus, payload)
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func writeAction(w http.ResponseWriter, result app.ActionResult) {
	if result.OK {
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusBadRequest, result)
}

// writeRateLimited 返回 429 与人类可读的剩余锁定时长。
func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	minutes := int((retryAfter + time.Minute - 1) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	writeError(w, http.StatusTooManyRequests, fmt.Sprintf("失败次数过多，已临时锁定，请约 %d 分钟后再试", minutes))
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// setStaticCacheHeaders 设置静态资源缓存策略：Vite 产物 assets/ 下文件名带内容
// 哈希，可长期缓存；其余文件（index.html、favicon 等）路径固定，必须每次回源
// 校验，否则自更新替换二进制后浏览器可能继续渲染旧页面。
func setStaticCacheHeaders(w http.ResponseWriter, cleanPath string) {
	if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

func staticHandler(webDir string, webFS http.FileSystem) http.Handler {
	if webDir != "" {
		return staticDirHandler(webDir)
	}
	if webFS != nil {
		return staticFSHandler(webFS)
	}
	return staticDirHandler("web/dist")
}

func staticDirHandler(webDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "api route not found")
			return
		}

		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		path := filepath.Join(webDir, cleanPath)
		info, err := os.Stat(path)
		if r.URL.Path == "/" || err != nil || info.IsDir() {
			setStaticCacheHeaders(w, "index.html")
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
			return
		}

		setStaticCacheHeaders(w, cleanPath)
		fileServer.ServeHTTP(w, r)
	})
}

func staticFSHandler(webFS http.FileSystem) http.Handler {
	fileServer := http.FileServer(webFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "api route not found")
			return
		}

		cleanPath := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		if cleanPath == "." {
			serveIndexFromFS(w, r, webFS)
			return
		}
		file, err := webFS.Open(cleanPath)
		if err != nil {
			serveIndexFromFS(w, r, webFS)
			return
		}
		info, err := file.Stat()
		_ = file.Close()
		if err != nil || info.IsDir() {
			serveIndexFromFS(w, r, webFS)
			return
		}

		setStaticCacheHeaders(w, cleanPath)
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndexFromFS(w http.ResponseWriter, r *http.Request, webFS http.FileSystem) {
	setStaticCacheHeaders(w, "index.html")
	file, err := webFS.Open("index.html")
	if err != nil {
		writeError(w, http.StatusNotFound, "frontend index not found")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "frontend index not found")
		return
	}
	reader, ok := file.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read frontend index failed")
			return
		}
		http.ServeContent(w, r, "index.html", info.ModTime(), strings.NewReader(string(data)))
		return
	}
	http.ServeContent(w, r, "index.html", info.ModTime(), reader)
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start).Round(time.Millisecond))
	})
}
