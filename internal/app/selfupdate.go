package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// updateRepo 是 frpc-web 自身的发布仓库。
const updateRepo = "sccens/frpc-web"

// maxUpdateBinarySize 限制自更新下载的二进制大小，防止异常响应耗尽内存。
const maxUpdateBinarySize int64 = 64 << 20

type UpdateCheck struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"hasUpdate"`
	NotesURL  string `json:"notesUrl"`
	CanApply  bool   `json:"canApply"`
	ApplyHint string `json:"applyHint,omitempty"`
}

var updateHTTPClient = &http.Client{Timeout: 120 * time.Second}

// CheckUpdate 查询 GitHub 最新发布并与当前运行版本比较。
func (s *Service) CheckUpdate(ctx context.Context) (UpdateCheck, error) {
	latest, err := s.latestAppVersion(ctx)
	if err != nil {
		return UpdateCheck{}, err
	}
	result := UpdateCheck{
		Current:   s.version,
		Latest:    latest,
		HasUpdate: versionLess(s.version, latest),
		NotesURL:  fmt.Sprintf("https://github.com/%s/releases/tag/%s", updateRepo, latest),
	}
	exe, err := os.Executable()
	if err != nil {
		result.ApplyHint = "无法定位当前二进制：" + err.Error()
		return result, nil
	}
	if writableForUpdate(exe) {
		result.CanApply = true
	} else {
		result.ApplyHint = "当前进程无权限替换自身二进制，请在服务器上重跑安装命令完成升级：curl -fsSL https://raw.githubusercontent.com/" + updateRepo + "/main/install.sh | bash"
	}
	return result, nil
}

// ApplyUpdate 下载最新版本、校验 SHA256 后原子替换自身二进制，并通过
// syscall.Exec 原地重启（PID 不变，systemd 无感知；运行中的 frpc 子进程
// 不会中断，重启后由 Restore/Adopt 重新接管）。
func (s *Service) ApplyUpdate(ctx context.Context) (ActionResult, error) {
	check, err := s.CheckUpdate(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	if !check.HasUpdate {
		return ActionResult{OK: false, Message: "当前已是最新版本 " + check.Current}, nil
	}
	if !check.CanApply {
		return ActionResult{OK: false, Message: check.ApplyHint}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return ActionResult{}, err
	}

	asset := fmt.Sprintf("frpc-web_%s_%s", runtime.GOOS, runtime.GOARCH)
	base := fmt.Sprintf("https://github.com/%s/releases/download/%s", updateRepo, check.Latest)
	binary, err := s.fetchUpdateAsset(ctx, base+"/"+asset, maxUpdateBinarySize)
	if err != nil {
		return ActionResult{}, fmt.Errorf("下载新版本失败: %w", err)
	}
	sums, err := s.fetchUpdateAsset(ctx, base+"/SHA256SUMS", 1<<20)
	if err != nil {
		return ActionResult{}, fmt.Errorf("下载校验文件失败: %w", err)
	}
	expected := checksumFromSums(string(sums), asset)
	if expected == "" {
		return ActionResult{}, fmt.Errorf("SHA256SUMS 中没有 %s 的校验记录", asset)
	}
	digest := sha256.Sum256(binary)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), expected) {
		return ActionResult{}, errors.New("SHA256 校验失败：下载内容可能损坏或被篡改")
	}

	// 写入同目录临时文件后原子替换，保证任意时刻磁盘上都有完整可执行文件。
	tmp := exe + ".update"
	if err := os.WriteFile(tmp, binary, 0o755); err != nil {
		return ActionResult{}, fmt.Errorf("写入新版本失败: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return ActionResult{}, err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return ActionResult{}, fmt.Errorf("替换二进制失败: %w", err)
	}

	// 延迟重启，先让本次 HTTP 响应送达客户端。
	go func() {
		time.Sleep(800 * time.Millisecond)
		// Exec 仅在失败时返回；失败则退出交给 systemd 重新拉起（新二进制已就位）。
		_ = syscall.Exec(exe, os.Args, os.Environ())
		os.Exit(1)
	}()

	return ActionResult{OK: true, Message: "已更新到 " + check.Latest + "，服务正在重启，页面稍后自动恢复"}, nil
}

func (s *Service) latestAppVersion(ctx context.Context) (string, error) {
	url := s.githubProxied(ctx, fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("查询最新版本失败: %s", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	tag := strings.TrimSpace(payload.TagName)
	if tag == "" {
		return "", errors.New("发布信息中缺少版本号")
	}
	return tag, nil
}

func (s *Service) fetchUpdateAsset(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.githubProxied(ctx, rawURL), nil)
	if err != nil {
		return nil, err
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s: %s", filepath.Base(rawURL), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s 超过大小上限", filepath.Base(rawURL))
	}
	return data, nil
}

// githubProxied 给 GitHub URL 套上用户配置的下载代理（设置项优先，环境变量兜底）。
func (s *Service) githubProxied(ctx context.Context, raw string) string {
	proxy, _ := s.store.GetSetting(ctx, "github_proxy")
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		proxy = strings.TrimSpace(os.Getenv("FRPC_WEB_GITHUB_PROXY"))
	}
	if proxy == "" {
		return raw
	}
	return strings.TrimRight(proxy, "/") + "/" + raw
}

// checksumFromSums 从 sha256sum 格式的清单中取出指定文件的哈希。
func checksumFromSums(sums string, filename string) string {
	for _, line := range strings.Split(sums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filename {
			return fields[0]
		}
	}
	return ""
}

// versionLess 比较形如 v1.2.3 的版本号；任一侧无法解析时返回 false（如 dev 构建）。
func versionLess(current, latest string) bool {
	a, okA := parseVersion(current)
	b, okB := parseVersion(latest)
	if !okA || !okB {
		return false
	}
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func parseVersion(value string) ([3]int, bool) {
	var out [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" {
		return out, false
	}
	parts := strings.SplitN(value, ".", 3)
	for i, part := range parts {
		if i >= 3 {
			break
		}
		// 容忍 1.0.0-rc1 之类的后缀：取数字前缀
		digits := part
		if idx := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); idx >= 0 {
			digits = part[:idx]
		}
		if digits == "" {
			return out, false
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// writableForUpdate 判断当前进程能否替换该二进制（文件与所在目录均可写）。
func writableForUpdate(path string) bool {
	if syscall.Access(path, 0x2 /* W_OK */) != nil {
		return false
	}
	return syscall.Access(filepath.Dir(path), 0x2) == nil
}
