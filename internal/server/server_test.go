package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

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

	// 用出厂默认密钥登录，拿到一个“待改密”会话。
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"accessKey":"`+app.DefaultAccessKey+`"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login should set session cookie")
	}
	initCookie := cookies[0]
	if !initCookie.HttpOnly || initCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected cookie flags: %#v", initCookie)
	}

	// 待改密会话只能改密：访问其他业务接口被拦截为 403。
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusForbidden, initCookie)

	// 设置自己的密码后，初始密钥与该会话同时失效。
	assertStatus(t, handler, http.MethodPost, "/api/auth/access-key", `{"newAccessKey":"Password123"}`, http.StatusOK, initCookie)
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, initCookie)
	assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"`+app.DefaultAccessKey+`"}`, http.StatusUnauthorized, nil)

	// 用新密码登录后即可访问业务接口。
	full := loginCookie(t, handler, "Password123")
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusOK, full)

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(full)
	handler.ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body = %s", logout.Code, logout.Body.String())
	}
	if len(logout.Result().Cookies()) == 0 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout should clear cookie: %#v", logout.Result().Cookies())
	}
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, full)
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

	firstCookie := personalize(t, handler, "Password123")
	secondCookie := loginCookie(t, handler, "Password123")

	createServerReq := `{"name":"main","serverAddr":"frp.example.com","serverPort":7000,"transportProtocol":"tcp","adminPort":17400}`
	assertStatus(t, handler, http.MethodPost, "/api/servers", createServerReq, http.StatusCreated, secondCookie)
	servers, err := store.ListServers(ctx)
	if err != nil || len(servers) != 1 {
		t.Fatalf("list servers: servers=%#v err=%v", servers, err)
	}

	// 修改 Access Key 后所有会话立即失效。
	assertStatus(t, handler, http.MethodPost, "/api/auth/access-key", `{"currentAccessKey":"Password123","newAccessKey":"NewPass456"}`, http.StatusOK, secondCookie)
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, firstCookie)
	assertStatus(t, handler, http.MethodGet, "/api/settings", "", http.StatusUnauthorized, secondCookie)
	assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"Password123"}`, http.StatusUnauthorized, nil)
	newCookie := loginCookie(t, handler, "NewPass456")
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

	for i := 0; i < 5; i++ {
		assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"wrong-password"}`, http.StatusUnauthorized, nil)
	}
	assertStatus(t, handler, http.MethodPost, "/api/auth/login", `{"accessKey":"`+app.DefaultAccessKey+`"}`, http.StatusTooManyRequests, nil)
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
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"accessKey":"`+app.DefaultAccessKey+`"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login should set session cookie")
	}
	if !cookies[0].Secure {
		t.Fatalf("session cookie should be secure behind trusted HTTPS proxy: %#v", cookies[0])
	}
}

func TestStaticCacheHeaders(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &serverFakeRuntime{}, Addr: "127.0.0.1:8080"})

	webDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(webDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	embedded := http.FS(fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: []byte("<html>ok</html>")},
		"assets/app-abc123.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	})

	handlers := map[string]http.Handler{
		"dir": New(Options{Service: svc, WebDir: webDir}),
		"fs":  New(Options{Service: svc, WebFS: embedded}),
	}

	const immutable = "public, max-age=31536000, immutable"
	cases := []struct {
		path string
		want string
	}{
		{"/", "no-cache"},
		{"/servers", "no-cache"},             // SPA 路由回退 index.html
		{"/assets/app-abc123.js", immutable}, // 文件名带内容哈希，可长缓存
		{"/assets/gone-xyz.js", "no-cache"},  // 旧资源缺失时回退 index.html，不能继承长缓存
	}
	for name, handler := range handlers {
		for _, tc := range cases {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("[%s] GET %s status = %d, body = %s", name, tc.path, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.want {
				t.Fatalf("[%s] GET %s Cache-Control = %q, want %q", name, tc.path, got, tc.want)
			}
		}
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

// personalize 完成首登改密流程：用初始密钥登录、设置满足策略的新密码，
// 再用新密码登录，返回一个可访问业务接口的会话 Cookie。
func personalize(t *testing.T, handler http.Handler, newKey string) *http.Cookie {
	t.Helper()
	initCookie := loginCookie(t, handler, app.DefaultAccessKey)
	assertStatus(t, handler, http.MethodPost, "/api/auth/access-key", `{"newAccessKey":"`+newKey+`"}`, http.StatusOK, initCookie)
	return loginCookie(t, handler, newKey)
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

func (r *serverFakeRuntime) ProxyStatus(context.Context, app.Server) ([]app.ProxyStatus, error) {
	return []app.ProxyStatus{}, nil
}

func (r *serverFakeRuntime) ProcessAlive(context.Context, int) bool {
	return r.alive
}

func (r *serverFakeRuntime) SetExitHandler(func(string, error)) {}

func (r *serverFakeRuntime) Adopt(string, int) {}

func (r *serverFakeRuntime) DiscoverBinaries() []app.FRPCBinaryCandidate { return nil }

func (r *serverFakeRuntime) DiscoverProcesses() ([]app.FRPCProcessCandidate, error) {
	return nil, nil
}

func (r *serverFakeRuntime) RegisterBinary(path string) (app.FRPCVersion, error) {
	return app.FRPCVersion{Version: "system", Path: path, Source: "system", Installed: true}, nil
}
