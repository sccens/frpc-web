// Package storage 以单个 JSON 状态文件持久化全部数据。
// 个人单机场景下数据量极小（个位数服务器、几十条规则），
// 内存持有 + 原子写回比嵌入式数据库更简单：文件人类可读、
// 天然可备份、无迁移负担。
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sccens/frpc-web/internal/app"
)

const (
	stateFileName   = "state.json"
	maxAuditEntries = 500
	maxHealthEvents = 200
	healthRetention = 30 * 24 * time.Hour
)

// storedSession 是 app.Session 的持久化形态：只存凭证哈希，永不存明文 token。
type storedSession struct {
	ID           string `json:"id"`
	IDHash       string `json:"idHash"`
	IP           string `json:"ip"`
	UserAgent    string `json:"userAgent"`
	CreatedAt    string `json:"createdAt"`
	LastAccessAt string `json:"lastAccessAt"`
	ExpiresAt    string `json:"expiresAt"`
}

type state struct {
	Settings  map[string]string          `json:"settings"`
	Servers   []app.Server               `json:"servers"`
	Versions  []app.FRPCVersion          `json:"versions"`
	Processes map[string]app.ProcessInfo `json:"processes"`
	Sessions  []storedSession            `json:"sessions"`
	Health    []app.HealthEvent          `json:"health"`
	Audit     []app.AuditLog             `json:"audit"`
}

type Store struct {
	mu      sync.Mutex
	dataDir string
	path    string
	st      state
}

func Open(_ context.Context, dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = "frpc-web-data"
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, err
	}

	store := &Store{
		dataDir: dataDir,
		path:    filepath.Join(dataDir, stateFileName),
		st: state{
			Settings:  map[string]string{},
			Processes: map[string]app.ProcessInfo{},
		},
	}

	data, err := os.ReadFile(store.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &store.st); err != nil {
			return nil, fmt.Errorf("parse %s failed: %w", store.path, err)
		}
		if store.st.Settings == nil {
			store.st.Settings = map[string]string{}
		}
		if store.st.Processes == nil {
			store.st.Processes = map[string]app.ProcessInfo{}
		}
	case errors.Is(err, os.ErrNotExist):
		if _, dbErr := os.Stat(filepath.Join(dataDir, "app.db")); dbErr == nil {
			slog.Warn("检测到旧版 SQLite 数据库 app.db；本版本已改用 state.json 存储。" +
				"请在旧版本中导出配置（设置 → 配置备份），再到本版本导入；app.db 不会被修改。")
		}
	default:
		return nil, err
	}

	store.pruneExpired()
	if err := store.save(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) DataDir() string {
	return s.dataDir
}

// save 原子写回状态文件；调用方需持有 s.mu（Open 阶段除外）。
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dataDir, stateFileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	// 先 fsync 再 rename：保证崩溃后磁盘上要么是旧文件、要么是完整的新文件，
	// 不会把只写了一半的内容 rename 成正式状态文件。
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, s.path)
}

// pruneExpired 清理已过期会话与过旧的健康事件，防止状态文件无限增长。
func (s *Store) pruneExpired() {
	now := time.Now()
	live := s.st.Sessions[:0]
	for _, session := range s.st.Sessions {
		expires, err := time.Parse(time.RFC3339, session.ExpiresAt)
		if err == nil && expires.After(now) {
			live = append(live, session)
		}
	}
	s.st.Sessions = live

	cutoff := now.Add(-healthRetention).Format(time.RFC3339)
	health := s.st.Health[:0]
	for _, event := range s.st.Health {
		if event.CreatedAt >= cutoff {
			health = append(health, event)
		}
	}
	s.st.Health = health
}

func (s *Store) GetSetting(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.st.Settings[key]
	if !ok {
		return "", app.ErrNotFound
	}
	return value, nil
}

func (s *Store) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Settings[key] = value
	return s.save()
}

