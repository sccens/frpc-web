package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sccens/frpc-web/internal/app"
	_ "modernc.org/sqlite"
)

type Store struct {
	db      *sql.DB
	dataDir string
}

func Open(ctx context.Context, dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = "frpc-web-data"
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "app.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(dbPath, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = db.Close()
		return nil, err
	}

	store := &Store{db: db, dataDir: dataDir}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DataDir() string {
	return s.dataDir
}

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			server_addr TEXT NOT NULL,
			server_port INTEGER NOT NULL,
			auth_token TEXT NOT NULL DEFAULT '',
			transport_protocol TEXT NOT NULL DEFAULT 'tcp',
			config_mode TEXT NOT NULL DEFAULT 'toml_reload',
			auto_start INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'stopped',
			admin_addr TEXT NOT NULL DEFAULT '127.0.0.1',
			admin_port INTEGER NOT NULL,
			frpc_version_id TEXT NOT NULL DEFAULT '',
			restart_required INTEGER NOT NULL DEFAULT 0,
			last_reload_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS proxy_rules (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			local_ip TEXT NOT NULL,
			local_port INTEGER NOT NULL,
			remote_port INTEGER NOT NULL DEFAULT 0,
			custom_domains TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE,
			UNIQUE(server_id, name)
		);`,
		`CREATE TABLE IF NOT EXISTS frpc_versions (
			id TEXT PRIMARY KEY,
			version TEXT NOT NULL,
			platform TEXT NOT NULL,
			arch TEXT NOT NULL,
			binary_path TEXT NOT NULL,
			source TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			UNIQUE(version, platform, arch)
		);`,
		`CREATE TABLE IF NOT EXISTS process_instances (
			server_id TEXT PRIMARY KEY,
			pid INTEGER NOT NULL,
			frpc_version TEXT NOT NULL,
			config_path TEXT NOT NULL,
			log_path TEXT NOT NULL,
			started_at TEXT NOT NULL,
			stopped_at TEXT NOT NULL DEFAULT '',
			exit_code INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS config_versions (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			version_no INTEGER NOT NULL,
			toml_snapshot TEXT NOT NULL,
			change_summary TEXT NOT NULL,
			checksum TEXT NOT NULL,
			created_at TEXT NOT NULL,
			applied_at TEXT NOT NULL DEFAULT '',
			apply_result TEXT NOT NULL DEFAULT 'pending',
			FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS health_events (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL DEFAULT '',
			level TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'open',
			message TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			id_hash TEXT NOT NULL UNIQUE,
			ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			last_access_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			revoked_at TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_id_hash ON sessions(id_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL DEFAULT '',
			username TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			resource_type TEXT NOT NULL DEFAULT '',
			resource_id TEXT NOT NULL DEFAULT '',
			result TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_result ON audit_logs(result);`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "servers", "auto_restart", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "servers", "max_restarts", "INTEGER NOT NULL DEFAULT 3"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "servers", "admin_user", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "servers", "admin_password", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE servers SET admin_user = 'frpc-web' WHERE admin_user = ''`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE servers SET admin_password = lower(hex(randomblob(16))) WHERE admin_password = ''`); err != nil {
		return err
	}
	proxyColumns := []struct {
		name string
		ddl  string
	}{
		{"secret_key", "TEXT NOT NULL DEFAULT ''"},
		{"role", "TEXT NOT NULL DEFAULT ''"},
		{"server_name", "TEXT NOT NULL DEFAULT ''"},
		{"bind_addr", "TEXT NOT NULL DEFAULT ''"},
		{"bind_port", "INTEGER NOT NULL DEFAULT 0"},
		{"use_encryption", "INTEGER NOT NULL DEFAULT 0"},
		{"use_compression", "INTEGER NOT NULL DEFAULT 0"},
		{"bandwidth_limit", "TEXT NOT NULL DEFAULT ''"},
		{"locations", "TEXT NOT NULL DEFAULT '[]'"},
		{"host_header_rewrite", "TEXT NOT NULL DEFAULT ''"},
		{"http_user", "TEXT NOT NULL DEFAULT ''"},
		{"http_password", "TEXT NOT NULL DEFAULT ''"},
		{"request_headers", "TEXT NOT NULL DEFAULT '[]'"},
	}
	for _, column := range proxyColumns {
		if err := s.ensureColumn(ctx, "proxy_rules", column.name, column.ddl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, ddl string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, ddl))
	return err
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	return value, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_settings (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowString())
	return err
}

func (s *Store) CreateSession(ctx context.Context, session app.Session) (app.Session, error) {
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions
		(id, id_hash, ip, user_agent, created_at, last_access_at, expires_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.IDHash, session.IP, session.UserAgent, session.CreatedAt, session.LastAccessAt, session.ExpiresAt, session.RevokedAt)
	if err != nil {
		return app.Session{}, err
	}
	return s.GetSessionByHash(ctx, session.IDHash)
}

func (s *Store) GetSessionByHash(ctx context.Context, idHash string) (app.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, id_hash, ip, user_agent, created_at, last_access_at, expires_at, revoked_at
		FROM sessions WHERE id_hash = ?`, idHash)
	return scanSession(row)
}

func (s *Store) ListSessions(ctx context.Context) ([]app.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, id_hash, ip, user_agent, created_at, last_access_at, expires_at, revoked_at
		FROM sessions ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := make([]app.Session, 0)
	for rows.Next() {
		session, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func (s *Store) TouchSession(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_access_at = ? WHERE id_hash = ? AND revoked_at = ''`, nowString(), idHash)
	return err
}

func (s *Store) RevokeSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id = ? AND revoked_at = ''`, nowString(), id)
	return err
}

func (s *Store) RevokeSessionByHash(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id_hash = ? AND revoked_at = ''`, nowString(), idHash)
	return err
}

func (s *Store) RevokeOtherSessions(ctx context.Context, idHash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE id_hash <> ? AND revoked_at = ''`, nowString(), idHash)
	return err
}

func (s *Store) RevokeAllSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at = ? WHERE revoked_at = ''`, nowString())
	return err
}

func (s *Store) ListServers(ctx context.Context) ([]app.Server, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, server_addr, server_port, auth_token, transport_protocol,
		config_mode, auto_start, auto_restart, max_restarts, status, admin_addr, admin_port, admin_user, admin_password, frpc_version_id, restart_required, last_reload_at,
		created_at, updated_at FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := make([]app.Server, 0)
	for rows.Next() {
		server, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	for i := range servers {
		rules, err := s.ListRules(ctx, servers[i].ID)
		if err != nil {
			return nil, err
		}
		servers[i].Rules = rules
		servers[i].ProxyCount = len(rules)
		servers[i].Uptime = "-"
	}
	return servers, rows.Err()
}

func (s *Store) GetServer(ctx context.Context, id string) (app.Server, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, server_addr, server_port, auth_token, transport_protocol,
		config_mode, auto_start, auto_restart, max_restarts, status, admin_addr, admin_port, admin_user, admin_password, frpc_version_id, restart_required, last_reload_at,
		created_at, updated_at FROM servers WHERE id = ?`, id)
	server, err := scanServer(row)
	if err != nil {
		return app.Server{}, err
	}
	rules, err := s.ListRules(ctx, server.ID)
	if err != nil {
		return app.Server{}, err
	}
	server.Rules = rules
	server.ProxyCount = len(rules)
	server.Uptime = "-"
	return server, nil
}

