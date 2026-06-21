package app_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
	"github.com/sccens/frpc-web/internal/storage"
)

// fakeRuntime 实现 app.Runtime（v2.0 只读三件套），供 service 测试使用。
type fakeRuntime struct {
	proxyStatuses []app.ProxyStatus
	proxyErr      error
	reloadErr     error
	logs          []app.LogLine
}

func (r *fakeRuntime) Logs(context.Context, string, int) ([]app.LogLine, error) {
	return r.logs, nil
}

func (r *fakeRuntime) ProxyStatus(context.Context, app.Server) ([]app.ProxyStatus, error) {
	if r.proxyErr != nil {
		return nil, r.proxyErr
	}
	return r.proxyStatuses, nil
}

func (r *fakeRuntime) Reload(context.Context, app.Server) error {
	return r.reloadErr
}

func TestServiceAccessKeyAndSettings(t *testing.T) {
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

	// 弱密码被策略拒绝。
	for _, weak := range []string{"password", "password123", "Password", "Short1A", "Password-123"} {
		if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{NewAccessKey: weak}); !errors.Is(err, app.ErrInvalidInput) {
			t.Fatalf("weak password %q error = %v, want invalid input", weak, err)
		}
	}

	// 首次设置密码：无需当前密钥。
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

	// 常规改密需校验当前密码。
	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{CurrentAccessKey: "WrongPass9", NewAccessKey: "NewPass456"}); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("change with wrong current error = %v, want invalid credentials", err)
	}
	if err := svc.ChangeAccessKey(ctx, app.AccessKeyInput{CurrentAccessKey: "Password123", NewAccessKey: "NewPass456"}); err != nil {
		t.Fatalf("change access key: %v", err)
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

	if _, err := svc.Login(ctx, app.AuthInput{AccessKey: app.DefaultAccessKey}, meta); !errors.Is(err, app.ErrInvalidCredentials) {
		t.Fatalf("default key with env override error = %v, want invalid credentials", err)
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

// 写一个 frpc 配置文件到扫描路径，验证扫描解析、敏感字段掩码、导出。
func TestConfigScanAndExport(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("FRPC_WEB_CONFIG_PATH", dir)

	cfgPath := filepath.Join(dir, "frpc.toml")
	content := "serverAddr = \"frp.example.com\"\n" +
		"serverPort = 7000\n" +
		"auth.token = \"server-token\"\n" +
		"webServer.addr = \"127.0.0.1\"\n" +
		"webServer.port = 7400\n" +
		"webServer.user = \"admin\"\n" +
		"webServer.password = \"admin-secret\"\n" +
		"[[proxies]]\n" +
		"name = \"ssh\"\n" +
		"type = \"tcp\"\n" +
		"localIP = \"127.0.0.1\"\n" +
		"localPort = 22\n" +
		"remotePort = 6000\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})
	svc.RefreshScan(ctx)

	servers, err := svc.Servers(ctx)
	if err != nil {
		t.Fatalf("servers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("want 1 server, got %d", len(servers))
	}
	s := servers[0]
	if s.ServerAddr != "frp.example.com" || s.ServerPort != 7000 || s.AdminPort != 7400 {
		t.Fatalf("unexpected server fields: %#v", s)
	}
	if s.ProxyCount != 1 || len(s.Rules) != 1 || s.Rules[0].Name != "ssh" {
		t.Fatalf("unexpected rules: %#v", s.Rules)
	}
	if s.ConfigPath != cfgPath {
		t.Fatalf("config path = %q, want %q", s.ConfigPath, cfgPath)
	}
	// 敏感字段应被掩码（首尾各 2 字符 + ****）。
	if s.AuthToken != "se****en" {
		t.Fatalf("auth token not masked: %q", s.AuthToken)
	}
	if s.AdminPassword != "ad****et" {
		t.Fatalf("admin password not masked: %q", s.AdminPassword)
	}

	// 导出应包含原文（含明文密钥）。
	bundle, err := svc.ExportConfig(ctx)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(bundle.Files) != 1 || bundle.Files[0].Path != cfgPath || bundle.Files[0].Content != content {
		t.Fatalf("unexpected export: %#v", bundle.Files)
	}
}

// ProxiesStatus 对配置了 admin API 的实例应返回 running + proxies。
func TestProxiesStatus(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("FRPC_WEB_CONFIG_PATH", dir)

	content := "serverAddr = \"frp.example.com\"\nserverPort = 7000\n" +
		"webServer.addr = \"127.0.0.1\"\nwebServer.port = 7400\n" +
		"[[proxies]]\nname = \"ssh\"\ntype = \"tcp\"\nlocalPort = 22\nremotePort = 6000\n"
	if err := os.WriteFile(filepath.Join(dir, "frpc.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	rt := &fakeRuntime{proxyStatuses: []app.ProxyStatus{{Name: "ssh", Phase: "running"}}}
	svc := app.NewService(app.Options{Store: store, Runtime: rt, Addr: "127.0.0.1:8080"})
	svc.RefreshScan(ctx)

	statuses, err := svc.ProxiesStatus(ctx)
	if err != nil {
		t.Fatalf("proxies status: %v", err)
	}
	if len(statuses) != 1 || !statuses[0].Running || len(statuses[0].Proxies) != 1 {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}

// 导入配置应把原文写回扫描路径内的目标文件。
func TestConfigImportWritesFiles(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("FRPC_WEB_CONFIG_PATH", dir)

	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

	target := filepath.Join(dir, "imported.toml")
	bundle := app.ConfigBundle{
		Version: 1,
		Files:   []app.ConfigFile{{Path: target, Content: "serverAddr = \"x\"\nserverPort = 7000\n"}},
	}
	result, err := svc.ImportConfig(ctx, app.ConfigImportInput{Bundle: bundle})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !result.OK {
		t.Fatalf("import not ok: %#v", result)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("imported file not written: %v", err)
	}
}

// 导入必须拒绝写到扫描范围外的任意路径，以及非配置后缀（如 state.json），
// 防止用伪造的 bundle 覆盖面板状态文件或越权写。
func TestConfigImportRejectsUnsafePaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("FRPC_WEB_CONFIG_PATH", dir)

	store, err := storage.Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})

	// 1) 范围外的绝对路径应被跳过。
	outside := filepath.Join(t.TempDir(), "evil.toml")
	// 2) 范围内但非 .toml/.ini 的文件不应被导入覆盖（即便在数据目录内）。
	//    先放一个哨兵文件，导入后内容应保持不变。
	sentinel := filepath.Join(dir, "state.json")
	if err := os.WriteFile(sentinel, []byte("SENTINEL"), 0o600); err != nil {
		t.Fatal(err)
	}

	bundle := app.ConfigBundle{
		Version: 1,
		Files: []app.ConfigFile{
			{Path: outside, Content: "serverAddr = \"x\"\n"},
			{Path: sentinel, Content: "{\"sessions\":[]}"},
		},
	}
	result, err := svc.ImportConfig(ctx, app.ConfigImportInput{Bundle: bundle})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.OK {
		t.Fatalf("unsafe import should not succeed: %#v", result)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("out-of-scope path was written")
	}
	if got, _ := os.ReadFile(sentinel); string(got) != "SENTINEL" {
		t.Fatalf("non-config file was overwritten by import: %q", got)
	}
}
