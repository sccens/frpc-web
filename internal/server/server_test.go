package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
	"github.com/sccens/frpc-web/internal/storage"
)

func TestAccessKeyAPIProtectsBusinessRoutes(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &serverFakeRuntime{}, Addr: "127.0.0.1:8080"})
	handler := New(Options{Service: svc, WebDir: t.TempDir()})

	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", bytes.NewBufferString(`{"accessKey":"password123"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("bootstrap should set session cookie")
	}
	sessionCookie := cookies[0]
	if !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected cookie flags: %#v", sessionCookie)
	}

	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusOK, sessionCookie)
	assertStatus(t, handler, http.MethodPost, "/api/auth/bootstrap", `{"accessKey":"password123"}`, http.StatusConflict, nil)
	assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"bad-password"}`, http.StatusUnauthorized, nil)

	login := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"accessKey":"password123"}`))
	handler.ServeHTTP(login, loginReq)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	if len(login.Result().Cookies()) == 0 {
		t.Fatal("login should set session cookie")
	}

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	handler.ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	if len(logout.Result().Cookies()) == 0 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout should clear cookie: %#v", logout.Result().Cookies())
	}
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, sessionCookie)
}

func TestAccessKeyAndAuditAPI(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	runtime := &serverFakeRuntime{alive: true}
	svc := app.NewService(app.Options{Store: store, Runtime: runtime, Addr: "127.0.0.1:8080"})
	handler := New(Options{Service: svc, WebDir: t.TempDir()})

	firstCookie := bootstrapCookie(t, handler)
	secondCookie := loginCookie(t, handler, "password123")

	createServerReq := `{"name":"main","serverAddr":"frp.example.com","serverPort":7000,"transportProtocol":"tcp","adminPort":17400}`
	assertStatus(t, handler, http.MethodPost, "/api/servers", createServerReq, http.StatusCreated, secondCookie)
	servers, err := store.ListServers(ctx)
	if err != nil || len(servers) != 1 {
		t.Fatalf("list servers: servers=%#v err=%v", servers, err)
	}

	// 修改 Access Key 后所有会话立即失效。
	assertStatus(t, handler, http.MethodPost, "/api/auth/access-key", `{"currentAccessKey":"password123","newAccessKey":"new-password123"}`, http.StatusOK, secondCookie)
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, firstCookie)
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, secondCookie)
	assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"password123"}`, http.StatusUnauthorized, nil)
	newCookie := loginCookie(t, handler, "new-password123")
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusOK, newCookie)
	assertStatus(t, handler, http.MethodGet, "/api/audit-logs", "", http.StatusOK, newCookie)
	assertStatus(t, handler, http.MethodDelete, "/api/audit-logs", "", http.StatusOK, newCookie)
}

func TestLoginRateLimit(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &serverFakeRuntime{}, Addr: "127.0.0.1:8080"})
	handler := New(Options{Service: svc, WebDir: t.TempDir()})
	_ = bootstrapCookie(t, handler)

	for i := 0; i < 5; i++ {
		assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"wrong-password"}`, http.StatusUnauthorized, nil)
	}
	assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"password123"}`, http.StatusTooManyRequests, nil)
}

func TestClientIPTrustProxyHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.8")
	req.Header.Set("X-Real-IP", "203.0.113.9")

	if got := clientIP(req, false); got != "10.0.0.2" {
		t.Fatalf("clientIP without trust = %q, want remote addr", got)
	}
	if got := clientIP(req, true); got != "203.0.113.8" {
		t.Fatalf("clientIP with trust = %q, want XFF", got)
	}
}

func TestSessionCookieSecureBehindTrustedHTTPSProxy(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &serverFakeRuntime{}, Addr: "127.0.0.1:8080"})
	handler := New(Options{Service: svc, WebDir: t.TempDir(), TrustProxyHeaders: true})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", bytes.NewBufferString(`{"accessKey":"password123"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("bootstrap should set session cookie")
	}
	if !cookies[0].Secure {
		t.Fatalf("session cookie should be secure behind trusted HTTPS proxy: %#v", cookies[0])
	}
}

func assertStatus(t *testing.T, handler http.Handler, method, path, body string, want int, cookie *http.Cookie) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, path, rec.Code, want, rec.Body.String())
	}
}

func bootstrapCookie(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", bytes.NewBufferString(`{"accessKey":"password123"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bootstrap status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()[0]
}

func loginCookie(t *testing.T, handler http.Handler, accessKey string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"accessKey":"`+accessKey+`"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()[0]
}

type serverFakeRuntime struct {
	alive bool
}

func (r *serverFakeRuntime) RenderConfig(context.Context, app.Server) (app.ConfigPreview, error) {
	return app.ConfigPreview{}, nil
}

func (r *serverFakeRuntime) CheckConfig(context.Context, app.Server, app.FRPCVersion) app.ActionResult {
	return app.ActionResult{OK: true, Message: "ok"}
}

func (r *serverFakeRuntime) Start(context.Context, app.Server, app.FRPCVersion) (app.ProcessInfo, app.ActionResult) {
	return app.ProcessInfo{}, app.ActionResult{OK: true, Message: "ok"}
}

func (r *serverFakeRuntime) Stop(context.Context, app.Server, app.ProcessInfo) app.ActionResult {
	return app.ActionResult{OK: true, Message: "ok"}
}

func (r *serverFakeRuntime) Reload(context.Context, app.Server, app.FRPCVersion) app.ActionResult {
	return app.ActionResult{OK: true, Message: "ok"}
}

func (r *serverFakeRuntime) Logs(context.Context, string, int) ([]app.LogLine, error) {
	return []app.LogLine{}, nil
}

func (r *serverFakeRuntime) InstallOnline(context.Context, app.FRPCInstallOnlineInput) (app.FRPCVersion, error) {
	return app.FRPCVersion{}, nil
}

func (r *serverFakeRuntime) InstallOffline(context.Context, string, io.Reader) (app.FRPCVersion, error) {
	return app.FRPCVersion{}, nil
}

func (r *serverFakeRuntime) LatestVersion(context.Context, string) (string, error) {
	return "0.70.0", nil
}

func (r *serverFakeRuntime) ProcessAlive(context.Context, int) bool {
	return r.alive
}

func (r *serverFakeRuntime) SetExitHandler(func(string, error)) {}

func (r *serverFakeRuntime) Adopt(string, int) {}