func (s *Store) CreateSession(_ context.Context, session app.Session) (app.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.ID == "" {
		session.ID = newID("sess")
	}
	now := nowString()
	if session.CreatedAt == "" {
		session.CreatedAt = now
	}
	if session.LastAccessAt == "" {
		session.LastAccessAt = now
	}
	s.st.Sessions = append(s.st.Sessions, storedSession{
		ID:           session.ID,
		IDHash:       session.IDHash,
		IP:           session.IP,
		UserAgent:    session.UserAgent,
		CreatedAt:    session.CreatedAt,
		LastAccessAt: session.LastAccessAt,
		ExpiresAt:    session.ExpiresAt,
	})
	if err := s.save(); err != nil {
		return app.Session{}, err
	}
	return session, nil
}

func (s *Store) GetSessionByHash(_ context.Context, idHash string) (app.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.st.Sessions {
		if session.IDHash == idHash {
			return app.Session{
				ID:           session.ID,
				IDHash:       session.IDHash,
				IP:           session.IP,
				UserAgent:    session.UserAgent,
				CreatedAt:    session.CreatedAt,
				LastAccessAt: session.LastAccessAt,
				ExpiresAt:    session.ExpiresAt,
			}, nil
		}
	}
	return app.Session{}, app.ErrNotFound
}

// TouchSession 只更新内存中的最近访问时间，不立刻落盘——
// 避免每个认证请求都重写状态文件；该字段会随下一次状态变更一并持久化。
func (s *Store) TouchSession(_ context.Context, idHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Sessions {
		if s.st.Sessions[i].IDHash == idHash {
			s.st.Sessions[i].LastAccessAt = nowString()
			return nil
		}
	}
	return nil
}

func (s *Store) RevokeSessionByHash(_ context.Context, idHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.st.Sessions[:0]
	for _, session := range s.st.Sessions {
		if session.IDHash != idHash {
			kept = append(kept, session)
		}
	}
	s.st.Sessions = kept
	return s.save()
}

func (s *Store) RevokeAllSessions(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Sessions = nil
	return s.save()
}

func (s *Store) ListServers(_ context.Context) ([]app.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	servers := make([]app.Server, len(s.st.Servers))
	for i := range s.st.Servers {
		servers[i] = cloneServer(s.st.Servers[i])
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].CreatedAt > servers[j].CreatedAt })
	return servers, nil
}

func (s *Store) GetServer(_ context.Context, id string) (app.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(id)
	if err != nil {
		return app.Server{}, err
	}
	return cloneServer(*server), nil
}

