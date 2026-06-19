package app_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
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
	if !status.MustChangePassword {
		t.Fatalf("fresh store should require password change: %#v", status)
	}

	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "wrong-password"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("wrong login error = %v, want invalid credentials", err)
	}
	// 出厂默认密钥可登录，但仍需强制改密。
	session, err := svc.Login(ctx, app.AuthInput{AccessKey: app.DefaultAccessKey}, meta)
	if err != nil {
		t.Fatalf("login with default key: %v", err)
	}
	if session.Token == "" {
		t.Fatalf("unexpected login session: %#v", session)
	}
	if _, err := svc.VerifySession(ctx, session.Token); err != nil {
		t.Fatalf("verify session: %v", err)
	}
	if !svc.RequiresPasswordChange(ctx) {
		t.Fatal("should still require password change after default-key login")
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

	// 弱密码被策略拒绝（缺大写、缺数字、含非字母数字字符均不通过）。
	for _, weak := range []string{"password", "password123", "Password", "Short1A", "Password-123"} {
		if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{NewAccessKey: weak}); !errors.Is(err, app.ErrInvalidInput) {
			t.Fatalf("weak password %q error = %v, want invalid input", weak, err)
		}
	}

	// 首次设置自己的密码：仍是初始密钥状态，无需提供当前密钥（有效会话即凭证）。
	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{NewAccessKey: "Password123"}); err != nil {
		t.Fatalf("set initial password: %v", err)
	}
	if svc.RequiresPasswordChange(ctx) {
		t.Fatal("should not require password change after setting one")
	}
	// 改密后旧会话与初始密钥同时失效。
	if _, err := svc.VerifySession(ctx, session.Token); !errors.Is(err, app.ErrUnauthorized) {
		t.Fatalf("old session verify error = %v, want unauthorized", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: app.DefaultAccessKey}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("default key after set error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "Password123"}, meta); err != nil {
		t.Fatalf("login with new password: %v", err)
	}

	// 常规改密需校验当前密码。
	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{CurrentAccessKey: "WrongPass9", NewAccessKey: "NewPass456"}); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("change with wrong current error = %v, want invalid credentials", err)
	}
	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{CurrentAccessKey: "Password123", NewAccessKey: "NewPass456"}); err != nil {
		t.Fatalf("change access key: %v", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "Password123"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("old password login error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "NewPass456"}, meta); err != nil {
		t.Fatalf("new password login: %v", err)
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
	if !status.MustChangePassword {
		t.Fatalf("env key install should require password change: %#v", status)
	}
	// env 覆盖出厂默认：默认密钥不再是有效初始密钥，env 密钥才是。
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: app.DefaultAccessKey}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("default key with env override error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "password123"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("wrong env key login error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "env-secret-123"}, meta); err != nil {
		t.Fatalf("env key login: %v", err)
	}
	// 设置用户密码后，env 初始密钥失效。
	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{NewAccessKey: "Password123"}); err != nil {
		t.Fatalf("set password: %v", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "env-secret-123"}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("env key after set error = %v, want invalid credentials", err)
	}
	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: "Password123"}, meta); err != nil {
		t.Fatalf("user password login: %v", err)
	}
}

func TestServiceAudit(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

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

// 恶意/共享的配置 bundle 不能通过 version.Path 指向受管目录之外的可执行文件，
// 否则激活后 Start 会 exec 它。导入时必须拒绝目录外路径，只保留受管目录内真实存在的二进制。
func TestImportRejectsVersionPathOutsideManagedDir(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := storage.Open(ctx, dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

	// 受管目录内、真实存在的二进制：应被接受。
	legitPath := filepath.Join(dataDir, "bin", "frpc", "9.9.9", "frpc")
	if err := os.MkdirAll(filepath.Dir(legitPath), 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(legitPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write legit binary: %v", err)
	}

	if _, err := svc.ImportConfig(ctx, app.ConfigImportInput{
		Mode: "merge",
		Bundle: app.ConfigBundle{
			Version: 1,
			Versions: []app.FRPCVersion{
				{Version: "evil", Path: "/bin/sh", Installed: true, Active: true},
				{Version: "9.9.9", Path: legitPath, Installed: true},
			},
		},
	}); err != nil {
		t.Fatalf("import: %v", err)
	}

	versions, err := svc.Versions(ctx)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	foundLegit := false
	for _, v := range versions {
		if v.Path == "/bin/sh" || v.Version == "evil" {
			t.Fatalf("import accepted out-of-tree version path: %#v", v)
		}
		if v.Path == legitPath {
			foundLegit = true
		}
	}
	if !foundLegit {
		t.Fatalf("import dropped legitimate in-tree version; got %#v", versions)
	}
}

type fakeRuntime struct {
	latest        string
	latestProxy   string
	installInput  app.FRPCInstallOnlineInput
	alive         bool
	proxyStatuses []app.ProxyStatus
	proxyErr      error
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

func (r *fakeRuntime) ProxyStatus(context.Context, app.Server) ([]app.ProxyStatus, error) {
	if r.proxyErr != nil {
		return nil, r.proxyErr
	}
	return r.proxyStatuses, nil
}

func (r *fakeRuntime) ProcessAlive(context.Context, int) bool {
	return r.alive
}

func (r *fakeRuntime) SetExitHandler(func(string, error)) {}

func (r *fakeRuntime) Adopt(string, int) {}
