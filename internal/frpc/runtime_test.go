package frpc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
)

func TestRenderConfigTomlReloadAndFileMode(t *testing.T) {
	rt := New(t.TempDir())
	server := app.Server{
		ID:            "srv-test",
		ServerAddr:    "frp.example.com",
		ServerPort:    7000,
		AuthToken:     "secret",
		AdminPort:     17400,
		AdminUser:     "frpc-web",
		AdminPassword: "admin-secret",
		Rules: []app.ProxyRule{
			{Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6022, Enabled: true},
			{
				Name: "web", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080, CustomDomains: []string{"app.example.com"}, Enabled: true,
				UseEncryption: true, UseCompression: true, BandwidthLimit: "2MB", Locations: []string{"/", "/api"}, HostHeaderRewrite: "internal.local",
				HTTPUser: "admin", HTTPPassword: "web-secret", RequestHeaders: []string{"X-Forwarded-Proto: https"},
			},
			{Name: "ssh-secure", Type: "stcp", LocalIP: "127.0.0.1", LocalPort: 22, SecretKey: "stcp-secret", Role: "server", Enabled: true},
			{Name: "ssh-visitor", Type: "stcp", Role: "visitor", ServerName: "ssh-secure", SecretKey: "stcp-secret", BindAddr: "127.0.0.1", BindPort: 6000, Enabled: true},
			{Name: "disabled", Type: "udp", LocalIP: "127.0.0.1", LocalPort: 53, RemotePort: 6053, Enabled: false},
		},
	}

	preview, err := rt.RenderConfig(context.Background(), server)
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	assertContains(t, preview.Content, `serverAddr = "frp.example.com"`)
	assertContains(t, preview.Content, `auth.token = "secret"`)
	assertContains(t, preview.Content, `webServer.user = "frpc-web"`)
	assertContains(t, preview.Content, `webServer.password = "admin-secret"`)
	assertContains(t, preview.Content, `name = "ssh"`)
	assertContains(t, preview.Content, `name = "web"`)
	assertContains(t, preview.Content, `transport.useEncryption = true`)
	assertContains(t, preview.Content, `transport.bandwidthLimit = "2MB"`)
	assertContains(t, preview.Content, `requestHeaders.set.X-Forwarded-Proto = "https"`)
	assertContains(t, preview.Content, `[[visitors]]`)
	assertContains(t, preview.Content, `serverName = "ssh-secure"`)
	assertContains(t, preview.Content, `bindPort = 6000`)
	if strings.Contains(preview.Content, `name = "disabled"`) {
		t.Fatalf("disabled rule should not be rendered:\n%s", preview.Content)
	}
	if mode := mustMode(t, preview.ConfigPath); mode.Perm() != 0o600 {
		t.Fatalf("config mode = %v, want 0600", mode.Perm())
	}
}

func TestInstallOfflineTarGZUsesBinaryVersion(t *testing.T) {
	rt := New(t.TempDir())
	archive := fakeFrpcArchive(t, "0.69.1")

	version, err := rt.InstallOffline(context.Background(), "frp.tar.gz", bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("install offline: %v", err)
	}
	if version.Version != "0.69.1" {
		t.Fatalf("version = %q, want 0.69.1", version.Version)
	}
	if _, err := os.Stat(filepath.Join(rt.dataDir, "bin", "frpc", "0.69.1", "frpc")); err != nil {
		t.Fatalf("installed binary not found: %v", err)
	}
}

func TestInstallOnlineVerifiesChecksum(t *testing.T) {
	rt := New(t.TempDir())
	archive := fakeFrpcArchive(t, "0.70.0")
	sum := sha256.Sum256(archive)
	assetName := "frp_0.70.0_linux_amd64.tar.gz"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "frp_sha256_checksums.txt"):
			_, _ = w.Write([]byte(hexString(sum[:]) + "  " + assetName + "\n"))
		case strings.Contains(r.URL.Path, assetName):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer proxy.Close()

	version, err := rt.InstallOnline(context.Background(), app.FRPCInstallOnlineInput{
		Version:     "0.70.0",
		Platform:    "linux",
		Arch:        "amd64",
		GithubProxy: proxy.URL,
	})
	if err != nil {
		t.Fatalf("install online: %v", err)
	}
	if version.Version != "0.70.0" || version.Source != "online" {
		t.Fatalf("unexpected version: %#v", version)
	}
}

func TestInstallOnlineRejectsChecksumMismatch(t *testing.T) {
	rt := New(t.TempDir())
	archive := fakeFrpcArchive(t, "0.70.0")
	assetName := "frp_0.70.0_linux_amd64.tar.gz"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "frp_sha256_checksums.txt"):
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + assetName + "\n"))
		case strings.Contains(r.URL.Path, assetName):
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer proxy.Close()

	_, err := rt.InstallOnline(context.Background(), app.FRPCInstallOnlineInput{
		Version:     "0.70.0",
		Platform:    "linux",
		Arch:        "amd64",
		GithubProxy: proxy.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("install error = %v, want checksum mismatch", err)
	}
}

