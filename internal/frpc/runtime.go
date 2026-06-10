package frpc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sccens/frpc-web/internal/app"
)

type Runtime struct {
	dataDir     string
	githubProxy string
	mu          sync.Mutex
	cmds        map[string]*exec.Cmd
	dones       map[string]chan struct{}
	stopping    map[string]bool
	onExit      func(serverID string, err error)
}

// stopGraceTimeout 是 SIGTERM 后等待 frpc 退出的时长，超时升级为 SIGKILL。
const stopGraceTimeout = 5 * time.Second

// maxReleaseArchiveSize 是在线安装时允许下载的发布包大小上限。
const maxReleaseArchiveSize int64 = 128 << 20

func New(dataDir string) *Runtime {
	if dataDir == "" {
		dataDir = "frpc-web-data"
	}
	return &Runtime{
		dataDir:     dataDir,
		githubProxy: os.Getenv("FRPC_WEB_GITHUB_PROXY"),
		cmds:        map[string]*exec.Cmd{},
		dones:       map[string]chan struct{}{},
		stopping:    map[string]bool{},
	}
}

func (r *Runtime) SetExitHandler(handler func(serverID string, err error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onExit = handler
}

func (r *Runtime) RenderConfig(_ context.Context, server app.Server) (app.ConfigPreview, error) {
	configDir := filepath.Join(r.serverDir(server.ID), "configs")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return app.ConfigPreview{}, err
	}

	content := renderTOML(server)
	configPath := filepath.Join(configDir, "frpc.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		return app.ConfigPreview{}, err
	}
	return app.ConfigPreview{ConfigPath: configPath, Content: content}, nil
}

func (r *Runtime) CheckConfig(ctx context.Context, server app.Server, version app.FRPCVersion) app.ActionResult {
	if version.Path == "" {
		return app.ActionResult{OK: false, Message: "frpc is not installed"}
	}
	preview, err := r.RenderConfig(ctx, server)
	if err != nil {
		return app.ActionResult{OK: false, Message: err.Error()}
	}
	out, err := exec.CommandContext(ctx, version.Path, "verify", "-c", preview.ConfigPath).CombinedOutput()
	if err != nil {
		return app.ActionResult{OK: false, Message: "frpc verify failed", Output: string(out)}
	}
	return app.ActionResult{OK: true, Message: "configuration verified", Output: string(out)}
}

func (r *Runtime) Start(ctx context.Context, server app.Server, version app.FRPCVersion) (app.ProcessInfo, app.ActionResult) {
	preview, err := r.RenderConfig(ctx, server)
	if err != nil {
		return app.ProcessInfo{}, app.ActionResult{OK: false, Message: err.Error()}
	}
	if version.Path == "" {
		return app.ProcessInfo{}, app.ActionResult{OK: false, Message: "frpc is not installed"}
	}

	logPath := r.logPath(server.ID)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return app.ProcessInfo{}, app.ActionResult{OK: false, Message: err.Error()}
	}
	logFile, err := newRotatingLogWriter(logPath, 10*1024*1024, 3)
	if err != nil {
		return app.ProcessInfo{}, app.ActionResult{OK: false, Message: err.Error()}
	}

	cmd := exec.Command(version.Path, "-c", preview.ConfigPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return app.ProcessInfo{}, app.ActionResult{OK: false, Message: err.Error()}
	}

	r.mu.Lock()
	r.cmds[server.ID] = cmd
	done := make(chan struct{})
	r.dones[server.ID] = done
	delete(r.stopping, server.ID)
	r.mu.Unlock()

	startedAt := time.Now().Format(time.RFC3339)
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		close(done)
		r.mu.Lock()
		// 仅当退出的进程仍是当前追踪的一代时才清理并通知；
		// 否则这是重启后旧进程的延迟退出，不能动新进程的状态。
		current := r.cmds[server.ID] == cmd
		stopping := r.stopping[server.ID]
		if current {
			delete(r.cmds, server.ID)
			delete(r.dones, server.ID)
			delete(r.stopping, server.ID)
		}
		handler := r.onExit
		r.mu.Unlock()
		if current && !stopping && handler != nil {
			handler(server.ID, err)
		}
	}()

	return app.ProcessInfo{
		ServerID:    server.ID,
		PID:         cmd.Process.Pid,
		FRPCVersion: version.Version,
		ConfigPath:  preview.ConfigPath,
		LogPath:     logPath,
		StartedAt:   startedAt,
	}, app.ActionResult{OK: true, Message: "frpc started"}
}

