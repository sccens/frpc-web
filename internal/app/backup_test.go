package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sccens/frpc-web/internal/app"
	"github.com/sccens/frpc-web/internal/storage"
)

func newBackupTestService(t *testing.T) (*app.Service, *storage.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	svc := app.NewService(app.Options{Store: store, Runtime: &fakeRuntime{}, Addr: "127.0.0.1:8080"})
	return svc, store
}

func createBackupFixture(t *testing.T, svc *app.Service) app.Server {
	t.Helper()
	ctx := context.Background()
	server, err := svc.CreateServer(ctx, app.ServerInput{
		Name:       "main",
		ServerAddr: "frp.example.com",
		ServerPort: 7000,
		AuthToken:  "server-token",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := svc.CreateRule(ctx, server.ID, app.ProxyRuleInput{
		Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6022, Enabled: true,
	}); err != nil {
		t.Fatalf("create rule: %v", err)
	}
	return server
}

func TestBackupNowListAndRestore(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBackupTestService(t)
	createBackupFixture(t, svc)

	file, err := svc.BackupNow(ctx)
	if err != nil {
		t.Fatalf("backup now: %v", err)
	}
	if file.Name == "" || file.Size == 0 {
		t.Fatalf("unexpected backup file: %#v", file)
	}

	files, err := svc.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(files) != 1 || files[0].Name != file.Name {
		t.Fatalf("unexpected backup list: %#v", files)
	}

	data, err := svc.ReadBackup(ctx, file.Name)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	var bundle app.ConfigBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("backup is not a valid bundle: %v", err)
	}
	if len(bundle.Servers) != 1 || bundle.Servers[0].Server.AuthToken != "server-token" {
		t.Fatalf("backup bundle missing secrets: %#v", bundle.Servers)
	}

	// 制造漂移后用 replace 恢复，应回到备份时的单服务器状态。
	if _, err := svc.CreateServer(ctx, app.ServerInput{Name: "extra", ServerAddr: "other.example.com", ServerPort: 7000}); err != nil {
		t.Fatalf("create extra server: %v", err)
	}
	result, err := svc.RestoreBackup(ctx, file.Name, "replace")
	if err != nil || !result.OK {
		t.Fatalf("restore backup: result=%#v err=%v", result, err)
	}
	servers, err := svc.Servers(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "main" || servers[0].ProxyCount != 1 {
		t.Fatalf("unexpected servers after restore: %#v", servers)
	}
}

func TestReadBackupRejectsUnsafeNames(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBackupTestService(t)
	for _, name := range []string{"../state.json", "auto-backup-20260612-120000.json.bak", "x.json", "auto-backup-../-120000.json"} {
		if _, err := svc.ReadBackup(ctx, name); !errors.Is(err, app.ErrInvalidInput) {
			t.Fatalf("ReadBackup(%q) error = %v, want ErrInvalidInput", name, err)
		}
	}
	if _, err := svc.ReadBackup(ctx, "auto-backup-20990101-000000.json"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("missing backup error = %v, want ErrNotFound", err)
	}
}

func TestAutoBackupDedupeAndPrune(t *testing.T) {
	ctx := context.Background()
	svc, store := newBackupTestService(t)
	server := createBackupFixture(t, svc)

	enabled := true
	interval := 1
	maxFiles := 2
	if _, err := svc.UpdateSettings(ctx, app.SettingsInput{
		AutoBackupEnabled:       &enabled,
		AutoBackupIntervalHours: &interval,
		AutoBackupMaxFiles:      &maxFiles,
	}); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	backupCount := func() int {
		t.Helper()
		files, err := svc.ListBackups(ctx)
		if err != nil {
			t.Fatalf("list backups: %v", err)
		}
		return len(files)
	}
	rewindLastRun := func() {
		t.Helper()
		past := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
		if err := store.SetSetting(ctx, "auto_backup_last_run", past); err != nil {
			t.Fatalf("rewind last run: %v", err)
		}
	}

	svc.AutoBackupCheck(ctx)
	if got := backupCount(); got != 1 {
		t.Fatalf("first check backups = %d, want 1", got)
	}

	// 间隔未到：不应产生新备份。
	svc.AutoBackupCheck(ctx)
	if got := backupCount(); got != 1 {
		t.Fatalf("within interval backups = %d, want 1", got)
	}

	// 间隔已到但内容未变：哈希去重应跳过。
	rewindLastRun()
	svc.AutoBackupCheck(ctx)
	if got := backupCount(); got != 1 {
		t.Fatalf("unchanged content backups = %d, want 1", got)
	}

	// 配置变化后应写出第二份。
	time.Sleep(20 * time.Millisecond)
	if _, err := svc.CreateRule(ctx, server.ID, app.ProxyRuleInput{
		Name: "web", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 8080, RemotePort: 6080, Enabled: true,
	}); err != nil {
		t.Fatalf("create second rule: %v", err)
	}
	rewindLastRun()
	svc.AutoBackupCheck(ctx)
	if got := backupCount(); got != 2 {
		t.Fatalf("changed content backups = %d, want 2", got)
	}

	// 第三份触发保留上限清理，且留下的是最新的两份。
	time.Sleep(20 * time.Millisecond)
	if err := store.DeleteRule(ctx, server.ID, mustFirstRuleID(t, store, server.ID)); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	rewindLastRun()
	svc.AutoBackupCheck(ctx)
	files, err := svc.ListBackups(ctx)
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("after prune backups = %d, want 2: %#v", len(files), files)
	}

	settings, err := svc.Settings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if settings.LastAutoBackupAt == "" {
		t.Fatal("LastAutoBackupAt should be recorded")
	}
}

func mustFirstRuleID(t *testing.T, store *storage.Store, serverID string) string {
	t.Helper()
	rules, err := store.ListRules(context.Background(), serverID)
	if err != nil || len(rules) == 0 {
		t.Fatalf("list rules: %v (%d)", err, len(rules))
	}
	return rules[0].ID
}

func TestUpdateSettingsAutoBackupValidation(t *testing.T) {
	ctx := context.Background()
	svc, _ := newBackupTestService(t)

	bad := 0
	if _, err := svc.UpdateSettings(ctx, app.SettingsInput{AutoBackupIntervalHours: &bad}); !errors.Is(err, app.ErrInvalidInput) {
		t.Fatalf("interval 0 error = %v, want ErrInvalidInput", err)
	}
	if _, err := svc.UpdateSettings(ctx, app.SettingsInput{AutoBackupMaxFiles: &bad}); !errors.Is(err, app.ErrInvalidInput) {
		t.Fatalf("max files 0 error = %v, want ErrInvalidInput", err)
	}

	// 不带备份字段的更新不应改动既有值。
	enabled := true
	if _, err := svc.UpdateSettings(ctx, app.SettingsInput{AutoBackupEnabled: &enabled}); err != nil {
		t.Fatalf("enable auto backup: %v", err)
	}
	settings, err := svc.UpdateSettings(ctx, app.SettingsInput{GithubProxy: "https://proxy.example/"})
	if err != nil {
		t.Fatalf("update github proxy: %v", err)
	}
	if !settings.AutoBackupEnabled {
		t.Fatal("partial update must not reset auto backup settings")
	}
	if settings.AutoBackupIntervalHours != 24 || settings.AutoBackupMaxFiles != 7 {
		t.Fatalf("unexpected defaults: %#v", settings)
	}
}

func TestProxiesStatus(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	runtime := &fakeRuntime{proxyStatuses: []app.ProxyStatus{
		{Name: "ssh", Type: "tcp", Phase: "running"},
		{Name: "web", Type: "http", Phase: "start error", Err: "port already used"},
	}}
	svc := app.NewService(app.Options{Store: store, Runtime: runtime, Addr: "127.0.0.1:8080"})

	running, err := svc.CreateServer(ctx, app.ServerInput{Name: "running", ServerAddr: "a.example.com", ServerPort: 7000})
	if err != nil {
		t.Fatalf("create running server: %v", err)
	}
	stopped, err := svc.CreateServer(ctx, app.ServerInput{Name: "stopped", ServerAddr: "b.example.com", ServerPort: 7000})
	if err != nil {
		t.Fatalf("create stopped server: %v", err)
	}
	if err := store.SetServerStatus(ctx, running.ID, "running"); err != nil {
		t.Fatalf("set status: %v", err)
	}

	statuses, err := svc.ProxiesStatus(ctx)
	if err != nil {
		t.Fatalf("proxies status: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want 2", len(statuses))
	}
	byID := map[string]app.ServerProxyStatus{}
	for _, status := range statuses {
		byID[status.ServerID] = status
	}
	got := byID[running.ID]
	if !got.Running || got.Error != "" || len(got.Proxies) != 2 || got.Proxies[0].Phase != "running" {
		t.Fatalf("unexpected running server status: %#v", got)
	}
	if idle := byID[stopped.ID]; idle.Running || len(idle.Proxies) != 0 {
		t.Fatalf("unexpected stopped server status: %#v", idle)
	}

	runtime.proxyErr = errors.New("admin api unreachable")
	statuses, err = svc.ProxiesStatus(ctx)
	if err != nil {
		t.Fatalf("proxies status with error: %v", err)
	}
	for _, status := range statuses {
		if status.ServerID == running.ID && status.Error == "" {
			t.Fatalf("expected per-server error, got %#v", status)
		}
	}
}