func (s *Store) CreateServer(ctx context.Context, input app.ServerInput) (app.Server, error) {
	now := nowString()
	id := newID("srv")
	input = normalizeServerInput(input)
	if input.AdminPort == 0 {
		input.AdminPort = s.NextAdminPort(ctx)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO servers (
		id, name, server_addr, server_port, auth_token, transport_protocol, config_mode, auto_start, auto_restart, max_restarts, status,
		admin_addr, admin_port, admin_user, admin_password, frpc_version_id, restart_required, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'stopped', '127.0.0.1', ?, ?, ?, ?, 0, ?, ?)`,
		id, input.Name, input.ServerAddr, input.ServerPort, input.AuthToken, input.TransportProtocol,
		input.ConfigMode, boolInt(input.AutoStart), boolInt(input.AutoRestart), input.MaxRestarts, input.AdminPort, input.AdminUser, input.AdminPassword, input.FRPCVersionID, now, now)
	if err != nil {
		return app.Server{}, err
	}
	return s.GetServer(ctx, id)
}

func (s *Store) UpdateServer(ctx context.Context, id string, input app.ServerInput) (app.Server, error) {
	input = normalizeServerInput(input)
	current, err := s.GetServer(ctx, id)
	if err != nil {
		return app.Server{}, err
	}
	restartRequired := current.RestartRequired || current.ServerAddr != input.ServerAddr ||
		current.ServerPort != input.ServerPort || current.AuthToken != input.AuthToken ||
		current.TransportProtocol != input.TransportProtocol || current.AdminPort != input.AdminPort ||
		current.ConfigMode != input.ConfigMode
	_, err = s.db.ExecContext(ctx, `UPDATE servers SET name = ?, server_addr = ?, server_port = ?, auth_token = ?,
		transport_protocol = ?, config_mode = ?, auto_start = ?, auto_restart = ?, max_restarts = ?, admin_port = ?, admin_user = ?, admin_password = ?, frpc_version_id = ?,
		restart_required = ?, updated_at = ? WHERE id = ?`,
		input.Name, input.ServerAddr, input.ServerPort, input.AuthToken, input.TransportProtocol, input.ConfigMode,
		boolInt(input.AutoStart), boolInt(input.AutoRestart), input.MaxRestarts, input.AdminPort, input.AdminUser, input.AdminPassword, input.FRPCVersionID, boolInt(restartRequired), nowString(), id)
	if err != nil {
		return app.Server{}, err
	}
	return s.GetServer(ctx, id)
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	return err
}

func (s *Store) SetServerStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET status = ?, updated_at = ? WHERE id = ?`, status, nowString(), id)
	return err
}