func (r *Runtime) Stop(_ context.Context, server app.Server, process app.ProcessInfo) app.ActionResult {
	r.mu.Lock()
	r.stopping[server.ID] = true
	cmd := r.cmds[server.ID]
	done := r.dones[server.ID]
	r.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return app.ActionResult{OK: false, Message: err.Error()}
		}
		// 等待进程真正退出再返回，避免重启时新旧进程并存（admin 端口冲突）。
		if done != nil {
			select {
			case <-done:
			case <-time.After(stopGraceTimeout):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				select {
				case <-done:
				case <-time.After(2 * time.Second):
				}
			}
		}
		return app.ActionResult{OK: true, Message: "frpc stopped"}
	}

	if process.PID > 0 {
		if err := syscall.Kill(-process.PID, syscall.SIGTERM); err != nil {
			if err := syscall.Kill(process.PID, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return app.ActionResult{OK: false, Message: err.Error()}
			}
		}
		deadline := time.Now().Add(stopGraceTimeout)
		for time.Now().Before(deadline) {
			if !r.ProcessAlive(context.Background(), process.PID) {
				return app.ActionResult{OK: true, Message: "frpc stopped"}
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = syscall.Kill(-process.PID, syscall.SIGKILL)
	}
	return app.ActionResult{OK: true, Message: "frpc stopped"}
}

func (r *Runtime) Reload(ctx context.Context, server app.Server, version app.FRPCVersion) app.ActionResult {
	if version.Path == "" {
		return app.ActionResult{OK: false, Message: "frpc is not installed"}
	}
	preview, err := r.RenderConfig(ctx, server)
	if err != nil {
		return app.ActionResult{OK: false, Message: err.Error()}
	}
	out, err := exec.CommandContext(ctx, version.Path, "reload", "-c", preview.ConfigPath).CombinedOutput()
	if err != nil {
		return app.ActionResult{OK: false, Message: "frpc reload failed", Output: string(out)}
	}
	return app.ActionResult{OK: true, Message: "frpc reloaded", Output: string(out)}
}

func (r *Runtime) Logs(_ context.Context, serverID string, tail int) ([]app.LogLine, error) {
	lines, err := tailLines(r.logPath(serverID), tail)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []app.LogLine{}, nil
		}
		return nil, err
	}
	result := make([]app.LogLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, parseLogLine(line))
	}
	return result, nil
}

func (r *Runtime) ProcessAlive(_ context.Context, pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	return true
}

func (r *Runtime) InstallOnline(ctx context.Context, input app.FRPCInstallOnlineInput) (app.FRPCVersion, error) {
	if input.Platform == "" {
		input.Platform = runtime.GOOS
	}
	if input.Arch == "" {
		input.Arch = runtime.GOARCH
	}
	if input.Version == "" || input.Version == "latest" {
		latest, err := r.latestVersion(ctx, input.GithubProxy)
		if err != nil {
			return app.FRPCVersion{}, err
		}
		input.Version = latest
	}
	tag := input.Version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	assetVersion := strings.TrimPrefix(tag, "v")
	assetName := fmt.Sprintf("frp_%s_%s_%s.tar.gz", assetVersion, input.Platform, input.Arch)
	url := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/%s/%s", tag, assetName)
	checksumURL := fmt.Sprintf("https://github.com/fatedier/frp/releases/download/%s/frp_sha256_checksums.txt", tag)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.githubURL(url, input.GithubProxy), nil)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return app.FRPCVersion{}, fmt.Errorf("download failed: %s", resp.Status)
	}
	// frp 发布包约 10-20MB；限制读取上限，防止恶意代理把内存吃满。
	archive, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseArchiveSize+1))
	if err != nil {
		return app.FRPCVersion{}, err
	}
	if int64(len(archive)) > maxReleaseArchiveSize {
		return app.FRPCVersion{}, fmt.Errorf("release archive exceeds %d MB limit", maxReleaseArchiveSize/(1<<20))
	}
	if err := r.verifyReleaseChecksum(ctx, checksumURL, input.GithubProxy, assetName, archive); err != nil {
		return app.FRPCVersion{}, err
	}
	return r.installTarGZ(bytes.NewReader(archive), assetVersion, input.Platform, input.Arch, "online")
}