func (s *Store) CreateServer(_ context.Context, input app.ServerInput) (app.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input = withAdminDefaults(input)
	if input.AdminPort == 0 {
		input.AdminPort = s.nextAdminPort()
	}
	now := nowString()
	server := app.Server{
		ID:                newID("srv"),
		Name:              input.Name,
		ServerAddr:        input.ServerAddr,
		ServerPort:        input.ServerPort,
		AuthToken:         input.AuthToken,
		TransportProtocol: input.TransportProtocol,
		Status:            "stopped",
		AutoStart:         input.AutoStart,
		AutoRestart:       input.AutoRestart,
		MaxRestarts:       input.MaxRestarts,
		AdminAddr:         "127.0.0.1",
		AdminPort:         input.AdminPort,
		AdminUser:         input.AdminUser,
		AdminPassword:     input.AdminPassword,
		ManagementMode:    "managed", // 默认为完全托管
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	s.st.Servers = append(s.st.Servers, server)
	if err := s.save(); err != nil {
		return app.Server{}, err
	}
	return cloneServer(server), nil
}

func (s *Store) UpdateServer(_ context.Context, id string, input app.ServerInput) (app.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input = withAdminDefaults(input)
	server, _, err := s.findServer(id)
	if err != nil {
		return app.Server{}, err
	}
	server.RestartRequired = server.RestartRequired || server.ServerAddr != input.ServerAddr ||
		server.ServerPort != input.ServerPort || server.AuthToken != input.AuthToken ||
		server.TransportProtocol != input.TransportProtocol || server.AdminPort != input.AdminPort ||
		server.AdminUser != input.AdminUser || server.AdminPassword != input.AdminPassword
	server.Name = input.Name
	server.ServerAddr = input.ServerAddr
	server.ServerPort = input.ServerPort
	server.AuthToken = input.AuthToken
	server.TransportProtocol = input.TransportProtocol
	server.AutoStart = input.AutoStart
	server.AutoRestart = input.AutoRestart
	server.MaxRestarts = input.MaxRestarts
	server.AdminPort = input.AdminPort
	server.AdminUser = input.AdminUser
	server.AdminPassword = input.AdminPassword
	server.UpdatedAt = nowString()
	if err := s.save(); err != nil {
		return app.Server{}, err
	}
	return cloneServer(*server), nil
}

func (s *Store) DeleteServer(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.st.Servers[:0]
	for _, server := range s.st.Servers {
		if server.ID != id {
			kept = append(kept, server)
		}
	}
	s.st.Servers = kept
	delete(s.st.Processes, id)
	return s.save()
}

func (s *Store) SetServerStatus(_ context.Context, id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(id)
	if err != nil {
		return err
	}
	server.Status = status
	server.UpdatedAt = nowString()
	return s.save()
}

func (s *Store) SetServerManagementMode(_ context.Context, id, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(id)
	if err != nil {
		return err
	}
	server.ManagementMode = mode
	server.UpdatedAt = nowString()
	return s.save()
}

func (s *Store) MarkReloaded(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(id)
	if err != nil {
		return err
	}
	now := nowString()
	server.Status = "running"
	server.RestartRequired = false
	server.LastReloadAt = now
	server.UpdatedAt = now
	return s.save()
}

func (s *Store) ListRules(_ context.Context, serverID string) ([]app.ProxyRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(serverID)
	if err != nil {
		return nil, err
	}
	rules := make([]app.ProxyRule, len(server.Rules))
	copy(rules, server.Rules)
	return rules, nil
}

func (s *Store) GetRule(_ context.Context, serverID, ruleID string) (app.ProxyRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rule, _, err := s.findRule(serverID, ruleID)
	if err != nil {
		return app.ProxyRule{}, err
	}
	return *rule, nil
}

func (s *Store) CreateRule(_ context.Context, serverID string, input app.ProxyRuleInput) (app.ProxyRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(serverID)
	if err != nil {
		return app.ProxyRule{}, err
	}
	for _, rule := range server.Rules {
		if rule.Name == input.Name {
			return app.ProxyRule{}, fmt.Errorf("rule name %q already exists", input.Name)
		}
	}
	now := nowString()
	rule := ruleFromInput(input)
	rule.ID = newID("rule")
	rule.ServerID = serverID
	rule.CreatedAt = now
	rule.UpdatedAt = now
	server.Rules = append(server.Rules, rule)
	if err := s.save(); err != nil {
		return app.ProxyRule{}, err
	}
	return rule, nil
}

func (s *Store) UpdateRule(_ context.Context, serverID, ruleID string, input app.ProxyRuleInput) (app.ProxyRule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(serverID)
	if err != nil {
		return app.ProxyRule{}, err
	}
	for _, existing := range server.Rules {
		if existing.ID != ruleID && existing.Name == input.Name {
			return app.ProxyRule{}, fmt.Errorf("rule name %q already exists", input.Name)
		}
	}
	rule, _, err := s.findRule(serverID, ruleID)
	if err != nil {
		return app.ProxyRule{}, err
	}
	updated := ruleFromInput(input)
	updated.ID = rule.ID
	updated.ServerID = rule.ServerID
	updated.CreatedAt = rule.CreatedAt
	updated.UpdatedAt = nowString()
	*rule = updated
	if err := s.save(); err != nil {
		return app.ProxyRule{}, err
	}
	return updated, nil
}

func (s *Store) DeleteRule(_ context.Context, serverID, ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	server, _, err := s.findServer(serverID)
	if err != nil {
		return err
	}
	kept := server.Rules[:0]
	for _, rule := range server.Rules {
		if rule.ID != ruleID {
			kept = append(kept, rule)
		}
	}
	server.Rules = kept
	return s.save()
}

func (s *Store) ListVersions(_ context.Context) ([]app.FRPCVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := make([]app.FRPCVersion, len(s.st.Versions))
	copy(versions, s.st.Versions)
	sort.SliceStable(versions, func(i, j int) bool {
		if versions[i].Active != versions[j].Active {
			return versions[i].Active
		}
		return versions[i].CreatedAt > versions[j].CreatedAt
	})
	return versions, nil
}

func (s *Store) ActiveVersion(_ context.Context) (app.FRPCVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, version := range s.st.Versions {
		if version.Active {
			return version, nil
		}
	}
	return app.FRPCVersion{}, app.ErrNotFound
}

func (s *Store) GetVersion(_ context.Context, id string) (app.FRPCVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, version := range s.st.Versions {
		if version.ID == id {
			return version, nil
		}
	}
	return app.FRPCVersion{}, app.ErrNotFound
}

func (s *Store) AddVersion(_ context.Context, version app.FRPCVersion) (app.FRPCVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version.ID == "" {
		version.ID = newID("frpc")
	}
	if version.CreatedAt == "" {
		version.CreatedAt = nowString()
	}
	if version.Active {
		for i := range s.st.Versions {
			s.st.Versions[i].Active = false
		}
	}
	// 同一 (version, platform, arch) 视为重复安装，替换旧记录。
	kept := s.st.Versions[:0]
	for _, existing := range s.st.Versions {
		duplicate := existing.ID == version.ID ||
			(existing.Version == version.Version && existing.Platform == version.Platform && existing.Arch == version.Arch)
		if !duplicate {
			kept = append(kept, existing)
		}
	}
	s.st.Versions = append(kept, version)
	if err := s.save(); err != nil {
		return app.FRPCVersion{}, err
	}
	return version, nil
}

func (s *Store) SetActiveVersion(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Versions {
		s.st.Versions[i].Active = s.st.Versions[i].ID == id
	}
	return s.save()
}

func (s *Store) UpsertProcess(_ context.Context, info app.ProcessInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Processes[info.ServerID] = info
	return s.save()
}

func (s *Store) GetProcess(_ context.Context, serverID string) (app.ProcessInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.st.Processes[serverID]
	if !ok {
		return app.ProcessInfo{}, app.ErrNotFound
	}
	return info, nil
}

func (s *Store) DeleteProcess(_ context.Context, serverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.st.Processes, serverID)
	return s.save()
}