func (s *Store) MarkReloaded(ctx context.Context, id string) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `UPDATE servers SET status = 'running', restart_required = 0, last_reload_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) ListRules(ctx context.Context, serverID string) ([]app.ProxyRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, server_id, name, type, local_ip, local_port, remote_port,
		custom_domains, enabled, secret_key, role, server_name, bind_addr, bind_port, use_encryption, use_compression,
		bandwidth_limit, locations, host_header_rewrite, http_user, http_password, request_headers, created_at, updated_at
		FROM proxy_rules WHERE server_id = ? ORDER BY created_at ASC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := make([]app.ProxyRule, 0)
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) GetRule(ctx context.Context, serverID, ruleID string) (app.ProxyRule, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, server_id, name, type, local_ip, local_port, remote_port,
		custom_domains, enabled, secret_key, role, server_name, bind_addr, bind_port, use_encryption, use_compression,
		bandwidth_limit, locations, host_header_rewrite, http_user, http_password, request_headers, created_at, updated_at
		FROM proxy_rules WHERE server_id = ? AND id = ?`, serverID, ruleID)
	return scanRule(row)
}

func (s *Store) CreateRule(ctx context.Context, serverID string, input app.ProxyRuleInput) (app.ProxyRule, error) {
	input = normalizeRuleInput(input)
	now := nowString()
	id := newID("rule")
	domains, _ := json.Marshal(input.CustomDomains)
	locations, _ := json.Marshal(input.Locations)
	headers, _ := json.Marshal(input.RequestHeaders)
	_, err := s.db.ExecContext(ctx, `INSERT INTO proxy_rules (
		id, server_id, name, type, local_ip, local_port, remote_port, custom_domains, enabled, secret_key, role, server_name,
		bind_addr, bind_port, use_encryption, use_compression, bandwidth_limit, locations, host_header_rewrite, http_user,
		http_password, request_headers, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, serverID, input.Name, input.Type, input.LocalIP, input.LocalPort, input.RemotePort, string(domains), boolInt(input.Enabled),
		input.SecretKey, input.Role, input.ServerName, input.BindAddr, input.BindPort, boolInt(input.UseEncryption), boolInt(input.UseCompression),
		input.BandwidthLimit, string(locations), input.HostHeaderRewrite, input.HTTPUser, input.HTTPPassword, string(headers), now, now)
	if err != nil {
		return app.ProxyRule{}, err
	}
	return s.GetRule(ctx, serverID, id)
}

func (s *Store) UpdateRule(ctx context.Context, serverID, ruleID string, input app.ProxyRuleInput) (app.ProxyRule, error) {
	input = normalizeRuleInput(input)
	domains, _ := json.Marshal(input.CustomDomains)
	locations, _ := json.Marshal(input.Locations)
	headers, _ := json.Marshal(input.RequestHeaders)
	_, err := s.db.ExecContext(ctx, `UPDATE proxy_rules SET name = ?, type = ?, local_ip = ?, local_port = ?,
		remote_port = ?, custom_domains = ?, enabled = ?, secret_key = ?, role = ?, server_name = ?, bind_addr = ?, bind_port = ?,
		use_encryption = ?, use_compression = ?, bandwidth_limit = ?, locations = ?, host_header_rewrite = ?, http_user = ?,
		http_password = ?, request_headers = ?, updated_at = ? WHERE server_id = ? AND id = ?`,
		input.Name, input.Type, input.LocalIP, input.LocalPort, input.RemotePort, string(domains), boolInt(input.Enabled), input.SecretKey,
		input.Role, input.ServerName, input.BindAddr, input.BindPort, boolInt(input.UseEncryption), boolInt(input.UseCompression),
		input.BandwidthLimit, string(locations), input.HostHeaderRewrite, input.HTTPUser, input.HTTPPassword, string(headers), nowString(), serverID, ruleID)
	if err != nil {
		return app.ProxyRule{}, err
	}
	return s.GetRule(ctx, serverID, ruleID)
}

func (s *Store) DeleteRule(ctx context.Context, serverID, ruleID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM proxy_rules WHERE server_id = ? AND id = ?`, serverID, ruleID)
	return err
}

