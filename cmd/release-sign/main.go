// Command release-sign 生成 ed25519 签名密钥，并对发布校验和文件产生分离签名。
// 它是构建/CI 辅助工具，不会被编入 frpc-web 二进制（发布只构建 ./cmd/frpc-web）。
//
// 用法：
//
//	release-sign keygen           # 打印一对新的公钥/私钥
//	release-sign sign SHA256SUMS  # 生成 SHA256SUMS.sig（需要环境变量 RELEASE_SIGNING_KEY）
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: release-sign <keygen|sign> [file]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		keygen()
	case "sign":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: release-sign sign <file>")
			os.Exit(2)
		}
		sign(os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func keygen() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail(err)
	}
	fmt.Println("# 公钥：粘贴进 internal/app/selfupdate.go 的 releaseSigningPublicKey")
	fmt.Printf("PUBLIC  %s\n", base64.StdEncoding.EncodeToString(pub))
	fmt.Println("# 私钥(seed)：存为 GitHub 仓库 Secret RELEASE_SIGNING_KEY，切勿提交进仓库")
	fmt.Printf("PRIVATE %s\n", base64.StdEncoding.EncodeToString(priv.Seed()))
}

func sign(path string) {
	seedB64 := os.Getenv("RELEASE_SIGNING_KEY")
	if seedB64 == "" {
		fail(fmt.Errorf("RELEASE_SIGNING_KEY is empty"))
	}
	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil {
		fail(fmt.Errorf("decode RELEASE_SIGNING_KEY: %w", err))
	}
	if len(seed) != ed25519.SeedSize {
		fail(fmt.Errorf("RELEASE_SIGNING_KEY must decode to %d bytes, got %d", ed25519.SeedSize, len(seed)))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	out := path + ".sig"
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s\n", out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "release-sign:", err)
	os.Exit(1)
}