func TestAdminStatusUsesBasicAuth(t *testing.T) {
	rt := New(t.TempDir())
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "frpc-web" || pass != "admin-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tcp": []map[string]any{{
				"name":       "ssh",
				"status":     "online",
				"trafficIn":  10,
				"trafficOut": 20,
			}},
		})
	}))
	defer admin.Close()
	parsed, err := url.Parse(admin.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	_, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	adminPort, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}

	status, err := rt.AdminStatus(context.Background(), app.Server{
		AdminPort:     adminPort,
		AdminUser:     "frpc-web",
		AdminPassword: "admin-secret",
	})
	if err != nil {
		t.Fatalf("admin status: %v", err)
	}
	if len(status.Proxies) != 1 || status.Proxies[0].TrafficIn != 10 || status.Proxies[0].TrafficOut != 20 {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestTailLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpc.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, err := tailLines(path, 2)
	if err != nil {
		t.Fatalf("tail lines: %v", err)
	}
	if strings.Join(lines, ",") != "two,three" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
}

func TestTailLinesReadsLargeFileFromEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpc.log")
	var b strings.Builder
	for i := 0; i < 10000; i++ {
		b.WriteString("line-")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, err := tailLines(path, 3)
	if err != nil {
		t.Fatalf("tail lines: %v", err)
	}
	if strings.Join(lines, ",") != "line-9997,line-9998,line-9999" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
}

func TestRotatingLogWriterRotatesBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frpc.log")
	writer, err := newRotatingLogWriter(path, 12, 2)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if _, err := writer.Write([]byte("first-line\n")); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if _, err := writer.Write([]byte("second-line\n")); err != nil {
		t.Fatalf("write second: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("rotated backup missing: %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	if !strings.Contains(string(current), "second-line") {
		t.Fatalf("unexpected current log: %q", current)
	}
}

func TestGithubURLUsesRequestProxyFirst(t *testing.T) {
	t.Setenv("FRPC_WEB_GITHUB_PROXY", "https://env.example/")
	rt := New(t.TempDir())
	got := rt.githubURL("https://github.com/fatedier/frp", "https://request.example/path/")
	want := "https://request.example/path/https://github.com/fatedier/frp"
	if got != want {
		t.Fatalf("githubURL = %q, want %q", got, want)
	}
}

func TestGithubURLUsesEnvironmentProxyWhenRequestProxyEmpty(t *testing.T) {
	t.Setenv("FRPC_WEB_GITHUB_PROXY", "https://env.example/")
	rt := New(t.TempDir())
	got := rt.githubURL("https://github.com/fatedier/frp", "")
	want := "https://env.example/https://github.com/fatedier/frp"
	if got != want {
		t.Fatalf("githubURL = %q, want %q", got, want)
	}
}

func TestGithubURLCanConnectDirectlyWhenNoProxyConfigured(t *testing.T) {
	t.Setenv("FRPC_WEB_GITHUB_PROXY", "")
	rt := New(t.TempDir())
	rt.githubProxy = ""
	got := rt.githubURL("https://github.com/fatedier/frp", "")
	want := "https://github.com/fatedier/frp"
	if got != want {
		t.Fatalf("githubURL = %q, want %q", got, want)
	}
}

func fakeFrpcArchive(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"frpc version " + version + "\"; exit 0; fi\nexit 0\n"
	header := &tar.Header{
		Name: "frp_" + version + "_linux_amd64/frpc",
		Mode: 0o700,
		Size: int64(len(body)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func hexString(data []byte) string {
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = alphabet[b>>4]
		out[i*2+1] = alphabet[b&0x0f]
	}
	return string(out)
}

func assertContains(t *testing.T, value string, want string) {
	t.Helper()
	if !strings.Contains(value, want) {
		t.Fatalf("expected %q in:\n%s", want, value)
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

func TestHeaderPairsSortedAndFiltered(t *testing.T) {
	got := headerPairs([]string{
		"X-Real-IP: 1.2.3.4",
		"X-Forwarded-Proto = https",
		"bad header: nope",        // space in name -> invalid, skipped
		"X-Forwarded-Proto: http", // duplicate, last value wins
		"   ",                     // empty, skipped
		"NoDelimiterHere",         // no ':' or '=' -> skipped
	})
	want := []headerPair{
		{key: "X-Forwarded-Proto", value: "http"},
		{key: "X-Real-IP", value: "1.2.3.4"},
	}
	if len(got) != len(want) {
		t.Fatalf("headerPairs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("headerPairs[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestRenderConfigRequestHeadersDeterministicAndSorted(t *testing.T) {
	rt := New(t.TempDir())
	server := app.Server{
		ID: "srv-headers", ServerAddr: "frp.example.com", ServerPort: 7000, AdminPort: 17402,
		Rules: []app.ProxyRule{{
			Name: "web", Type: "http", LocalIP: "127.0.0.1", LocalPort: 8080,
			CustomDomains: []string{"app.example.com"}, Enabled: true,
			RequestHeaders: []string{"X-Real-IP: 1.1.1.1", "A-First: a", "M-Middle: m"},
		}},
	}

	first, err := rt.RenderConfig(context.Background(), server)
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	second, err := rt.RenderConfig(context.Background(), server)
	if err != nil {
		t.Fatalf("render config: %v", err)
	}
	if first.Content != second.Content {
		t.Fatalf("render output not deterministic:\n--- first ---\n%s\n--- second ---\n%s", first.Content, second.Content)
	}

	ai := strings.Index(first.Content, "requestHeaders.set.A-First")
	mi := strings.Index(first.Content, "requestHeaders.set.M-Middle")
	xi := strings.Index(first.Content, "requestHeaders.set.X-Real-IP")
	if ai < 0 || mi < 0 || xi < 0 || !(ai < mi && mi < xi) {
		t.Fatalf("request headers not sorted (A=%d M=%d X=%d):\n%s", ai, mi, xi, first.Content)
	}
}
