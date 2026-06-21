package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
)

func TestStoreSettingsSessionsAndFilePermissions(t *testing.T) {
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

	// settings 持久化
	if err := store.SetSetting(ctx, "github_proxy", "https://proxy.example/"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	got, err := store.GetSetting(ctx, "github_proxy")
	if err != nil || got != "https://proxy.example/" {
		t.Fatalf("get setting: err=%v got=%q", err, got)
	}

	// session 持久化
	sess, err := store.CreateSession(ctx, app.Session{IDHash: "hash-1", ExpiresAt: "2099-01-01T00:00:00Z"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := store.GetSessionByHash(ctx, "hash-1"); err != nil {
		t.Fatalf("get session: %v", err)
	}

	// 重新打开：settings 与 sessions 应从 state.json 完整恢复。
	reopened, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if v, _ := reopened.GetSetting(ctx, "github_proxy"); v != "https://proxy.example/" {
		t.Fatalf("setting not restored: %q", v)
	}
	if _, err := reopened.GetSessionByHash(ctx, sess.IDHash); err != nil {
		t.Fatalf("session not restored: %v", err)
	}
}

func TestStoreNotFoundAndAuditCap(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
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

// v1 的 state.json（含 servers/versions）首启应被备份为 state.json.v1.bak。
func TestStoreBacksUpLegacyState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	legacy := `{"settings":{},"servers":[{"id":"x","name":"old"}],"versions":[]}`
	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, dir)
	if err != nil {
		t.Fatalf("open with legacy state: %v", err)
	}
	defer store.Close()

	backup := filepath.Join(dir, stateFileName+".v1.bak")
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("legacy backup not created: %v", err)
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
