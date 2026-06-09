package server

import (
	"database/sql"
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
	mux.HandleFunc("POST /api/auth/bootstrap", api.bootstrap)
	mux.HandleFunc("POST /api/auth/login", api.login)
	mux.HandleFunc("POST /api/auth/logout", api.logout)
	mux.HandleFunc("GET /api/auth/me", api.me)
	mux.HandleFunc("GET /api/auth/sessions", api.sessions)
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", api.revokeSession)
	mux.HandleFunc("POST /api/auth/sessions/revoke-others", api.revokeOtherSessions)
	mux.HandleFunc("POST /api/auth/access-key", api.changeAccessKey)
	mux.HandleFunc("GET /api/audit-logs", api.auditLogs)
	mux.HandleFunc("GET /api/dashboard", api.dashboard)
	mux.HandleFunc("GET /api/stats", api.stats)
	mux.HandleFunc("GET /api/settings", api.settings)
	mux.HandleFunc("PUT /api/settings", api.updateSettings)
	mux.HandleFunc("GET /api/config/export", api.exportConfig)
	mux.HandleFunc("POST /api/config/import", api.importConfig)
	mux.HandleFunc("GET /api/servers", api.listServers)
	mux.HandleFunc("POST /api/servers", api.createServer)
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
	mux.HandleFunc("POST /api/frpc/versions/{id}/activate", api.activateVersion)
	mux.HandleFunc("POST /api/frpc/check-latest", api.checkLatest)
	mux.HandleFunc("POST /api/frpc/install/online", api.installOnline)
	mux.HandleFunc("POST /api/frpc/install/offline", api.installOffline)
	mux.Handle("/", staticHandler(opts.WebDir, opts.WebFS))

	return loggingMiddleware(opts.Logger, authMiddleware(opts.Service, opts.TrustProxyHeaders, auditMiddleware(opts.Service, opts.TrustProxyHeaders, mux)))
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
	if user, err := userFromRequest(r, h.service); err == nil {
		status.Authenticated = true
		status.User = &user
	}
	writeJSON(w, http.StatusOK, status)
}