func (r *Runtime) InstallOffline(_ context.Context, filename string, file io.Reader) (app.FRPCVersion, error) {
	name := strings.ToLower(filename)
	if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tgz") {
		return r.installTarGZ(file, "offline-"+strconv.FormatInt(time.Now().Unix(), 10), runtime.GOOS, runtime.GOARCH, "offline")
	}

	tmpDir := filepath.Join(r.dataDir, "uploads")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return app.FRPCVersion{}, err
	}
	tmpPath := filepath.Join(tmpDir, "frpc-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	// 成功 Rename 后临时文件已不存在，这里的 Remove 只负责清理失败路径。
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		return app.FRPCVersion{}, err
	}
	_ = out.Close()
	version, err := binaryVersion(tmpPath)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	targetDir := filepath.Join(r.dataDir, "bin", "frpc", version)
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return app.FRPCVersion{}, err
	}
	target := filepath.Join(targetDir, "frpc")
	if err := os.Rename(tmpPath, target); err != nil {
		return app.FRPCVersion{}, err
	}
	_ = os.Chmod(target, 0o700)
	return app.FRPCVersion{Version: version, Platform: runtime.GOOS, Arch: runtime.GOARCH, Path: target, Source: "offline", Installed: true}, nil
}

func (r *Runtime) LatestVersion(ctx context.Context, githubProxy string) (string, error) {
	return r.latestVersion(ctx, githubProxy)
}

func (r *Runtime) AdminStatus(ctx context.Context, server app.Server) (app.AdminStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminBaseURL(server)+"/api/status", nil)
	if err != nil {
		return app.AdminStatus{}, err
	}
	setAdminAuth(req, server)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return app.AdminStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return app.AdminStatus{}, fmt.Errorf("GET /api/status failed: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload any
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return app.AdminStatus{}, err
	}
	return app.AdminStatus{Proxies: adminStatusProxies(payload)}, nil
}

func (r *Runtime) installTarGZ(reader io.Reader, version string, platform string, arch string, source string) (app.FRPCVersion, error) {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	defer gz.Close()

	uploadsDir := filepath.Join(r.dataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0o700); err != nil {
		return app.FRPCVersion{}, err
	}
	tmpDir, err := os.MkdirTemp(uploadsDir, "frpc-extract-")
	if err != nil {
		return app.FRPCVersion{}, err
	}
	defer os.RemoveAll(tmpDir)
	tmpTarget := filepath.Join(tmpDir, "frpc")

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return app.FRPCVersion{}, err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "frpc" {
			continue
		}
		out, err := os.OpenFile(tmpTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
		if err != nil {
			return app.FRPCVersion{}, err
		}
		if _, err := io.Copy(out, tr); err != nil {
			_ = out.Close()
			return app.FRPCVersion{}, err
		}
		_ = out.Close()
		_ = os.Chmod(tmpTarget, 0o700)
		installedVersion, err := binaryVersion(tmpTarget)
		if err == nil && installedVersion != "" {
			version = installedVersion
		}
		targetDir := filepath.Join(r.dataDir, "bin", "frpc", version)
		if err := os.MkdirAll(targetDir, 0o700); err != nil {
			return app.FRPCVersion{}, err
		}
		target := filepath.Join(targetDir, "frpc")
		if err := os.Rename(tmpTarget, target); err != nil {
			return app.FRPCVersion{}, err
		}
		_ = os.Chmod(target, 0o700)
		return app.FRPCVersion{Version: version, Platform: platform, Arch: arch, Path: target, Source: source, Installed: true}, nil
	}
	return app.FRPCVersion{}, errors.New("frpc binary not found in archive")
}

func (r *Runtime) serverDir(serverID string) string {
	return filepath.Join(r.dataDir, "servers", serverID)
}

func (r *Runtime) logPath(serverID string) string {
	return filepath.Join(r.dataDir, "logs", serverID, "frpc.log")
}

