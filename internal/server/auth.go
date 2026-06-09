package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sccens/frpc-web/internal/app"
)

const (
	sessionCookieName = "frpc_web_session"
	sessionTTL        = 12 * time.Hour
)

type contextKey string

const userContextKey contextKey = "user"

type tokenClaims struct {
	Subject   string `json:"sub"`
	SessionID string `json:"sid"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	IssuedAt  int64  `json:"iat"`
	Expires   int64  `json:"exp"`
}

func authMiddleware(service *app.Service, trustProxyHeaders bool, next http.Handler) http.Handler {
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

		user, err := userFromRequest(r, service)
		if err != nil {
			writeError(w, http.StatusUnauthorized, app.ErrUnauthorized.Error())
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
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

func writeSessionCookie(w http.ResponseWriter, r *http.Request, service *app.Service, session app.AuthSession) error {
	secret, err := service.JWTSecret(r.Context())
	if err != nil {
		return err
	}
	token, err := signToken(secret, session.User, session.Session.Token)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
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

func userFromRequest(r *http.Request, service *app.Service) (app.User, error) {
	user, _, err := authSessionFromRequest(r, service)
	return user, err
}

func sessionIDFromRequest(r *http.Request, service *app.Service) (string, error) {
	_, session, err := authSessionFromRequest(r, service)
	return session.Token, err
}

func authSessionFromRequest(r *http.Request, service *app.Service) (app.User, app.Session, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return app.User{}, app.Session{}, app.ErrUnauthorized
	}
	secret, err := service.JWTSecret(r.Context())
	if err != nil {
		return app.User{}, app.Session{}, err
	}
	claims, err := verifyToken(secret, cookie.Value)
	if err != nil {
		return app.User{}, app.Session{}, app.ErrUnauthorized
	}
	user, session, err := service.VerifySession(r.Context(), claims.SessionID)
	if err != nil {
		return app.User{}, app.Session{}, err
	}
	if claims.Subject != "" && claims.Subject != user.ID {
		return app.User{}, app.Session{}, app.ErrUnauthorized
	}
	return user, session, nil
}

func userFromContext(ctx context.Context) (app.User, bool) {
	user, ok := ctx.Value(userContextKey).(app.User)
	return user, ok
}

func signToken(secret []byte, user app.User, sessionID string) (string, error) {
	now := time.Now()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := tokenClaims{
		Subject:   user.ID,
		SessionID: sessionID,
		Username:  user.Username,
		Role:      user.Role,
		IssuedAt:  now.Unix(),
		Expires:   now.Add(sessionTTL).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	return unsigned + "." + signSegment(secret, unsigned), nil
}

func verifyToken(secret []byte, token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("invalid token")
	}
	unsigned := parts[0] + "." + parts[1]
	expected := signSegment(secret, unsigned)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return tokenClaims{}, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, err
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, err
	}
	if claims.Subject == "" || claims.SessionID == "" || claims.Expires < time.Now().Unix() {
		return tokenClaims{}, errors.New("token expired")
	}
	return claims, nil
}

func signSegment(secret []byte, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
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
		user, _ := userFromContext(r.Context())
		service.AddAudit(r.Context(), app.AuditLogInput{
			UserID:       user.ID,
			Username:     user.Username,
			Role:         user.Role,
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
	case path == "/api/auth/access-key" && method == http.MethodPost:
		return auditMeta{"auth.access_key", "settings", "access_key"}, true
	case strings.HasPrefix(path, "/api/auth/sessions/") && method == http.MethodDelete:
		return auditMeta{"auth.session_revoke", "session", part(parts, 3)}, true
	case path == "/api/auth/sessions/revoke-others" && method == http.MethodPost:
		return auditMeta{"auth.session_revoke_others", "session", ""}, true
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
