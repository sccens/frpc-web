package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 本文件实现 v2.0 的「配置文件监控」：扫描一组路径下的 frpc 配置文件，解析为
// 只读的 Server 列表，并在后台周期性探测各实例 admin API 的实时状态（running /
// stopped / no-admin），结果缓存供 Servers() 读取——避免每次列表请求都同步探活。
//
// 一文件 = 一 server；ID 是配置文件绝对路径的稳定哈希（跨重扫一致）。

const (
	configScanInterval  = 10 * time.Second
	configStatusTimeout = 3 * time.Second
)

// statusProbe 探测单个 server 的 admin API 实时状态（由 frpc.Runtime 实现）。
type statusProbe interface {
	ProxyStatus(ctx context.Context, server Server) ([]ProxyStatus, error)
}

// ConfigScanner 周期扫描配置文件并缓存 server 列表与实时状态。
type ConfigScanner struct {
	probe   statusProbe
	dataDir string
	paths   []string

	mu      sync.RWMutex
	servers []Server
}

// NewConfigScanner 构造扫描器。probe 为 nil 时跳过 admin API 探活（仅展示配置）。
func NewConfigScanner(dataDir string, probe statusProbe) *ConfigScanner {
	return &ConfigScanner{
		probe:   probe,
		dataDir: dataDir,
		paths:   resolveConfigPaths(dataDir),
	}
}

// Run 阻塞运行扫描与探活循环，直到 ctx 取消。启动时立即扫一次。
func (c *ConfigScanner) Run(ctx context.Context) {
	c.refresh(ctx)
	ticker := time.NewTicker(configScanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(ctx)
		}
	}
}

// RefreshNow 立即触发一次扫描+探活（供保存配置后调用，让结果尽快反映磁盘变更）。
func (c *ConfigScanner) RefreshNow(ctx context.Context) {
	c.refresh(ctx)
}

func (c *ConfigScanner) refresh(ctx context.Context) {
	c.scan()
	c.probeStatuses(ctx)
}

// Servers 返回当前缓存的 server 列表（含实时状态）的拷贝。
func (c *ConfigScanner) Servers() []Server {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Server, len(c.servers))
	copy(out, c.servers)
	return out
}

// Server 返回指定 ID 的 server。
func (c *ConfigScanner) Server(id string) (Server, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, s := range c.servers {
		if s.ID == id {
			return s, true
		}
	}
	return Server{}, false
}

// Paths 返回当前扫描路径（用于导入配置时校验目标路径是否在受控范围内）。
func (c *ConfigScanner) Paths() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, len(c.paths))
	copy(out, c.paths)
	return out
}

func (c *ConfigScanner) snapshotStatus() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m := make(map[string]string, len(c.servers))
	for _, s := range c.servers {
		m[s.ID] = s.Status
	}
	return m
}

// scan 重扫磁盘、解析配置文件为 server 列表，保留上一轮已探得的状态。
func (c *ConfigScanner) scan() {
	files := discoverConfigFiles(c.paths)
	prevStatus := c.snapshotStatus()

	servers := make([]Server, 0, len(files))
	for _, path := range files {
		server, err := parseServerFromPath(path)
		if err != nil {
			// 单个文件解析失败不影响其余；坏文件直接跳过。
			continue
		}
		if status, ok := prevStatus[server.ID]; ok {
			server.Status = status
		}
		servers = append(servers, server)
	}

	c.mu.Lock()
	c.servers = servers
	c.mu.Unlock()
}

// probeStatuses 并发探测各 server 的 admin API，把结果写回缓存。
func (c *ConfigScanner) probeStatuses(ctx context.Context) {
	c.mu.RLock()
	servers := make([]Server, len(c.servers))
	copy(servers, c.servers)
	c.mu.RUnlock()

	if c.probe == nil {
		return
	}

	results := make(map[string]string, len(servers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, server := range servers {
		if server.AdminPort == 0 {
			results[server.ID] = "no-admin"
			continue
		}
		wg.Add(1)
		go func(server Server) {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, configStatusTimeout)
			defer cancel()
			status := "stopped"
			if _, err := c.probe.ProxyStatus(callCtx, server); err == nil {
				status = "running"
			}
			mu.Lock()
			results[server.ID] = status
			mu.Unlock()
		}(server)
	}
	wg.Wait()

	c.mu.Lock()
	for i := range c.servers {
		if status, ok := results[c.servers[i].ID]; ok {
			c.servers[i].Status = status
		}
	}
	c.mu.Unlock()
}

// resolveConfigPaths 确定要扫描的路径：设置了 FRPC_WEB_CONFIG_PATH 时只用它
// （PATH 分隔符分隔多个，显式替代默认）；否则扫描常见目录与数据目录。
func resolveConfigPaths(dataDir string) []string {
	if env := strings.TrimSpace(os.Getenv("FRPC_WEB_CONFIG_PATH")); env != "" {
		var paths []string
		for _, p := range filepath.SplitList(env) {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
		return paths
	}
	return []string{"/etc/frpc", "/usr/local/etc/frpc", dataDir}
}

// discoverConfigFiles 在给定路径（文件或目录）下收集去重后的配置文件绝对路径。
func discoverConfigFiles(paths []string) []string {
	seen := map[string]bool{}
	var files []string
	add := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return
		}
		if !seen[abs] {
			seen[abs] = true
			files = append(files, abs)
		}
	}
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if isConfigFileName(p) {
				add(p)
			}
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !isConfigFileName(e.Name()) {
				continue
			}
			add(filepath.Join(p, e.Name()))
		}
	}
	return files
}

func isConfigFileName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".toml", ".ini":
		return true
	default:
		return false
	}
}

// parseServerFromPath 读取并解析单个配置文件为 Server（不含已探得的实时状态）。
func parseServerFromPath(path string) (Server, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Server{}, err
	}
	server, rules, err := ParseFrpcConfig(string(content))
	if err != nil {
		return Server{}, err
	}
	id := configID(path)
	for i := range rules {
		rules[i].ID = fmt.Sprintf("%s-%d", id, i)
		rules[i].ServerID = id
	}
	server.ID = id
	server.ConfigPath = path
	server.Name = serverNameFor(path, server)
	server.Rules = rules
	server.ProxyCount = len(rules)
	server.Writable = isFileWritable(path)
	if server.AdminPort == 0 {
		server.Status = "no-admin"
	} else {
		server.Status = "stopped"
	}
	return server, nil
}

// configID 以配置文件绝对路径的哈希作为稳定 ID（跨重扫一致）。
func configID(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha256.Sum256([]byte(filepath.ToSlash(abs)))
	return hex.EncodeToString(sum[:])[:12]
}

func serverNameFor(path string, server Server) string {
	if strings.TrimSpace(server.ServerAddr) != "" {
		return fmt.Sprintf("%s:%d", server.ServerAddr, server.ServerPort)
	}
	return filepath.Base(path)
}

// isFileWritable 以“能用 O_WRONLY 打开”探测文件可写性（不截断内容）。
func isFileWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
