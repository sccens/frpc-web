package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.1", "v1.0.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.9.0", "v1.10.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"1.0.0", "v1.1.0", true},
		{"v1.0.0", "v1.1.0-rc1", true},
		{"dev", "v9.9.9", false},
		{"v1.0.0", "", false},
		{"", "v1.0.0", false},
	}
	for _, tc := range cases {
		if got := versionLess(tc.current, tc.latest); got != tc.want {
			t.Errorf("versionLess(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

func TestChecksumFromSums(t *testing.T) {
	sums := "abc123  frpc-web_linux_amd64\ndef456  frpc-web_darwin_arm64\n789aaa *frpc-web_linux_arm64\n"
	if got := checksumFromSums(sums, "frpc-web_darwin_arm64"); got != "def456" {
		t.Fatalf("expected def456, got %q", got)
	}
	// sha256sum 二进制模式会给文件名加 * 前缀
	if got := checksumFromSums(sums, "frpc-web_linux_arm64"); got != "789aaa" {
		t.Fatalf("expected 789aaa, got %q", got)
	}
	if got := checksumFromSums(sums, "missing"); got != "" {
		t.Fatalf("expected empty for missing entry, got %q", got)
	}
}

func TestVerifyChecksumSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	sums := []byte("abc123  frpc-web_linux_amd64\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)))

	// 空公钥 = 一键更新不可用，避免只依赖可被代理同时替换的 SHA256SUMS。
	if err := verifyChecksumSignature("", sums, sig); err == nil {
		t.Fatal("empty pubkey accepted")
	}
	// 合法签名通过。
	if err := verifyChecksumSignature(pubB64, sums, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	// 校验和被篡改 → 拒绝。
	if err := verifyChecksumSignature(pubB64, []byte("tampered\n"), sig); err == nil {
		t.Fatal("tampered sums accepted")
	}
	// 用别的私钥签的名 → 拒绝。
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	badSig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(otherPriv, sums)))
	if err := verifyChecksumSignature(pubB64, sums, badSig); err == nil {
		t.Fatal("signature from wrong key accepted")
	}
	// 公钥配置无效 → 报错。
	if err := verifyChecksumSignature("not-base64!!", sums, sig); err == nil {
		t.Fatal("invalid pubkey accepted")
	}
}

func TestValidateReleaseSigningPublicKey(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	if err := validateReleaseSigningPublicKey(base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatalf("valid pubkey rejected: %v", err)
	}
	if err := validateReleaseSigningPublicKey(""); err == nil {
		t.Fatal("empty pubkey accepted")
	}
	if err := validateReleaseSigningPublicKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("short pubkey accepted")
	}
}

func TestCheckUpdateRequiresSigningPublicKey(t *testing.T) {
	oldClient := updateHTTPClient
	oldKey := releaseSigningPublicKey
	defer func() {
		updateHTTPClient = oldClient
		releaseSigningPublicKey = oldKey
	}()
	releaseSigningPublicKey = ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "api.github.com/repos/") {
			t.Fatalf("unexpected update URL path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer ts.Close()
	updateHTTPClient = ts.Client()

	store := &updateTestStore{settings: map[string]string{"github_proxy": ts.URL}}
	svc := NewService(Options{Store: store, Runtime: updateTestRuntime{}, Addr: "127.0.0.1:8080", Version: "v1.0.0"})
	check, err := svc.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("check update: %v", err)
	}
	if !check.HasUpdate {
		t.Fatal("expected update to be detected")
	}
	if check.CanApply {
		t.Fatal("missing signing key should disable one-click update")
	}
	if !strings.Contains(check.ApplyHint, "发布签名公钥") {
		t.Fatalf("unexpected apply hint: %q", check.ApplyHint)
	}
}

type updateTestStore struct {
	settings map[string]string
}

func (s *updateTestStore) DataDir() string { return "" }

func (s *updateTestStore) GetSetting(_ context.Context, key string) (string, error) {
	if s.settings == nil {
		return "", ErrNotFound
	}
	value, ok := s.settings[key]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (s *updateTestStore) SetSetting(_ context.Context, key, value string) error {
	if s.settings == nil {
		s.settings = map[string]string{}
	}
	s.settings[key] = value
	return nil
}

func (s *updateTestStore) CreateSession(context.Context, Session) (Session, error) {
	return Session{}, nil
}

func (s *updateTestStore) GetSessionByHash(context.Context, string) (Session, error) {
	return Session{}, ErrNotFound
}

func (s *updateTestStore) TouchSession(context.Context, string) error { return nil }

func (s *updateTestStore) RevokeSessionByHash(context.Context, string) error { return nil }

func (s *updateTestStore) RevokeAllSessions(context.Context) error { return nil }

func (s *updateTestStore) ListHealth(context.Context) ([]HealthEvent, error) { return nil, nil }

func (s *updateTestStore) AddHealth(context.Context, string, string, string) error { return nil }

func (s *updateTestStore) AddAudit(context.Context, AuditLogInput) error { return nil }

func (s *updateTestStore) ListAuditLogs(context.Context, AuditLogQuery) (AuditLogPage, error) {
	return AuditLogPage{}, nil
}

func (s *updateTestStore) ClearAuditLogs(context.Context) error { return nil }

type updateTestRuntime struct{}

func (updateTestRuntime) Logs(context.Context, string, int) ([]LogLine, error) { return nil, nil }

func (updateTestRuntime) ProxyStatus(context.Context, Server) ([]ProxyStatus, error) {
	return nil, nil
}

func (updateTestRuntime) Reload(context.Context, Server) error { return nil }
