package server

import (
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/sccens/frpc-web/internal/app"
)

const sessionCookieName = "frpc_web_session"

func authMiddleware(service *app.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || isPublicAPI(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		status, err := service.AuthStatus(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !status.Bootstrapped {
			writeError(w, http.StatusUnauthorized, app.ErrBootstrapRequired.Error())
			return
		}

		if _, err := sessionFromRequest(r, service); err != nil {
			writeError(w, http.StatusUnauthorized, app.ErrUnauthorized.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicAPI(path string) bool {
	switch path {
	case "/api/health", "/api/auth/status", "/api/auth/bootstrap", "/api/auth/login", "/api/auth/logout":
		return true
	default:
		return false
	}
}

// 会话凭证直接放在 HttpOnly Cookie 中；服务端只存其 SHA-256 哈希。
func writeSessionCookie(w http.ResponseWriter, r *http.Request, session app.Session, trustProxyHeaders bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.Token,
		Path:     "/",
		MaxAge:   int(app.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isSecureRequest(r, trustProxyHeaders),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func sessionFromRequest(r *http.Request, service *app.Service) (app.Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return app.Session{}, app.ErrUnauthorized
	}
	return service.VerifySession(r.Context(), cookie.Value)
}

func isSecureRequest(r *http.Request, trustProxyHeaders bool) bool {
	if r.TLS != nil {
		return true
	}
	if !trustProxyHeaders {
		return false
	}
	proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	if first, _, ok := strings.Cut(proto, ","); ok {
		proto = strings.TrimSpace(first)
	}
	return proto == "https" || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func auditMiddleware(service *app.Service, trustProxyHeaders bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || isPublicAPI(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		meta, ok := auditMetaFor(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		result := "success"
		if status >= 400 {
			result = "failure"
		}
		service.AddAudit(r.Context(), app.AuditLogInput{
			IP:           clientIP(r, trustProxyHeaders),
			UserAgent:    r.UserAgent(),
			Action:       meta.action,
			ResourceType: meta.resourceType,
			ResourceID:   meta.resourceID,
			Result:       result,
			Error:        auditStatusError(status),
		})
	})
}

type auditMeta struct {
	action       string
	resourceType string
	resourceID   string
}

func auditMetaFor(r *http.Request) (auditMeta, bool) {
	path := r.URL.Path
	method := r.Method
	parts := pathParts(path)
	switch {
	case path == "/api/settings" && method == http.MethodPut:
		return auditMeta{"settings.update", "settings", ""}, true
	case path == "/api/config/export" && method == http.MethodGet:
		return auditMeta{"config.export", "config", ""}, true
	case path == "/api/config/import" && method == http.MethodPost:
		return auditMeta{"config.import", "config", ""}, true
	case path == "/api/backups" && method == http.MethodPost:
		return auditMeta{"backup.create", "backup", ""}, true
	case len(parts) == 3 && parts[1] == "backups" && method == http.MethodGet:
		return auditMeta{"backup.download", "backup", part(parts, 2)}, true
	case len(parts) == 4 && parts[1] == "backups" && parts[3] == "restore" && method == http.MethodPost:
		return auditMeta{"backup.restore", "backup", part(parts, 2)}, true
	case path == "/api/auth/access-key" && method == http.MethodPost:
		return auditMeta{"auth.access_key", "settings", "access_key"}, true
	case path == "/api/audit-logs" && method == http.MethodDelete:
		return auditMeta{"audit.clear", "audit", ""}, true
	case path == "/api/servers" && method == http.MethodPost:
		return auditMeta{"servers.create", "server", ""}, true
	case len(parts) == 3 && parts[1] == "servers" && method == http.MethodPut:
		return auditMeta{"servers.update", "server", part(parts, 2)}, true
	case len(parts) == 3 && parts[1] == "servers" && method == http.MethodDelete:
		return auditMeta{"servers.delete", "server", part(parts, 2)}, true
	case strings.HasSuffix(path, "/start"):
		return auditMeta{"servers.start", "server", part(parts, 2)}, true
	case strings.HasSuffix(path, "/stop"):
		return auditMeta{"servers.stop", "server", part(parts, 2)}, true
	case strings.HasSuffix(path, "/restart"):
		return auditMeta{"servers.restart", "server", part(parts, 2)}, true
	case strings.HasSuffix(path, "/reload"):
		return auditMeta{"servers.reload", "server", part(parts, 2)}, true
	case strings.HasSuffix(path, "/check"):
		return auditMeta{"servers.check", "server", part(parts, 2)}, true
	case strings.HasSuffix(path, "/rules") && method == http.MethodPost:
		return auditMeta{"rules.create", "server", part(parts, 2)}, true
	case len(parts) == 5 && parts[1] == "servers" && parts[3] == "rules" && method == http.MethodPut:
		return auditMeta{"rules.update", "rule", part(parts, 4)}, true
	case len(parts) == 5 && parts[1] == "servers" && parts[3] == "rules" && method == http.MethodDelete:
		return auditMeta{"rules.delete", "rule", part(parts, 4)}, true
	case strings.HasPrefix(path, "/api/frpc/versions/") && strings.HasSuffix(path, "/activate"):
		return auditMeta{"frpc.activate", "frpc_version", part(parts, 3)}, true
	case path == "/api/frpc/check-latest":
		return auditMeta{"frpc.check_latest", "frpc", ""}, true
	case path == "/api/frpc/install/online":
		return auditMeta{"frpc.install_online", "frpc", ""}, true
	case path == "/api/frpc/install/offline":
		return auditMeta{"frpc.install_offline", "frpc", ""}, true
	default:
		return auditMeta{}, false
	}
}

func pathParts(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func part(parts []string, index int) string {
	if index < 0 || index >= len(parts) {
		return ""
	}
	return parts[index]
}

func auditStatusError(status int) string {
	if status < 400 {
		return ""
	}
	return "http " + strconv.Itoa(status)
}

func clientIP(r *http.Request, trustProxyHeaders bool) string {
	if trustProxyHeaders {
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return strings.TrimSpace(strings.Split(forwarded, ",")[0])
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return realIP
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
