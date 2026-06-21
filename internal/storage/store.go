// Package storage 以单个 JSON 状态文件持久化面板自身的数据：设置、会话、健康
// 事件与审计日志。v2.0 起不再持久化 server/rule/version/process——server 列表
// 由扫描磁盘上的 frpc 配置文件实时得到（见 internal/app/configscan.go）。
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
	Settings map[string]string `json:"settings"`
	Sessions []storedSession   `json:"sessions"`
	Health   []app.HealthEvent `json:"health"`
	Audit    []app.AuditLog    `json:"audit"`
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
			Settings: map[string]string{},
		},
	}

	data, err := os.ReadFile(store.path)
	switch {
	case err == nil:
		// 检测到 v1 旧结构（含已废弃的 servers/versions/processes）时，先备份
		// 一份到 state.json.v1.bak，避免被新结构静默覆盖后无法回滚。
		if hasLegacyState(data) {
			backup := store.path + ".v1.bak"
			if _, statErr := os.Stat(backup); errors.Is(statErr, os.ErrNotExist) {
				if writeErr := os.WriteFile(backup, data, 0o600); writeErr == nil {
					slog.Warn("检测到 v1 版本的 state.json（含进程托管数据），已备份为 "+backup+
						"；v2.0 改为扫描配置文件，旧托管记录不再使用。")
				}
			}
		}
		if err := json.Unmarshal(data, &store.st); err != nil {
			return nil, fmt.Errorf("parse %s failed: %w", store.path, err)
		}
		if store.st.Settings == nil {
			store.st.Settings = map[string]string{}
		}
	case errors.Is(err, os.ErrNotExist):
		// 首次启动，无状态文件。
	default:
		return nil, err
	}

	store.pruneExpired()
	if err := store.save(); err != nil {
		return nil, err
	}
	return store, nil
}

// hasLegacyState 判断一段 state.json 内容是否包含 v1 才有的持久化字段
//（进程托管记录 / frpc 版本），用于决定是否先备份再迁移。
func hasLegacyState(data []byte) bool {
	var probe struct {
		Servers  []json.RawMessage `json:"servers"`
		Versions []json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return len(probe.Servers) > 0 || len(probe.Versions) > 0
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
	if serverID != "" {
		// serverName 由调用方在 message 里说明即可（v2.0 不再持久化 server 记录）。
		serverName = serverID
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