func (s *Store) ListHealth(_ context.Context) ([]app.HealthEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 20
	if len(s.st.Health) < count {
		count = len(s.st.Health)
	}
	events := make([]app.HealthEvent, count)
	// Health 按写入顺序存储，取末尾 N 条并倒序（最新在前）。
	for i := 0; i < count; i++ {
		events[i] = s.st.Health[len(s.st.Health)-1-i]
	}
	return events, nil
}

func (s *Store) AddHealth(_ context.Context, serverID, level, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	serverName := ""
	if server, _, err := s.findServer(serverID); err == nil {
		serverName = server.Name
	}
	s.st.Health = append(s.st.Health, app.HealthEvent{
		ID:        newID("event"),
		ServerID:  serverID,
		Server:    serverName,
		Level:     level,
		Message:   message,
		CreatedAt: nowString(),
	})
	if overflow := len(s.st.Health) - maxHealthEvents; overflow > 0 {
		s.st.Health = append([]app.HealthEvent(nil), s.st.Health[overflow:]...)
	}
	return s.save()
}

func (s *Store) AddAudit(_ context.Context, input app.AuditLogInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Audit = append(s.st.Audit, app.AuditLog{
		ID:           newID("aud"),
		IP:           input.IP,
		UserAgent:    input.UserAgent,
		Action:       input.Action,
		ResourceType: input.ResourceType,
		ResourceID:   input.ResourceID,
		Result:       input.Result,
		Error:        input.Error,
		CreatedAt:    nowString(),
	})
	if overflow := len(s.st.Audit) - maxAuditEntries; overflow > 0 {
		s.st.Audit = append([]app.AuditLog(nil), s.st.Audit[overflow:]...)
	}
	return s.save()
}