func renderTOML(server app.Server) string {
	var b strings.Builder
	fmt.Fprintf(&b, "serverAddr = %q\n", server.ServerAddr)
	fmt.Fprintf(&b, "serverPort = %d\n", server.ServerPort)
	if server.TransportProtocol != "" && server.TransportProtocol != "tcp" {
		fmt.Fprintf(&b, "transport.protocol = %q\n", server.TransportProtocol)
	}
	if server.AuthToken != "" && !app.LooksMaskedSecret(server.AuthToken) {
		fmt.Fprintf(&b, "auth.method = %q\n", "token")
		fmt.Fprintf(&b, "auth.token = %q\n", server.AuthToken)
	}
	fmt.Fprintf(&b, "webServer.addr = %q\n", "127.0.0.1")
	fmt.Fprintf(&b, "webServer.port = %d\n", server.AdminPort)
	if server.AdminUser != "" {
		fmt.Fprintf(&b, "webServer.user = %q\n", server.AdminUser)
	}
	if server.AdminPassword != "" && !app.LooksMaskedSecret(server.AdminPassword) {
		fmt.Fprintf(&b, "webServer.password = %q\n", server.AdminPassword)
	}
	for _, rule := range server.Rules {
		if !rule.Enabled {
			continue
		}
		writeRuleTOML(&b, rule)
	}
	return b.String()
}

func writeRuleTOML(b *strings.Builder, rule app.ProxyRule) {
	if (rule.Type == "stcp" || rule.Type == "xtcp") && rule.Role == "visitor" {
		fmt.Fprintf(b, "\n[[visitors]]\n")
		fmt.Fprintf(b, "name = %q\n", rule.Name)
		fmt.Fprintf(b, "type = %q\n", rule.Type)
		fmt.Fprintf(b, "serverName = %q\n", rule.ServerName)
		fmt.Fprintf(b, "secretKey = %q\n", rule.SecretKey)
		fmt.Fprintf(b, "bindAddr = %q\n", defaultString(rule.BindAddr, "127.0.0.1"))
		fmt.Fprintf(b, "bindPort = %d\n", rule.BindPort)
		return
	}

	fmt.Fprintf(b, "\n[[proxies]]\n")
	fmt.Fprintf(b, "name = %q\n", rule.Name)
	fmt.Fprintf(b, "type = %q\n", rule.Type)
	if rule.Type == "stcp" || rule.Type == "xtcp" {
		fmt.Fprintf(b, "secretKey = %q\n", rule.SecretKey)
	}
	fmt.Fprintf(b, "localIP = %q\n", defaultString(rule.LocalIP, "127.0.0.1"))
	fmt.Fprintf(b, "localPort = %d\n", rule.LocalPort)
	if rule.Type == "tcp" || rule.Type == "udp" {
		fmt.Fprintf(b, "remotePort = %d\n", rule.RemotePort)
	}
	if rule.Type == "http" || rule.Type == "https" {
		fmt.Fprintf(b, "customDomains = [%s]\n", quoteList(rule.CustomDomains))
		if len(rule.Locations) > 0 {
			fmt.Fprintf(b, "locations = [%s]\n", quoteList(rule.Locations))
		}
		if rule.HostHeaderRewrite != "" {
			fmt.Fprintf(b, "hostHeaderRewrite = %q\n", rule.HostHeaderRewrite)
		}
		if rule.HTTPUser != "" {
			fmt.Fprintf(b, "httpUser = %q\n", rule.HTTPUser)
		}
		if rule.HTTPPassword != "" && !app.LooksMaskedSecret(rule.HTTPPassword) {
			fmt.Fprintf(b, "httpPassword = %q\n", rule.HTTPPassword)
		}
		for _, header := range headerPairs(rule.RequestHeaders) {
			fmt.Fprintf(b, "requestHeaders.set.%s = %q\n", header.key, header.value)
		}
	}
	if rule.UseEncryption {
		fmt.Fprintf(b, "transport.useEncryption = true\n")
	}
	if rule.UseCompression {
		fmt.Fprintf(b, "transport.useCompression = true\n")
	}
	if rule.BandwidthLimit != "" {
		fmt.Fprintf(b, "transport.bandwidthLimit = %q\n", rule.BandwidthLimit)
	}
}

func quoteList(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.Quote(value))
	}
	return strings.Join(out, ", ")
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

type headerPair struct {
	key   string
	value string
}