func (s *Store) ListVersions(ctx context.Context) ([]app.FRPCVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, version, platform, arch, binary_path, source, active, created_at
		FROM frpc_versions ORDER BY active DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := make([]app.FRPCVersion, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
}

func (s *Store) ActiveVersion(ctx context.Context) (app.FRPCVersion, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, version, platform, arch, binary_path, source, active, created_at
		FROM frpc_versions WHERE active = 1 ORDER BY created_at DESC LIMIT 1`)
	return scanVersion(row)
}

func (s *Store) GetVersion(ctx context.Context, id string) (app.FRPCVersion, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, version, platform, arch, binary_path, source, active, created_at
		FROM frpc_versions WHERE id = ?`, id)
	return scanVersion(row)
}

func (s *Store) AddVersion(ctx context.Context, version app.FRPCVersion) (app.FRPCVersion, error) {
	if version.ID == "" {
		version.ID = newID("frpc")
	}
	if version.CreatedAt == "" {
		version.CreatedAt = nowString()
	}
	if version.Active {
		if _, err := s.db.ExecContext(ctx, `UPDATE frpc_versions SET active = 0`); err != nil {
			return app.FRPCVersion{}, err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO frpc_versions
		(id, version, platform, arch, binary_path, source, active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.Version, version.Platform, version.Arch, version.Path, version.Source, boolInt(version.Active), version.CreatedAt)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	return version, nil
}

func (s *Store) SetActiveVersion(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE frpc_versions SET active = 0`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE frpc_versions SET active = 1 WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertProcess(ctx context.Context, info app.ProcessInfo) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO process_instances
		(server_id, pid, frpc_version, config_path, log_path, started_at, stopped_at, exit_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id) DO UPDATE SET pid = excluded.pid, frpc_version = excluded.frpc_version,
		config_path = excluded.config_path, log_path = excluded.log_path, started_at = excluded.started_at,
		stopped_at = excluded.stopped_at, exit_code = excluded.exit_code`,
		info.ServerID, info.PID, info.FRPCVersion, info.ConfigPath, info.LogPath, info.StartedAt, info.StoppedAt, info.ExitCode)
	return err
}

func (s *Store) GetProcess(ctx context.Context, serverID string) (app.ProcessInfo, error) {
	row := s.db.QueryRowContext(ctx, `SELECT server_id, pid, frpc_version, config_path, log_path, started_at, stopped_at, exit_code
		FROM process_instances WHERE server_id = ?`, serverID)
	var info app.ProcessInfo
	err := row.Scan(&info.ServerID, &info.PID, &info.FRPCVersion, &info.ConfigPath, &info.LogPath, &info.StartedAt, &info.StoppedAt, &info.ExitCode)
	return info, err
}

func (s *Store) DeleteProcess(ctx context.Context, serverID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM process_instances WHERE server_id = ?`, serverID)
	return err
}

func (s *Store) AddConfigVersion(ctx context.Context, version app.ConfigVersion) error {
	if version.ID == "" {
		version.ID = newID("cfg")
	}
	if version.CreatedAt == "" {
		version.CreatedAt = nowString()
	}
	if version.VersionNo == 0 {
		version.VersionNo = s.nextConfigVersionNo(ctx, version.ServerID)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO config_versions
		(id, server_id, version_no, toml_snapshot, change_summary, checksum, created_at, applied_at, apply_result)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.ServerID, version.VersionNo, version.TOMLSnapshot, version.ChangeSummary, version.Checksum,
		version.CreatedAt, version.AppliedAt, version.ApplyResult)
	return err
}

func (s *Store) ListHealth(ctx context.Context) ([]app.HealthEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT h.id, h.server_id, COALESCE(s.name, ''), h.level, h.status, h.message, h.created_at
		FROM health_events h LEFT JOIN servers s ON s.id = h.server_id ORDER BY h.created_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]app.HealthEvent, 0)
	for rows.Next() {
		var event app.HealthEvent
		if err := rows.Scan(&event.ID, &event.ServerID, &event.Server, &event.Level, &event.Status, &event.Message, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) AddHealth(ctx context.Context, serverID, level, message string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO health_events (id, server_id, level, status, message, created_at)
		VALUES (?, ?, ?, 'open', ?, ?)`, newID("event"), serverID, level, message, nowString())
	return err
}