func (s *Store) ListAuditLogs(_ context.Context, query app.AuditLogQuery) (app.AuditLogPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 50
	}
	if query.PageSize > 200 {
		query.PageSize = 200
	}

	action := strings.TrimSpace(query.Action)
	result := strings.TrimSpace(query.Result)
	filtered := make([]app.AuditLog, 0, len(s.st.Audit))
	// Audit 按写入顺序存储，倒序遍历得到最新在前。
	for i := len(s.st.Audit) - 1; i >= 0; i-- {
		entry := s.st.Audit[i]
		if action != "" && entry.Action != action {
			continue
		}
		if result != "" && entry.Result != result {
			continue
		}
		filtered = append(filtered, entry)
	}

	start := (query.Page - 1) * query.PageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + query.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	return app.AuditLogPage{
		Items:    filtered[start:end],
		Total:    len(filtered),
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func (s *Store) ClearAuditLogs(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Audit = nil
	return s.save()
}

func (s *Store) findServer(id string) (*app.Server, int, error) {
	for i := range s.st.Servers {
		if s.st.Servers[i].ID == id {
			return &s.st.Servers[i], i, nil
		}
	}
	return nil, -1, app.ErrNotFound
}

func (s *Store) findRule(serverID, ruleID string) (*app.ProxyRule, int, error) {
	server, _, err := s.findServer(serverID)
	if err != nil {
		return nil, -1, err
	}
	for i := range server.Rules {
		if server.Rules[i].ID == ruleID {
			return &server.Rules[i], i, nil
		}
	}
	return nil, -1, app.ErrNotFound
}

func (s *Store) nextAdminPort() int {
	base := 17400
	used := map[int]bool{}
	for _, server := range s.st.Servers {
		used[server.AdminPort] = true
	}
	for port := base; port < base+1000; port++ {
		if !used[port] {
			return port
		}
	}
	return base
}

// cloneServer 返回深拷贝，避免调用方修改（如掩码处理）影响内部状态。
func cloneServer(server app.Server) app.Server {
	rules := make([]app.ProxyRule, len(server.Rules))
	copy(rules, server.Rules)
	server.Rules = rules
	server.ProxyCount = len(rules)
	server.Uptime = "-"
	return server
}

func ruleFromInput(input app.ProxyRuleInput) app.ProxyRule {
	return app.ProxyRule{
		Name:              input.Name,
		Type:              input.Type,
		LocalIP:           input.LocalIP,
		LocalPort:         input.LocalPort,
		RemotePort:        input.RemotePort,
		CustomDomains:     input.CustomDomains,
		Enabled:           input.Enabled,
		SecretKey:         input.SecretKey,
		Role:              input.Role,
		ServerName:        input.ServerName,
		BindAddr:          input.BindAddr,
		BindPort:          input.BindPort,
		UseEncryption:     input.UseEncryption,
		UseCompression:    input.UseCompression,
		BandwidthLimit:    input.BandwidthLimit,
		Locations:         input.Locations,
		HostHeaderRewrite: input.HostHeaderRewrite,
		HTTPUser:          input.HTTPUser,
		HTTPPassword:      input.HTTPPassword,
		RequestHeaders:    input.RequestHeaders,
	}
}

// withAdminDefaults 补全 Admin API 的凭据默认值。
// 其余字段的清洗和默认值由 app 层的 normalize 函数负责。
func withAdminDefaults(input app.ServerInput) app.ServerInput {
	input.AdminUser = strings.TrimSpace(input.AdminUser)
	input.AdminPassword = strings.TrimSpace(input.AdminPassword)
	if input.AdminUser == "" {
		input.AdminUser = "frpc-web"
	}
	if input.AdminPassword == "" {
		input.AdminPassword = randomToken(16)
	}
	return input
}

func nowString() string {
	return time.Now().Format(time.RFC3339)
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func randomToken(n int) string {
	if n <= 0 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