// headerPairs parses "Key: Value" entries into a deterministically ordered
// slice. Later duplicates win, invalid header names are skipped, and the
// result is sorted by key so identical inputs always render identical TOML
// (avoiding spurious config checksum churn).
func headerPairs(values []string) []headerPair {
	seen := map[string]string{}
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key, value, ok := strings.Cut(item, ":")
		if !ok {
			key, value, ok = strings.Cut(item, "=")
		}
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !app.IsValidHeaderName(key) {
			continue
		}
		seen[key] = value
	}
	out := make([]headerPair, 0, len(seen))
	for key, value := range seen {
		out = append(out, headerPair{key: key, value: value})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

func adminBaseURL(server app.Server) string {
	port := server.AdminPort
	if port == 0 {
		port = 7400
	}
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

func setAdminAuth(req *http.Request, server app.Server) {
	if server.AdminUser != "" || server.AdminPassword != "" {
		req.SetBasicAuth(server.AdminUser, server.AdminPassword)
	}
}

func adminStatusProxies(payload any) []app.AdminProxyStatus {
	proxies := make([]app.AdminProxyStatus, 0)
	collectAdminProxies(payload, "", &proxies)
	return proxies
}

func collectAdminProxies(value any, typeHint string, out *[]app.AdminProxyStatus) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectAdminProxies(item, typeHint, out)
		}
	case map[string]any:
		if looksLikeAdminProxy(typed) {
			*out = append(*out, adminProxyFromMap(typed, typeHint))
			return
		}
		for key, child := range typed {
			nextHint := typeHint
			lower := strings.ToLower(key)
			switch lower {
			case "tcp", "udp", "http", "https", "stcp", "xtcp", "sudp":
				nextHint = lower
			}
			if nextHint != typeHint || lower == "proxies" || lower == "status" || lower == "proxy_status" || lower == "proxystatus" {
				collectAdminProxies(child, nextHint, out)
			}
		}
	}
}

func looksLikeAdminProxy(values map[string]any) bool {
	if stringFromMap(values, "name", "proxyName") == "" {
		return false
	}
	return stringFromMap(values, "status", "phase", "type", "proxyType", "localAddr", "remoteAddr", "error", "err") != ""
}

func adminProxyFromMap(values map[string]any, typeHint string) app.AdminProxyStatus {
	proxy := app.AdminProxyStatus{
		Name:       stringFromMap(values, "name", "proxyName"),
		Type:       stringFromMap(values, "type", "proxyType"),
		Status:     stringFromMap(values, "status", "phase"),
		LocalAddr:  stringFromMap(values, "localAddr", "local_addr", "localAddress", "local"),
		RemoteAddr: stringFromMap(values, "remoteAddr", "remote_addr", "remoteAddress", "remote"),
		Error:      stringFromMap(values, "error", "err", "lastErr", "lastError"),
	}
	if proxy.Type == "" {
		proxy.Type = typeHint
	}
	if proxy.LocalAddr == "" {
		proxy.LocalAddr = proxyLocalAddr(values)
	}
	if proxy.RemoteAddr == "" {
		proxy.RemoteAddr = proxyRemoteAddr(values)
	}
	if in, ok := int64FromMap(values, "trafficIn", "totalTrafficIn", "todayTrafficIn"); ok {
		proxy.TrafficIn = in
		proxy.TrafficAvailable = true
	}
	if out, ok := int64FromMap(values, "trafficOut", "totalTrafficOut", "todayTrafficOut"); ok {
		proxy.TrafficOut = out
		proxy.TrafficAvailable = true
	}
	return proxy
}

func proxyLocalAddr(values map[string]any) string {
	conf, _ := values["conf"].(map[string]any)
	if conf == nil {
		conf = values
	}
	ip := stringFromMap(conf, "localIP", "localIp", "local_ip")
	port, ok := int64FromMap(conf, "localPort", "local_port")
	if ip == "" || !ok || port == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func proxyRemoteAddr(values map[string]any) string {
	conf, _ := values["conf"].(map[string]any)
	if conf == nil {
		conf = values
	}
	port, ok := int64FromMap(conf, "remotePort", "remote_port")
	if !ok || port == 0 {
		return ""
	}
	return ":" + strconv.FormatInt(port, 10)
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		}
	}
	return ""
}

func int64FromMap(values map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			n, err := typed.Int64()
			if err == nil {
				return n, true
			}
			f, err := typed.Float64()
			if err == nil {
				return int64(f), true
			}
		case float64:
			return int64(typed), true
		case int:
			return int64(typed), true
		case int64:
			return typed, true
		case string:
			n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			if err == nil {
				return n, true
			}
		}
	}
	return 0, false
}