func (s *Store) AddAudit(ctx context.Context, input app.AuditLogInput) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_logs (
		id, user_id, username, role, ip, user_agent, action, resource_type, resource_id, result, error, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID("aud"), input.UserID, input.Username, input.Role, input.IP, input.UserAgent, input.Action,
		input.ResourceType, input.ResourceID, input.Result, input.Error, nowString())
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, query app.AuditLogQuery) (app.AuditLogPage, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 50
	}
	if query.PageSize > 200 {
		query.PageSize = 200
	}

	where, args := auditWhere(query)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return app.AuditLogPage{}, err
	}

	offset := (query.Page - 1) * query.PageSize
	listArgs := append(append([]any{}, args...), query.PageSize, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, username, role, ip, user_agent, action, resource_type,
		resource_id, result, error, created_at FROM audit_logs`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return app.AuditLogPage{}, err
	}
	defer rows.Close()

	items := make([]app.AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return app.AuditLogPage{}, err
		}
		items = append(items, item)
	}
	return app.AuditLogPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, rows.Err()
}

func (s *Store) nextConfigVersionNo(ctx context.Context, serverID string) int {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_no), 0) + 1 FROM config_versions WHERE server_id = ?`, serverID).Scan(&n)
	if n == 0 {
		return 1
	}
	return n
}

func (s *Store) NextAdminPort(ctx context.Context) int {
	base := 17400
	rows, err := s.db.QueryContext(ctx, `SELECT admin_port FROM servers`)
	if err != nil {
		return base
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var port int
		if rows.Scan(&port) == nil {
			used[port] = true
		}
	}
	for port := base; port < base+1000; port++ {
		if !used[port] {
			return port
		}
	}
	return base
}

type scanner interface {
	Scan(dest ...any) error
}

func scanServer(row scanner) (app.Server, error) {
	var server app.Server
	var autoStart, autoRestart, restartRequired int
	err := row.Scan(&server.ID, &server.Name, &server.ServerAddr, &server.ServerPort, &server.AuthToken,
		&server.TransportProtocol, &server.ConfigMode, &autoStart, &autoRestart, &server.MaxRestarts, &server.Status, &server.AdminAddr,
		&server.AdminPort, &server.AdminUser, &server.AdminPassword, &server.FRPCVersionID, &restartRequired, &server.LastReloadAt, &server.CreatedAt, &server.UpdatedAt)
	server.AutoStart = autoStart == 1
	server.AutoRestart = autoRestart == 1
	if server.MaxRestarts <= 0 {
		server.MaxRestarts = 3
	}
	server.RestartRequired = restartRequired == 1
	return server, err
}