func (h apiHandler) bootstrap(w http.ResponseWriter, r *http.Request) {
	var input app.AuthInput
	if !decodeJSON(w, r, &input) {
		return
	}
	ip := clientIP(r, h.trustProxyHeaders)
	if retryAfter, ok := h.limiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}
	meta := app.AuthMeta{IP: ip, UserAgent: r.UserAgent()}
	session, err := h.service.Bootstrap(r.Context(), input, meta)
	if err != nil {
		h.limiter.Fail(ip)
		h.auditAuth(r, "owner", "owner", "admin", "auth.bootstrap", "failure", err.Error())
		writeResult(w, session, err)
		return
	}
	if err := writeSessionCookie(w, r, h.service, session); err != nil {
		h.auditAuth(r, session.User.ID, session.User.Username, session.User.Role, "auth.bootstrap", "failure", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.limiter.Reset(ip)
	h.auditAuth(r, session.User.ID, session.User.Username, session.User.Role, "auth.bootstrap", "success", "")
	writeJSON(w, http.StatusCreated, session)
}

func (h apiHandler) login(w http.ResponseWriter, r *http.Request) {
	var input app.AuthInput
	if !decodeJSON(w, r, &input) {
		return
	}
	ip := clientIP(r, h.trustProxyHeaders)
	if retryAfter, ok := h.limiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}
	meta := app.AuthMeta{IP: ip, UserAgent: r.UserAgent()}
	session, err := h.service.Login(r.Context(), input, meta)
	if err != nil {
		h.limiter.Fail(ip)
		h.auditAuth(r, "owner", "owner", "admin", "auth.login", "failure", err.Error())
		writeResult(w, session, err)
		return
	}
	if err := writeSessionCookie(w, r, h.service, session); err != nil {
		h.auditAuth(r, session.User.ID, session.User.Username, session.User.Role, "auth.login", "failure", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.limiter.Reset(ip)
	h.auditAuth(r, session.User.ID, session.User.Username, session.User.Role, "auth.login", "success", "")
	writeJSON(w, http.StatusOK, session)
}

func (h apiHandler) logout(w http.ResponseWriter, r *http.Request) {
	if user, session, err := authSessionFromRequest(r, h.service); err == nil {
		_ = h.service.RevokeCurrentSession(r.Context(), session.Token)
		h.service.AddAudit(r.Context(), app.AuditLogInput{
			UserID:       user.ID,
			Username:     user.Username,
			Role:         user.Role,
			IP:           clientIP(r, h.trustProxyHeaders),
			UserAgent:    r.UserAgent(),
			Action:       "auth.logout",
			ResourceType: "session",
			Result:       "success",
		})
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h apiHandler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := userFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h apiHandler) sessions(w http.ResponseWriter, r *http.Request) {
	sessionID, err := sessionIDFromRequest(r, h.service)
	if err != nil {
		writeResult(w, []app.Session{}, err)
		return
	}
	payload, err := h.service.Sessions(r.Context(), sessionID)
	writeResult(w, payload, err)
}

func (h apiHandler) revokeSession(w http.ResponseWriter, r *http.Request) {
	currentSessionID, _ := sessionIDFromRequest(r, h.service)
	err := h.service.RevokeSession(r.Context(), r.PathValue("id"))
	if err == nil {
		sessions, _ := h.service.Sessions(r.Context(), currentSessionID)
		for _, session := range sessions {
			if session.ID == r.PathValue("id") && session.Current {
				clearSessionCookie(w)
				break
			}
		}
	}
	writeResult(w, map[string]bool{"ok": true}, err)
}

func (h apiHandler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	sessionID, err := sessionIDFromRequest(r, h.service)
	if err != nil {
		writeResult(w, map[string]bool{"ok": false}, err)
		return
	}
	err = h.service.RevokeOtherSessions(r.Context(), sessionID)
	writeResult(w, map[string]bool{"ok": true}, err)
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
		User:     r.URL.Query().Get("user"),
		Result:   r.URL.Query().Get("result"),
	}
	payload, err := h.service.AuditLogs(r.Context(), query)
	writeResult(w, payload, err)
}

func (h apiHandler) dashboard(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Dashboard(r.Context())
	writeResult(w, payload, err)
}

func (h apiHandler) stats(w http.ResponseWriter, r *http.Request) {
	payload, err := h.service.Stats(r.Context())
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
	includeSensitive := parseBoolQuery(r.URL.Query().Get("includeSensitive"))
	payload, err := h.service.ExportConfig(r.Context(), includeSensitive)
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
	if err := r.ParseMultipartForm(256 << 20); err != nil {
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

func (h apiHandler) auditAuth(r *http.Request, userID, username, role, action, result, errorText string) {
	h.service.AddAudit(r.Context(), app.AuditLogInput{
		UserID:       strings.TrimSpace(userID),
		Username:     strings.TrimSpace(username),
		Role:         strings.TrimSpace(role),
		IP:           clientIP(r, h.trustProxyHeaders),
		UserAgent:    r.UserAgent(),
		Action:       action,
		ResourceType: "session",
		Result:       result,
		Error:        errorText,
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
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
		} else if errors.Is(err, app.ErrInvalidCredentials) || errors.Is(err, app.ErrUnauthorized) || errors.Is(err, app.ErrBootstrapRequired) {
			status = http.StatusUnauthorized
		} else if errors.Is(err, app.ErrAlreadyBootstrapped) {
			status = http.StatusConflict
		} else if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
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

func parseBoolQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func writeAction(w http.ResponseWriter, result app.ActionResult) {
	if result.OK {
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusBadRequest, result)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
			return
		}

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

		fileServer.ServeHTTP(w, r)
	})
}

func serveIndexFromFS(w http.ResponseWriter, r *http.Request, webFS http.FileSystem) {
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
		next.ServeHTTP(w, r)
		logger.Info("request", "method", r.Method, "path", r.URL.Path)
	})
}