func binaryVersion(path string) (string, error) {
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, string(out))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "unknown", nil
	}
	return strings.TrimPrefix(fields[len(fields)-1], "v"), nil
}

func (r *Runtime) latestVersion(ctx context.Context, githubProxy string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.githubURL("https://api.github.com/repos/fatedier/frp/releases/latest", githubProxy), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github latest release failed: %s", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", errors.New("latest release tag not found")
	}
	return strings.TrimPrefix(payload.TagName, "v"), nil
}

func (r *Runtime) verifyReleaseChecksum(ctx context.Context, checksumURL string, githubProxy string, assetName string, archive []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.githubURL(checksumURL, githubProxy), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download checksum failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	want := checksumForAsset(string(data), assetName)
	if want == "" {
		return fmt.Errorf("checksum for %s not found", assetName)
	}
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func checksumForAsset(content string, assetName string) string {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) == assetName {
			return fields[0]
		}
	}
	return ""
}

func (r *Runtime) githubURL(raw string, githubProxy string) string {
	proxy := strings.TrimSpace(githubProxy)
	if proxy == "" {
		proxy = strings.TrimSpace(r.githubProxy)
	}
	if proxy == "" {
		return raw
	}
	return strings.TrimRight(proxy, "/") + "/" + raw
}

type rotatingLogWriter struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxBackups int
	file       *os.File
	size       int64
}

func newRotatingLogWriter(path string, maxSize int64, maxBackups int) (*rotatingLogWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &rotatingLogWriter{path: path, maxSize: maxSize, maxBackups: maxBackups, file: file, size: info.Size()}, nil
}

func (w *rotatingLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.maxSize > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *rotatingLogWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
	}
	if w.maxBackups > 0 {
		_ = os.Remove(fmt.Sprintf("%s.%d", w.path, w.maxBackups))
		for i := w.maxBackups - 1; i >= 1; i-- {
			oldPath := fmt.Sprintf("%s.%d", w.path, i)
			newPath := fmt.Sprintf("%s.%d", w.path, i+1)
			if _, err := os.Stat(oldPath); err == nil {
				_ = os.Rename(oldPath, newPath)
			}
		}
		if _, err := os.Stat(w.path); err == nil {
			_ = os.Rename(w.path, w.path+".1")
		}
	} else {
		_ = os.Remove(w.path)
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w.file = file
	w.size = 0
	return nil
}

func tailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		n = 200
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return []string{}, nil
	}

	const blockSize int64 = 8192
	var (
		pos       = info.Size()
		remainder string
		lines     []string
	)
	for pos > 0 && len(lines) <= n {
		readSize := blockSize
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		buf := make([]byte, readSize)
		if _, err := file.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		chunk := string(buf) + remainder
		parts := strings.Split(chunk, "\n")
		remainder = parts[0]
		for i := len(parts) - 1; i >= 1; i-- {
			if parts[i] == "" && pos+readSize == info.Size() && len(lines) == 0 {
				continue
			}
			lines = append(lines, parts[i])
			if len(lines) >= n {
				break
			}
		}
	}
	if len(lines) < n && remainder != "" {
		lines = append(lines, remainder)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// parseLogLine 从 frpc 日志行中提取真实时间戳（格式 2006/01/02 15:04:05，
// 可带毫秒）。解析不出时间时 Time 留空，避免用请求时刻冒充日志时间。
func parseLogLine(line string) app.LogLine {
	entry := app.LogLine{Level: "info", Message: line}
	if len(line) >= 19 {
		if ts, err := time.Parse("2006/01/02 15:04:05", line[:19]); err == nil {
			entry.Time = ts.Format("15:04:05")
			rest := line[19:]
			if len(rest) > 1 && rest[0] == '.' {
				i := 1
				for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
					i++
				}
				rest = rest[i:]
			}
			entry.Message = strings.TrimSpace(rest)
		}
	}
	lower := strings.ToLower(entry.Message)
	switch {
	case strings.Contains(entry.Message, "[E]") || strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
		entry.Level = "error"
	case strings.Contains(entry.Message, "[W]") || strings.Contains(lower, "warn"):
		entry.Level = "warn"
	}
	return entry
}