func scanRule(row scanner) (app.ProxyRule, error) {
	var rule app.ProxyRule
	var customDomains, locations, requestHeaders string
	var enabled, useEncryption, useCompression int
	err := row.Scan(&rule.ID, &rule.ServerID, &rule.Name, &rule.Type, &rule.LocalIP, &rule.LocalPort,
		&rule.RemotePort, &customDomains, &enabled, &rule.SecretKey, &rule.Role, &rule.ServerName, &rule.BindAddr,
		&rule.BindPort, &useEncryption, &useCompression, &rule.BandwidthLimit, &locations, &rule.HostHeaderRewrite,
		&rule.HTTPUser, &rule.HTTPPassword, &requestHeaders, &rule.CreatedAt, &rule.UpdatedAt)
	if customDomains != "" {
		_ = json.Unmarshal([]byte(customDomains), &rule.CustomDomains)
	}
	if locations != "" {
		_ = json.Unmarshal([]byte(locations), &rule.Locations)
	}
	if requestHeaders != "" {
		_ = json.Unmarshal([]byte(requestHeaders), &rule.RequestHeaders)
	}
	rule.Enabled = enabled == 1
	rule.UseEncryption = useEncryption == 1
	rule.UseCompression = useCompression == 1
	return rule, err
}

func scanVersion(row scanner) (app.FRPCVersion, error) {
	var version app.FRPCVersion
	var active int
	err := row.Scan(&version.ID, &version.Version, &version.Platform, &version.Arch, &version.Path, &version.Source, &active, &version.CreatedAt)
	version.Active = active == 1
	version.Installed = version.Path != ""
	version.Latest = version.Version
	return version, err
}

func scanSession(row scanner) (app.Session, error) {
	var session app.Session
	err := row.Scan(&session.ID, &session.IDHash, &session.IP, &session.UserAgent, &session.CreatedAt, &session.LastAccessAt, &session.ExpiresAt, &session.RevokedAt)
	return session, err
}

func scanAuditLog(row scanner) (app.AuditLog, error) {
	var log app.AuditLog
	err := row.Scan(&log.ID, &log.UserID, &log.Username, &log.Role, &log.IP, &log.UserAgent, &log.Action,
		&log.ResourceType, &log.ResourceID, &log.Result, &log.Error, &log.CreatedAt)
	return log, err
}

func auditWhere(query app.AuditLogQuery) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if action := strings.TrimSpace(query.Action); action != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, action)
	}
	if user := strings.TrimSpace(query.User); user != "" {
		clauses = append(clauses, "(username LIKE ? OR user_id = ?)")
		args = append(args, "%"+user+"%", user)
	}
	if result := strings.TrimSpace(query.Result); result != "" {
		clauses = append(clauses, "result = ?")
		args = append(args, result)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func normalizeServerInput(input app.ServerInput) app.ServerInput {
	input.Name = strings.TrimSpace(input.Name)
	input.ServerAddr = strings.TrimSpace(input.ServerAddr)
	input.AuthToken = strings.TrimSpace(input.AuthToken)
	input.AdminUser = strings.TrimSpace(input.AdminUser)
	input.AdminPassword = strings.TrimSpace(input.AdminPassword)
	if input.AdminUser == "" {
		input.AdminUser = "frpc-web"
	}
	if input.AdminPassword == "" {
		input.AdminPassword = randomToken(16)
	}
	if input.ServerPort == 0 {
		input.ServerPort = 7000
	}
	if input.TransportProtocol == "" {
		input.TransportProtocol = "tcp"
	}
	if input.ConfigMode == "" {
		input.ConfigMode = "toml_reload"
	}
	if input.MaxRestarts <= 0 {
		input.MaxRestarts = 3
	}
	return input
}

func normalizeRuleInput(input app.ProxyRuleInput) app.ProxyRuleInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	input.SecretKey = strings.TrimSpace(input.SecretKey)
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	input.ServerName = strings.TrimSpace(input.ServerName)
	input.BindAddr = strings.TrimSpace(input.BindAddr)
	input.BandwidthLimit = strings.TrimSpace(input.BandwidthLimit)
	input.HostHeaderRewrite = strings.TrimSpace(input.HostHeaderRewrite)
	input.HTTPUser = strings.TrimSpace(input.HTTPUser)
	input.HTTPPassword = strings.TrimSpace(input.HTTPPassword)
	if input.LocalIP == "" {
		input.LocalIP = "127.0.0.1"
	}
	if input.BindAddr == "" {
		input.BindAddr = "127.0.0.1"
	}
	input.LocalIP = strings.TrimSpace(input.LocalIP)
	input.CustomDomains = cleanStringList(input.CustomDomains)
	input.Locations = cleanStringList(input.Locations)
	input.RequestHeaders = cleanStringList(input.RequestHeaders)
	if !input.Enabled {
		input.Enabled = false
	}
	return input
}

func cleanStringList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
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
