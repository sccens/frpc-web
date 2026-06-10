package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
)

func TestStoreCRUDAndFilePermissions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if mode := mustMode(t, dir); mode.Perm() != 0o700 {
		t.Fatalf("data dir mode = %v, want 0700", mode.Perm())
	}
	if mode := mustMode(t, filepath.Join(dir, stateFileName)); mode.Perm() != 0o600 {
		t.Fatalf("state file mode = %v, want 0600", mode.Perm())
	}

	version, err := store.AddVersion(ctx, app.FRPCVersion{
		Version:   "0.69.1",
		Platform:  "linux",
		Arch:      "amd64",
		Path:      filepath.Join(dir, "frpc"),
		Source:    "offline",
		Active:    true,
		Installed: true,
	})
	if err != nil {
		t.Fatalf("add version: %v", err)
	}
	active, err := store.ActiveVersion(ctx)
	if err != nil {
		t.Fatalf("active version: %v", err)
	}
	if active.ID != version.ID || !active.Active {
		t.Fatalf("unexpected active version: %#v", active)
	}

	server, err := store.CreateServer(ctx, app.ServerInput{
		Name:              "Home Lab",
		ServerAddr:        "frp.example.com",
		ServerPort:        7000,
		AuthToken:         "secret",
		TransportProtocol: "tcp",
		AutoStart:         true,
		AutoRestart:       true,
		MaxRestarts:       4,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if server.AdminPort == 0 {
		t.Fatal("admin port should be allocated")
	}
	if server.AdminUser == "" || server.AdminPassword == "" {
		t.Fatal("admin credentials should be defaulted")
	}
	if !server.AutoRestart || server.MaxRestarts != 4 {
		t.Fatalf("unexpected restart policy: auto=%v max=%d", server.AutoRestart, server.MaxRestarts)
	}

	rule, err := store.CreateRule(ctx, server.ID, app.ProxyRuleInput{
		Name:       "ssh",
		Type:       "tcp",
		LocalIP:    "127.0.0.1",
		LocalPort:  22,
		RemotePort: 6022,
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if !rule.Enabled || rule.RemotePort != 6022 {
		t.Fatalf("unexpected rule: %#v", rule)
	}
	if _, err := store.CreateRule(ctx, server.ID, app.ProxyRuleInput{
		Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6023, Enabled: true,
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate rule name error = %v, want already exists", err)
	}

	servers, err := store.ListServers(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 || servers[0].ProxyCount != 1 {
		t.Fatalf("unexpected servers: %#v", servers)
	}

	// 重新打开验证持久化：状态应从 state.json 完整恢复。
	reopened, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	restored, err := reopened.GetServer(ctx, server.ID)
	if err != nil {
		t.Fatalf("get restored server: %v", err)
	}
	if restored.AuthToken != "secret" || restored.ProxyCount != 1 {
		t.Fatalf("unexpected restored server: %#v", restored)
	}
	if _, err := reopened.ActiveVersion(ctx); err != nil {
		t.Fatalf("restored active version: %v", err)
	}
}

func TestStoreNotFoundAndAuditCap(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	if _, err := store.GetServer(ctx, "missing"); err != app.ErrNotFound {
		t.Fatalf("get missing server error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetSetting(ctx, "missing"); err != app.ErrNotFound {
		t.Fatalf("get missing setting error = %v, want ErrNotFound", err)
	}

	for i := 0; i < maxAuditEntries+50; i++ {
		if err := store.AddAudit(ctx, app.AuditLogInput{Action: "test", Result: "success"}); err != nil {
			t.Fatalf("add audit: %v", err)
		}
	}
	page, err := store.ListAuditLogs(ctx, app.AuditLogQuery{PageSize: 10})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if page.Total != maxAuditEntries {
		t.Fatalf("audit total = %d, want capped at %d", page.Total, maxAuditEntries)
	}
	if err := store.ClearAuditLogs(ctx); err != nil {
		t.Fatalf("clear audit: %v", err)
	}
	page, err = store.ListAuditLogs(ctx, app.AuditLogQuery{})
	if err != nil || page.Total != 0 {
		t.Fatalf("audit after clear: total=%d err=%v", page.Total, err)
	}
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode()
}
