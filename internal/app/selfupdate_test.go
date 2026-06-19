package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
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

	// 空公钥 = 未启用签名校验，放行。
	if err := verifyChecksumSignature("", sums, sig); err != nil {
		t.Fatalf("empty pubkey should skip: %v", err)
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
