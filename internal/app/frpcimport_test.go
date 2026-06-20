package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
	"github.com/sccens/frpc-web/internal/storage"
)

func newImportService(t *testing.T, runtime app.Runtime) (*app.Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return app.NewService(app.Options{Store: store, Runtime: runtime, Addr: "127.0.0.1:8080"}), ctx
}

func findRule(rules []app.ProxyRule, name string) (app.ProxyRule, bool) {
	for _, rule := range rules {
		if rule.Name == name {
			return rule, true
		}
	}
	return app.ProxyRule{}, false
}

func TestImportFrpcConfigTOML(t *testing.T) {
	svc, ctx := newImportService(t, &fakeRuntime{})
	const config = `
# managed by hand
serverAddr = "frp.example.com"
serverPort = 7000
auth.token = "server-token"
transport.protocol = "kcp"
webServer.addr = "127.0.0.1"
webServer.port = 7400
webServer.user = "admin"
webServer.password = "admin-pass"

[[proxies]]
name = "ssh"
type = "tcp"
localIP = "127.0.0.1"
localPort = 22
remotePort = 6000

[[proxies]]
name = "web"
type = "http"
localPort = 8080
customDomains = ["a.example.com", "b.example.com"]
transport.useEncryption = true
requestHeaders.set.X-From-Where = "frp"

[[visitors]]
name = "ssh-visitor"
type = "stcp"
serverName = "ssh-server"
secretKey = "sk123"
bindAddr = "127.0.0.1"
bindPort = 6022
`
	server, err := svc.ImportFrpcConfig(ctx, app.ImportFrpcConfigInput{Name: "prod", Content: config})
	if err != nil {
		t.Fatalf("import toml: %v", err)
	}
	if server.Name != "prod" || server.ServerAddr != "frp.example.com" || server.ServerPort != 7000 {
		t.Fatalf("unexpected server fields: %#v", server)
	}
	if server.TransportProtocol != "kcp" || server.AdminPort != 7400 || server.AdminUser != "admin" {
		t.Fatalf("unexpected server transport/admin: %#v", server)
	}
	if server.ProxyCount != 3 {
		t.Fatalf("expected 3 rules, got %d: %#v", server.ProxyCount, server.Rules)
	}
	web, ok := findRule(server.Rules, "web")
	if !ok {
		t.Fatalf("web rule missing: %#v", server.Rules)
	}
	if web.Type != "http" || web.LocalPort != 8080 || !web.UseEncryption {
		t.Fatalf("unexpected web rule: %#v", web)
	}
	if len(web.CustomDomains) != 2 || web.CustomDomains[0] != "a.example.com" {
		t.Fatalf("unexpected custom domains: %#v", web.CustomDomains)
	}
	if len(web.RequestHeaders) != 1 || web.RequestHeaders[0] != "X-From-Where: frp" {
		t.Fatalf("unexpected request headers: %#v", web.RequestHeaders)
	}
	visitor, ok := findRule(server.Rules, "ssh-visitor")
	if !ok || visitor.Role != "visitor" || visitor.ServerName != "ssh-server" || visitor.BindPort != 6022 {
		t.Fatalf("unexpected visitor rule: %#v", visitor)
	}
}

func TestImportFrpcConfigINI(t *testing.T) {
	svc, ctx := newImportService(t, &fakeRuntime{})
	const config = `
[common]
server_addr = frp.example.com
server_port = 7000
token = server-token
protocol = tcp
admin_port = 7400
admin_user = admin
admin_pwd = admin-pass

[ssh]
type = tcp
local_ip = 127.0.0.1
local_port = 22
remote_port = 6000

[web]
type = http
local_port = 8080
custom_domains = a.example.com,b.example.com
host_header_rewrite = example.com
`
	server, err := svc.ImportFrpcConfig(ctx, app.ImportFrpcConfigInput{Content: config})
	if err != nil {
		t.Fatalf("import ini: %v", err)
	}
	if server.ServerAddr != "frp.example.com" || server.ServerPort != 7000 || server.AdminPort != 7400 {
		t.Fatalf("unexpected server fields: %#v", server)
	}
	if server.ProxyCount != 2 {
		t.Fatalf("expected 2 rules, got %d: %#v", server.ProxyCount, server.Rules)
	}
	web, ok := findRule(server.Rules, "web")
	if !ok || web.Type != "http" || web.HostHeaderRewrite != "example.com" {
		t.Fatalf("unexpected web rule: %#v", web)
	}
	if len(web.CustomDomains) != 2 {
		t.Fatalf("unexpected custom domains: %#v", web.CustomDomains)
	}
}

func TestImportFrpcConfigRejectsGarbage(t *testing.T) {
	svc, ctx := newImportService(t, &fakeRuntime{})
	if _, err := svc.ImportFrpcConfig(ctx, app.ImportFrpcConfigInput{Content: "not a config\n"}); err == nil {
		t.Fatal("expected garbage content to be rejected")
	}
}

func TestDiscoverFRPCPassesThrough(t *testing.T) {
	runtime := &fakeRuntime{
		binaries:  []app.FRPCBinaryCandidate{{Path: "/usr/local/bin/frpc", Version: "0.70.0"}},
		processes: []app.FRPCProcessCandidate{{PID: 999999, ConfigPath: "/etc/frp/frpc.toml"}},
	}
	svc, ctx := newImportService(t, runtime)
	discovery, err := svc.DiscoverFRPC(ctx)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(discovery.Binaries) != 1 || discovery.Binaries[0].Path != "/usr/local/bin/frpc" {
		t.Fatalf("unexpected binaries: %#v", discovery.Binaries)
	}
	if len(discovery.Processes) != 1 || discovery.Processes[0].Managed {
		t.Fatalf("unexpected processes: %#v", discovery.Processes)
	}
}

func TestAdoptProcessRestart(t *testing.T) {
	ctx := context.Background()
	configPath := filepath.Join(t.TempDir(), "frpc.toml")
	if err := os.WriteFile(configPath, []byte("serverAddr = \"frp.example.com\"\nserverPort = 7000\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{
		processes: []app.FRPCProcessCandidate{{PID: 4242, Exe: "", ConfigPath: configPath}},
	}
	svc, _ := newImportService(t, runtime)

	// 先植入一个可用版本，restart 模式才能重启拉起。
	if _, err := svc.InstallOnline(ctx, app.FRPCInstallOnlineInput{Version: "0.70.0", Platform: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("seed version: %v", err)
	}

	result, err := svc.AdoptProcess(ctx, app.AdoptProcessInput{PID: 4242, Mode: "restart"})
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	if !result.Started {
		t.Fatalf("expected adopted process to be started: %#v", result)
	}
	if result.Server.ServerAddr != "frp.example.com" {
		t.Fatalf("unexpected adopted server: %#v", result.Server)
	}
}

func TestAdoptProcessMissingPID(t *testing.T) {
	runtime := &fakeRuntime{processes: []app.FRPCProcessCandidate{{PID: 1, ConfigPath: "/x"}}}
	svc, ctx := newImportService(t, runtime)
	if _, err := svc.AdoptProcess(ctx, app.AdoptProcessInput{PID: 4242, Mode: "restart"}); err == nil {
		t.Fatal("expected adopt of unknown pid to fail")
	}
}
