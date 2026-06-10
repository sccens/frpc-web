package app_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
	"github.com/sccens/frpc-web/internal/storage"
)

func TestServiceAccessKeySettingsAndProxyPriority(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	runtime := &fakeRuntime{latest: "0.70.0"}
	svc := app.NewService(app.Options{
		Store:   store,
		Runtime: runtime,
		Addr:    "127.0.0.1:8080",
	})
	meta := app.AuthMeta{IP: "127.0.0.1", UserAgent: "go-test"}

	status, err := svc.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if status.Bootstrapped {
		t.Fatal("fresh store should not be bootstrapped")
	}

	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "password123"}, meta); !errors.Is(err, app.ErrBootstrapRequired) {
		t.Fatalf("login before bootstrap error = %v, want bootstrap required", err)
	}

	session, err := svc.Bootstrap(ctx, app.AuthInput{AccessKey: "password123"}, meta)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if session.Token == "" {
		t.Fatalf("unexpected bootstrap session: %#v", session)
	}
	if _, err := svc.VerifySession(ctx, session.Token); err != nil {
		t.Fatalf("verify session: %v", err)
	}
	if _, err := svc.Bootstrap(ctx, app.AuthInput{AccessKey: "password123"}, meta); !errors.Is(err, app.ErrAlreadyBootstrapped) {
		t.Fatalf("duplicate bootstrap error = %v, want already bootstrapped", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "wrong-password"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("wrong login error = %v, want invalid credentials", err)
	}
	login, err := svc.Login(ctx, app.AuthInput{AccessKey: "password123"}, meta)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if login.Token == "" {
		t.Fatalf("unexpected login session: %#v", login)
	}

	settings, err := svc.UpdateSettings(ctx, app.SettingsInput{GithubProxy: " https://proxy.example/ "})
	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if settings.GithubProxy != "https://proxy.example/" {
		t.Fatalf("unexpected settings: %#v", settings)
	}

	if _, err := svc.CheckLatest(ctx, app.LatestVersionInput{}); err != nil {
		t.Fatalf("check latest with stored proxy: %v", err)
	}
	if runtime.latestProxy != "https://proxy.example/" {
		t.Fatalf("latest proxy = %q, want persisted proxy", runtime.latestProxy)
	}
	if _, err := svc.CheckLatest(ctx, app.LatestVersionInput{GithubProxy: "https://request.example/"}); err != nil {
		t.Fatalf("check latest with request proxy: %v", err)
	}
	if runtime.latestProxy != "https://request.example/" {
		t.Fatalf("latest proxy = %q, want request proxy", runtime.latestProxy)
	}
	if _, err := svc.InstallOnline(ctx, app.FRPCInstallOnlineInput{Version: "0.70.0", Platform: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("install online: %v", err)
	}
	if runtime.installInput.GithubProxy != "https://proxy.example/" {
		t.Fatalf("install proxy = %q, want persisted proxy", runtime.installInput.GithubProxy)
	}

	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{CurrentAccessKey: "password123", NewAccessKey: "new-password123"}); err != nil {
		t.Fatalf("change access key: %v", err)
	}
	if _, err := svc.VerifySession(ctx, login.Token); !errors.Is(err, app.ErrUnauthorized) {
		t.Fatalf("old session verify error = %v, want unauthorized", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "password123"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("old key login error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "new-password123"}, meta); err != nil {
		t.Fatalf("new key login: %v", err)
	}
}

func TestServiceEnvAccessKeyPriority(t *testing.T) {
	t.Setenv("FRPC_WEB_ACCESS_KEY", "env-secret-123")
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})
	meta := app.AuthMeta{IP: "127.0.0.1", UserAgent: "go-test"}

	status, err := svc.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if !status.Bootstrapped {
		t.Fatal("env access key should mark auth as bootstrapped")
	}
	if _, err := svc.Bootstrap(ctx, app.AuthInput{AccessKey: "password123"}, meta); !errors.Is(err, app.ErrAlreadyBootstrapped) {
		t.Fatalf("bootstrap with env key error = %v, want already bootstrapped", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "password123"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("wrong env key login error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "env-secret-123"}, meta); err != nil {
		t.Fatalf("env key login: %v", err)
	}
}

func TestServiceStatsAndAudit(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	runtime := &fakeRuntime{
		alive: true,
		adminStatus: app.AdminStatus{Proxies: []app.AdminProxyStatus{
			{Name: "ssh", Type: "tcp", Status: "online", LocalAddr: "127.0.0.1:22", RemoteAddr: ":6022", TrafficAvailable: true, TrafficIn: 128, TrafficOut: 256},
			{Name: "web", Type: "http", Status: "error", Error: "domain unavailable"},
		}},
	}
	svc := app.NewService(app.Options{Store: store, Runtime: runtime, Addr: "127.0.0.1:8080"})
	server, err := svc.CreateServer(ctx, app.ServerInput{Name: "main", ServerAddr: "frp.example.com", ServerPort: 7000, AdminPort: 17400})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := svc.CreateRule(ctx, server.ID, app.ProxyRuleInput{Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6022, Enabled: true}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := store.SetServerStatus(ctx, server.ID, "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}
	if err := store.UpsertProcess(ctx, app.ProcessInfo{ServerID: server.ID, PID: 1234, StartedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("upsert process: %v", err)
	}

	stats, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Summary.RunningServers != 1 || stats.Summary.OnlineProxies != 1 || stats.Summary.ErrorProxies != 1 {
		t.Fatalf("unexpected stats summary: %#v", stats.Summary)
	}
	if !stats.Summary.TrafficAvailable || stats.Summary.TotalTrafficIn != 128 || stats.Summary.TotalTrafficOut != 256 {
		t.Fatalf("unexpected traffic summary: %#v", stats.Summary)
	}
	if len(stats.Errors) != 1 || stats.Errors[0].ProxyName != "web" {
		t.Fatalf("unexpected stats errors: %#v", stats.Errors)
	}

	svc.AddAudit(ctx, app.AuditLogInput{
		IP:           "127.0.0.1",
		Action:       "auth.login",
		ResourceType: "session",
		Result:       "success",
	})
	logs, err := svc.AuditLogs(ctx, app.AuditLogQuery{Action: "auth.login", PageSize: 10})
	if err != nil {
		t.Fatalf("audit logs: %v", err)
	}
	if logs.Total != 1 || len(logs.Items) != 1 || logs.Items[0].Action != "auth.login" {
		t.Fatalf("unexpected audit logs: %#v", logs)
	}
}

func TestServiceConfigExportImport(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

	server, err := svc.CreateServer(ctx, app.ServerInput{
		Name:          "main",
		ServerAddr:    "frp.example.com",
		ServerPort:    7000,
		AuthToken:     "server-token",
		AdminUser:     "frpc-web",
		AdminPassword: "admin-secret",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := svc.CreateRule(ctx, server.ID, app.ProxyRuleInput{
		Name:      "ssh-secure",
		Type:      "stcp",
		Role:      "server",
		LocalIP:   "127.0.0.1",
		LocalPort: 22,
		SecretKey: "stcp-secret",
		Enabled:   true,
	}); err != nil {
		t.Fatalf("create stcp rule: %v", err)
	}

	full, err := svc.ExportConfig(ctx)
	if err != nil {
		t.Fatalf("export full: %v", err)
	}
	if len(full.Servers) != 1 || full.Servers[0].Server.AuthToken != "server-token" || full.Servers[0].Rules[0].SecretKey != "stcp-secret" {
		t.Fatalf("expected full secrets in export: %#v", full.Servers)
	}

	targetStore, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open target store: %v", err)
	}
	defer targetStore.Close()
	target := app.NewService(app.Options{Store: targetStore, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})
	result, err := target.ImportConfig(ctx, app.ConfigImportInput{Mode: "merge", Bundle: full})
	if err != nil {
		t.Fatalf("import config: %v", err)
	}
	if !result.OK {
		t.Fatalf("import result not ok: %#v", result)
	}
	imported, err := target.Servers(ctx)
	if err != nil {
		t.Fatalf("list imported servers: %v", err)
	}
	if len(imported) != 1 || imported[0].ProxyCount != 1 {
		t.Fatalf("unexpected imported servers: %#v", imported)
	}
}

func TestImportReplaceKeepsConfigWhenBundleInvalid(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

	if _, err := svc.CreateServer(ctx, app.ServerInput{
		Name:       "keep-me",
		ServerAddr: "frp.example.com",
		ServerPort: 7000,
	}); err != nil {
		t.Fatalf("create server: %v", err)
	}

	// 名称为空的服务器会校验失败；replace 导入必须在删除现有配置前发现这一点。
	_, err = svc.ImportConfig(ctx, app.ConfigImportInput{
		Mode: "replace",
		Bundle: app.ConfigBundle{
			Version: 1,
			Servers: []app.ServerBundle{{Server: app.Server{Name: "", ServerAddr: "x", ServerPort: 7000}}},
		},
	})
	if err == nil {
		t.Fatal("expected import of invalid bundle to fail")
	}

	servers, err := svc.Servers(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "keep-me" {
		t.Fatalf("existing config was destroyed by failed replace import: %#v", servers)
	}
}

func TestServiceRejectsInvalidRequestHeader(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

	server, err := svc.CreateServer(ctx, app.ServerInput{Name: "main", ServerAddr: "frp.example.com", ServerPort: 7000, AdminPort: 17400})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	badHeaders := []string{"missing-delimiter", "bad header: value"}
	for _, header := range badHeaders {
		_, err := svc.CreateRule(ctx, server.ID, app.ProxyRuleInput{
			Name: "web", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080,
			CustomDomains: []string{"app.example.com"}, Enabled: true,
			RequestHeaders: []string{header},
		})
		if !errors.Is(err, app.ErrInvalidInput) {
			t.Fatalf("CreateRule with header %q error = %v, want ErrInvalidInput", header, err)
		}
	}

	if _, err := svc.CreateRule(ctx, server.ID, app.ProxyRuleInput{
		Name: "web", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080,
		CustomDomains: []string{"app.example.com"}, Enabled: true,
		RequestHeaders: []string{"X-Forwarded-Proto: https"},
	}); err != nil {
		t.Fatalf("CreateRule with valid header: %v", err)
	}
}

func TestIsValidHeaderName(t *testing.T) {
	cases := map[string]bool{
		"X-Forwarded-For": true,
		"X_Custom_1":      true,
		"":                false,
		"has space":       false,
		"bad:colon":       false,
	}
	for name, want := range cases {
		if got := app.IsValidHeaderName(name); got != want {
			t.Fatalf("IsValidHeaderName(%q) = %v, want %v", name, got, want)
		}
	}
}

type fakeRuntime struct {
	latest       string
	latestProxy  string
	installInput app.FRPCInstallOnlineInput
	adminStatus  app.AdminStatus
	adminErr     error
	alive        bool
}

func (r *fakeRuntime) RenderConfig(context.Context, app.Server) (app.ConfigPreview, error) {
	return app.ConfigPreview{}, nil
}

func (r *fakeRuntime) CheckConfig(context.Context, app.Server, app.FRPCVersion) app.ActionResult {
	return app.ActionResult{OK: true, Message: "ok"}
}

func (r *fakeRuntime) Start(context.Context, app.Server, app.FRPCVersion) (app.ProcessInfo, app.ActionResult) {
	return app.ProcessInfo{}, app.ActionResult{OK: true, Message: "ok"}
}

func (r *fakeRuntime) Stop(context.Context, app.Server, app.ProcessInfo) app.ActionResult {
	return app.ActionResult{OK: true, Message: "ok"}
}

func (r *fakeRuntime) Reload(context.Context, app.Server, app.FRPCVersion) app.ActionResult {
	return app.ActionResult{OK: true, Message: "ok"}
}

func (r *fakeRuntime) Logs(context.Context, string, int) ([]app.LogLine, error) {
	return []app.LogLine{}, nil
}

func (r *fakeRuntime) InstallOnline(_ context.Context, input app.FRPCInstallOnlineInput) (app.FRPCVersion, error) {
	r.installInput = input
	return app.FRPCVersion{
		Version:   input.Version,
		Platform:  input.Platform,
		Arch:      input.Arch,
		Path:      "/tmp/frpc",
		Source:    "online",
		Installed: true,
	}, nil
}

func (r *fakeRuntime) InstallOffline(context.Context, string, io.Reader) (app.FRPCVersion, error) {
	return app.FRPCVersion{}, nil
}

func (r *fakeRuntime) LatestVersion(_ context.Context, githubProxy string) (string, error) {
	r.latestProxy = githubProxy
	if r.latest == "" {
		return "0.70.0", nil
	}
	return r.latest, nil
}

func (r *fakeRuntime) AdminStatus(context.Context, app.Server) (app.AdminStatus, error) {
	return r.adminStatus, r.adminErr
}

func (r *fakeRuntime) ProcessAlive(context.Context, int) bool {
	return r.alive
}

func (r *fakeRuntime) SetExitHandler(func(string, error)) {}

func (r *fakeRuntime) Adopt(string, int) {}
