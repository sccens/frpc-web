package storage

import (
	"context"
	"os"
	"path/filepath"
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
	defer store.Close()

	if mode := mustMode(t, dir); mode.Perm() != 0o700 {
		t.Fatalf("data dir mode = %v, want 0700", mode.Perm())
	}
	if mode := mustMode(t, filepath.Join(dir, "app.db")); mode.Perm() != 0o600 {
		t.Fatalf("db mode = %v, want 0600", mode.Perm())
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
		ConfigMode:        "toml_reload",
		AutoStart:         true,
		AutoRestart:       true,
		MaxRestarts:       4,
		FRPCVersionID:     version.ID,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if server.AdminPort == 0 {
		t.Fatal("admin port should be allocated")
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

	servers, err := store.ListServers(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 || servers[0].ProxyCount != 1 {
		t.Fatalf("unexpected servers: %#v", servers)
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
