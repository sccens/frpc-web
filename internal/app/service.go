package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type Store interface {
	DataDir() string
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	CreateSession(ctx context.Context, session Session) (Session, error)
	GetSessionByHash(ctx context.Context, idHash string) (Session, error)
	TouchSession(ctx context.Context, idHash string) error
	RevokeSessionByHash(ctx context.Context, idHash string) error
	RevokeAllSessions(ctx context.Context) error
	ListServers(ctx context.Context) ([]Server, error)
	GetServer(ctx context.Context, id string) (Server, error)
	CreateServer(ctx context.Context, input ServerInput) (Server, error)
	UpdateServer(ctx context.Context, id string, input ServerInput) (Server, error)
	DeleteServer(ctx context.Context, id string) error
	SetServerStatus(ctx context.Context, id, status string) error
	MarkReloaded(ctx context.Context, id string) error
	ListRules(ctx context.Context, serverID string) ([]ProxyRule, error)
	CreateRule(ctx context.Context, serverID string, input ProxyRuleInput) (ProxyRule, error)
	UpdateRule(ctx context.Context, serverID, ruleID string, input ProxyRuleInput) (ProxyRule, error)
	DeleteRule(ctx context.Context, serverID, ruleID string) error
	GetRule(ctx context.Context, serverID, ruleID string) (ProxyRule, error)
	ListVersions(ctx context.Context) ([]FRPCVersion, error)
	ActiveVersion(ctx context.Context) (FRPCVersion, error)
	GetVersion(ctx context.Context, id string) (FRPCVersion, error)
	AddVersion(ctx context.Context, version FRPCVersion) (FRPCVersion, error)
	SetActiveVersion(ctx context.Context, id string) error
	UpsertProcess(ctx context.Context, info ProcessInfo) error
	GetProcess(ctx context.Context, serverID string) (ProcessInfo, error)
	DeleteProcess(ctx context.Context, serverID string) error
	ListHealth(ctx context.Context) ([]HealthEvent, error)
	AddHealth(ctx context.Context, serverID, level, message string) error
	AddAudit(ctx context.Context, input AuditLogInput) error
	ListAuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogPage, error)
	ClearAuditLogs(ctx context.Context) error
}

type Runtime interface {
	RenderConfig(ctx context.Context, server Server) (ConfigPreview, error)
	CheckConfig(ctx context.Context, server Server, version FRPCVersion) ActionResult
	Start(ctx context.Context, server Server, version FRPCVersion) (ProcessInfo, ActionResult)
	Stop(ctx context.Context, server Server, process ProcessInfo) ActionResult
	Reload(ctx context.Context, server Server, version FRPCVersion) ActionResult
	Logs(ctx context.Context, serverID string, tail int) ([]LogLine, error)
	InstallOnline(ctx context.Context, input FRPCInstallOnlineInput) (FRPCVersion, error)
	InstallOffline(ctx context.Context, filename string, file io.Reader) (FRPCVersion, error)
	LatestVersion(ctx context.Context, githubProxy string) (string, error)
	ProcessAlive(ctx context.Context, pid int) bool
	Adopt(serverID string, pid int)
	SetExitHandler(handler func(serverID string, err error))
}

var (
	ErrAlreadyBootstrapped = errors.New("access key already initialized")
	ErrBootstrapRequired   = errors.New("access key initialization required")
	ErrInvalidInput        = errors.New("invalid input")
	ErrInvalidCredentials  = errors.New("invalid access key")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrNotFound            = errors.New("resource not found")
)

type Options struct {
	Store   Store
	Runtime Runtime
	Addr    string
	Version string
}

type Service struct {
	store           Store
	runtime         Runtime
	addr            string
	version         string
	bootstrapMu     sync.Mutex
	restartMu       sync.Mutex
	restartAttempts map[string]int
	restartTimers   map[string]*time.Timer
}

func NewService(opts Options) *Service {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}
	svc := &Service{
		store:           opts.Store,
		runtime:         opts.Runtime,
		addr:            opts.Addr,
		version:         version,
		restartAttempts: map[string]int{},
		restartTimers:   map[string]*time.Timer{},
	}
	if opts.Runtime != nil {
		opts.Runtime.SetExitHandler(svc.handleRuntimeExit)
	}
	return svc
}

func (s *Service) Restore(ctx context.Context) error {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return err
	}

	for _, server := range servers {
		if isRunningState(server.Status) {
			process, err := s.store.GetProcess(ctx, server.ID)
			if err != nil || !s.runtime.ProcessAlive(ctx, process.PID) {
				_ = s.store.DeleteProcess(ctx, server.ID)
				_ = s.store.SetServerStatus(ctx, server.ID, "stopped")
				_ = s.store.AddHealth(ctx, server.ID, "warning", "上次运行的 frpc 进程不存在，已恢复为停止状态")
			} else {
				// 进程仍在运行（如自更新原地重启后遗留的子进程）：
				// 接管监控，退出时仍能触发自动重启等生命周期处理。
				s.runtime.Adopt(server.ID, process.PID)
			}
		}
	}

	servers, err = s.store.ListServers(ctx)
	if err != nil {
		return err
	}
	for _, server := range servers {
		if server.AutoStart && server.Status != "running" && server.Status != "config_dirty" {
			result := s.Start(ctx, server.ID)
			if !result.OK {
				_ = s.store.AddHealth(ctx, server.ID, "warning", "自动启动失败: "+result.Message)
			}
		}
	}
	return nil
}

func (s *Service) handleRuntimeExit(serverID string, exitErr error) {
	if strings.TrimSpace(serverID) == "" {
		return
	}
	server, err := s.store.GetServer(context.Background(), serverID)
	if err != nil {
		return
	}
	process, processErr := s.store.GetProcess(context.Background(), serverID)
	_ = s.store.DeleteProcess(context.Background(), serverID)

	if !server.AutoRestart {
		_ = s.store.SetServerStatus(context.Background(), serverID, "error")
		_ = s.store.AddHealth(context.Background(), serverID, "warning", "frpc 进程已退出: "+exitErrorText(exitErr))
		return
	}

	// 稳定运行一段时间后才崩溃的，视为新一轮故障，不累计历史次数；
	// 否则数月间偶发 N 次崩溃就会永久禁用自动重启。
	if processErr == nil {
		if started, err := time.Parse(time.RFC3339, process.StartedAt); err == nil && time.Since(started) >= stableRunDuration {
			s.restartMu.Lock()
			delete(s.restartAttempts, serverID)
			s.restartMu.Unlock()
		}
	}

	maxRestarts := server.MaxRestarts
	if maxRestarts <= 0 {
		maxRestarts = 3
	}

	s.restartMu.Lock()
	attempt := s.restartAttempts[serverID] + 1
	s.restartAttempts[serverID] = attempt
	if existing := s.restartTimers[serverID]; existing != nil {
		existing.Stop()
	}
	if attempt > maxRestarts {
		delete(s.restartTimers, serverID)
		s.restartMu.Unlock()
		_ = s.store.SetServerStatus(context.Background(), serverID, "error")
		_ = s.store.AddHealth(context.Background(), serverID, "critical", fmt.Sprintf("frpc 连续崩溃 %d 次，已停止自动重启: %s", maxRestarts, exitErrorText(exitErr)))
		return
	}
	delay := restartBackoff(attempt)
	var timer *time.Timer
	timer = time.AfterFunc(delay, func() {
		s.restartMu.Lock()
		// 手动 Stop/Start 会移除注册的定时器；若已不是本定时器，说明重启被取消。
		if s.restartTimers[serverID] != timer {
			s.restartMu.Unlock()
			return
		}
		delete(s.restartTimers, serverID)
		s.restartMu.Unlock()
		result := s.start(context.Background(), serverID, false)
		if !result.OK {
			_ = s.store.SetServerStatus(context.Background(), serverID, "error")
			_ = s.store.AddHealth(context.Background(), serverID, "warning", fmt.Sprintf("自动重启失败: %s", result.Message))
		}
	})
	s.restartTimers[serverID] = timer
	s.restartMu.Unlock()

	_ = s.store.SetServerStatus(context.Background(), serverID, "starting")
	_ = s.store.AddHealth(context.Background(), serverID, "warning", fmt.Sprintf("frpc 进程异常退出，%s 后尝试自动重启 (%d/%d): %s", durationTextShort(delay), attempt, maxRestarts, exitErrorText(exitErr)))
}

func (s *Service) AuthStatus(ctx context.Context) (AuthStatus, error) {
	return AuthStatus{Bootstrapped: s.accessKeyConfigured(ctx)}, nil
}

func (s *Service) Bootstrap(ctx context.Context, input AuthInput, meta AuthMeta) (Session, error) {
	// 串行化首次初始化，避免并发请求都通过 accessKeyConfigured 检查后互相覆盖。
	s.bootstrapMu.Lock()
	defer s.bootstrapMu.Unlock()
	if s.accessKeyConfigured(ctx) {
		return Session{}, ErrAlreadyBootstrapped
	}
	accessKey, err := normalizeAccessKey(input.AccessKey)
	if err != nil {
		return Session{}, invalidInput(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(accessKey), bcrypt.DefaultCost)
	if err != nil {
		return Session{}, err
	}
	if err := s.store.SetSetting(ctx, "access_key_hash", string(hash)); err != nil {
		return Session{}, err
	}
	return s.newSession(ctx, meta)
}

func (s *Service) Login(ctx context.Context, input AuthInput, meta AuthMeta) (Session, error) {
	accessKey, err := normalizeAccessKey(input.AccessKey)
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}
	if !s.accessKeyConfigured(ctx) {
		return Session{}, ErrBootstrapRequired
	}
	if err := s.verifyAccessKey(ctx, accessKey); err != nil {
		return Session{}, ErrInvalidCredentials
	}
	return s.newSession(ctx, meta)
}

func (s *Service) VerifySession(ctx context.Context, sessionID string) (Session, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Session{}, ErrUnauthorized
	}
	session, err := s.store.GetSessionByHash(ctx, hashSessionID(sessionID))
	if err != nil {
		return Session{}, ErrUnauthorized
	}
	if sessionExpired(session.ExpiresAt) {
		return Session{}, ErrUnauthorized
	}
	_ = s.store.TouchSession(ctx, session.IDHash)
	session.Token = sessionID
	return session, nil
}

func (s *Service) RevokeCurrentSession(ctx context.Context, sessionID string) error {
	return s.store.RevokeSessionByHash(ctx, hashSessionID(sessionID))
}

func (s *Service) ChangeAccessKey(ctx context.Context, input AccessKeyInput) error {
	if strings.TrimSpace(os.Getenv("FRPC_WEB_ACCESS_KEY")) != "" {
		return invalidInput(errors.New("access key is controlled by FRPC_WEB_ACCESS_KEY; edit the environment variable and restart frpc-web"))
	}
	current, err := normalizeAccessKey(input.CurrentAccessKey)
	if err != nil {
		return invalidInput(err)
	}
	next, err := normalizeAccessKey(input.NewAccessKey)
	if err != nil {
		return invalidInput(err)
	}
	if err := s.verifyAccessKey(ctx, current); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, "access_key_hash", string(hash)); err != nil {
		return err
	}
	return s.store.RevokeAllSessions(ctx)
}

func (s *Service) AuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogPage, error) {
	return s.store.ListAuditLogs(ctx, query)
}

func (s *Service) ClearAuditLogs(ctx context.Context) error {
	return s.store.ClearAuditLogs(ctx)
}

func (s *Service) AddAudit(ctx context.Context, input AuditLogInput) {
	input.Action = strings.TrimSpace(input.Action)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.Result = strings.TrimSpace(input.Result)
	if input.Result == "" {
		input.Result = "success"
	}
	if input.Action == "" {
		return
	}
	_ = s.store.AddAudit(ctx, input)
}

func (s *Service) Dashboard(ctx context.Context) (Dashboard, error) {
	servers, err := s.Servers(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	health, err := s.store.ListHealth(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	frpc := s.currentVersion(ctx)

	running := 0
	rules := 0
	for _, server := range servers {
		if isRunningState(server.Status) {
			running++
		}
		rules += server.ProxyCount
	}

	settings, err := s.Settings(ctx)
	if err != nil {
		return Dashboard{}, err
	}

	return Dashboard{
		Summary: Summary{
			TotalServers:   len(servers),
			RunningServers: running,
			ProxyRules:     rules,
			OpenEvents:     len(health),
		},
		Servers:     servers,
		Health:      health,
		CurrentFRPC: frpc,
		Settings:    settings,
	}, nil
}

func (s *Service) Settings(ctx context.Context) (Settings, error) {
	githubProxy, err := s.store.GetSetting(ctx, "github_proxy")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Settings{}, err
	}
	return Settings{
		Addr:        s.addr,
		GithubProxy: strings.TrimSpace(githubProxy),
	}, nil
}

func (s *Service) UpdateSettings(ctx context.Context, input SettingsInput) (Settings, error) {
	if err := s.store.SetSetting(ctx, "github_proxy", strings.TrimSpace(input.GithubProxy)); err != nil {
		return Settings{}, err
	}
	return s.Settings(ctx)
}

func (s *Service) Servers(ctx context.Context) ([]Server, error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		servers[i] = s.withRuntimeFields(ctx, servers[i])
	}
	return servers, nil
}

func (s *Service) Server(ctx context.Context, id string) (Server, error) {
	server, err := s.store.GetServer(ctx, id)
	if err != nil {
		return Server{}, err
	}
	return s.withRuntimeFields(ctx, server), nil
}

func (s *Service) CreateServer(ctx context.Context, input ServerInput) (Server, error) {
	input = normalizeServerDefaults(input)
	if err := validateServer(input); err != nil {
		return Server{}, invalidInput(err)
	}
	server, err := s.store.CreateServer(ctx, input)
	if err != nil {
		return Server{}, err
	}
	_, _ = s.applyConfig(ctx, server.ID)
	return s.Server(ctx, server.ID)
}

func (s *Service) UpdateServer(ctx context.Context, id string, input ServerInput) (Server, error) {
	current, err := s.store.GetServer(ctx, id)
	if err != nil {
		return Server{}, err
	}
	input = normalizeServerDefaults(input)
	if input.AuthToken == "" || LooksMaskedSecret(input.AuthToken) {
		input.AuthToken = current.AuthToken
	}
	if input.AdminUser == "" {
		input.AdminUser = current.AdminUser
	}
	if input.AdminPassword == "" || LooksMaskedSecret(input.AdminPassword) {
		input.AdminPassword = current.AdminPassword
	}
	if input.AdminPort == 0 {
		input.AdminPort = current.AdminPort
	}
	if err := validateServer(input); err != nil {
		return Server{}, invalidInput(err)
	}
	server, err := s.store.UpdateServer(ctx, id, input)
	if err != nil {
		return Server{}, err
	}
	_, _ = s.applyConfig(ctx, server.ID)
	return s.Server(ctx, server.ID)
}

func (s *Service) DeleteServer(ctx context.Context, id string) error {
	server, err := s.store.GetServer(ctx, id)
	if err == nil {
		if process, pErr := s.store.GetProcess(ctx, id); pErr == nil {
			_ = s.runtime.Stop(ctx, server, process)
			_ = s.store.DeleteProcess(ctx, id)
		}
	}
	return s.store.DeleteServer(ctx, id)
}

func (s *Service) Rules(ctx context.Context, serverID string) ([]ProxyRule, error) {
	if _, err := s.store.GetServer(ctx, serverID); err != nil {
		return nil, err
	}
	rules, err := s.store.ListRules(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return maskRuleSecrets(rules), nil
}

func (s *Service) CreateRule(ctx context.Context, serverID string, input ProxyRuleInput) (ProxyRule, error) {
	if _, err := s.store.GetServer(ctx, serverID); err != nil {
		return ProxyRule{}, err
	}
	input = normalizeRuleDefaults(input)
	if err := validateRule(input); err != nil {
		return ProxyRule{}, invalidInput(err)
	}
	rule, err := s.store.CreateRule(ctx, serverID, input)
	if err != nil {
		return ProxyRule{}, err
	}
	_ = s.syncProxyConfig(ctx, serverID)
	return maskRuleSecret(rule), nil
}

func (s *Service) UpdateRule(ctx context.Context, serverID, ruleID string, input ProxyRuleInput) (ProxyRule, error) {
	if _, err := s.store.GetServer(ctx, serverID); err != nil {
		return ProxyRule{}, err
	}
	current, err := s.store.GetRule(ctx, serverID, ruleID)
	if err != nil {
		return ProxyRule{}, err
	}
	input = normalizeRuleDefaults(input)
	if input.SecretKey == "" || LooksMaskedSecret(input.SecretKey) {
		input.SecretKey = current.SecretKey
	}
	if input.HTTPPassword == "" || LooksMaskedSecret(input.HTTPPassword) {
		input.HTTPPassword = current.HTTPPassword
	}
	if err := validateRule(input); err != nil {
		return ProxyRule{}, invalidInput(err)
	}
	rule, err := s.store.UpdateRule(ctx, serverID, ruleID, input)
	if err != nil {
		return ProxyRule{}, err
	}
	_ = s.syncProxyConfig(ctx, serverID)
	return maskRuleSecret(rule), nil
}

func (s *Service) DeleteRule(ctx context.Context, serverID, ruleID string) error {
	if err := s.store.DeleteRule(ctx, serverID, ruleID); err != nil {
		return err
	}
	_ = s.syncProxyConfig(ctx, serverID)
	return nil
}

func (s *Service) Start(ctx context.Context, serverID string) ActionResult {
	s.resetRestartAttempts(serverID)
	return s.start(ctx, serverID, true)
}

func (s *Service) start(ctx context.Context, serverID string, resetAttemptsOnSuccess bool) ActionResult {
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return errorResult(err)
	}
	if process, err := s.store.GetProcess(ctx, serverID); err == nil && s.runtime.ProcessAlive(ctx, process.PID) {
		return ActionResult{OK: true, Message: "frpc already running", Output: fmt.Sprintf("pid=%d", process.PID)}
	}
	_ = s.store.DeleteProcess(ctx, serverID)

	version, result := s.requireActiveVersion(ctx)
	if !result.OK {
		return result
	}
	if _, err := s.applyConfig(ctx, serverID); err != nil {
		return errorResult(err)
	}
	check := s.runtime.CheckConfig(ctx, server, version)
	if !check.OK {
		_ = s.store.AddHealth(ctx, serverID, "warning", check.Message)
		return check
	}

	_ = s.store.SetServerStatus(ctx, serverID, "starting")
	info, result := s.runtime.Start(ctx, server, version)
	if result.OK {
		if resetAttemptsOnSuccess {
			s.resetRestartAttempts(serverID)
		}
		_ = s.store.UpsertProcess(ctx, info)
		_ = s.store.SetServerStatus(ctx, serverID, "running")
		return result
	}
	_ = s.store.SetServerStatus(ctx, serverID, "error")
	_ = s.store.AddHealth(ctx, serverID, "warning", result.Message)
	return result
}

func (s *Service) Stop(ctx context.Context, serverID string) ActionResult {
	s.resetRestartAttempts(serverID)
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return errorResult(err)
	}
	process, err := s.store.GetProcess(ctx, serverID)
	if err != nil {
		_ = s.store.SetServerStatus(ctx, serverID, "stopped")
		return ActionResult{OK: true, Message: "server marked as stopped"}
	}
	result := s.runtime.Stop(ctx, server, process)
	if result.OK {
		_ = s.store.DeleteProcess(ctx, serverID)
		_ = s.store.SetServerStatus(ctx, serverID, "stopped")
		return result
	}
	_ = s.store.AddHealth(ctx, serverID, "warning", result.Message)
	return result
}

func (s *Service) Restart(ctx context.Context, serverID string) ActionResult {
	stop := s.Stop(ctx, serverID)
	if !stop.OK {
		return stop
	}
	return s.Start(ctx, serverID)
}

func (s *Service) Reload(ctx context.Context, serverID string) ActionResult {
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return errorResult(err)
	}
	if server.RestartRequired {
		return ActionResult{OK: false, Message: "公共配置已变更，需要重启后生效"}
	}
	version, result := s.requireActiveVersion(ctx)
	if !result.OK {
		return result
	}
	if _, err := s.applyConfig(ctx, serverID); err != nil {
		return errorResult(err)
	}
	check := s.runtime.CheckConfig(ctx, server, version)
	if !check.OK {
		_ = s.store.AddHealth(ctx, serverID, "warning", check.Message)
		return check
	}
	result = s.runtime.Reload(ctx, server, version)
	if result.OK {
		_ = s.store.MarkReloaded(ctx, serverID)
		return result
	}
	_ = s.store.AddHealth(ctx, serverID, "warning", result.Message)
	return result
}

func (s *Service) Check(ctx context.Context, serverID string) ActionResult {
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return errorResult(err)
	}
	version, result := s.requireActiveVersion(ctx)
	if !result.OK {
		return result
	}
	if _, err := s.applyConfig(ctx, serverID); err != nil {
		return errorResult(err)
	}
	return s.runtime.CheckConfig(ctx, server, version)
}

func (s *Service) ConfigPreview(ctx context.Context, serverID string) (ConfigPreview, error) {
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return ConfigPreview{}, err
	}
	return s.runtime.RenderConfig(ctx, server)
}

// applyConfig 重新渲染 frpc.toml 并落盘，使数据库中的配置生效到文件系统。
func (s *Service) applyConfig(ctx context.Context, serverID string) (ConfigPreview, error) {
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return ConfigPreview{}, err
	}
	return s.runtime.RenderConfig(ctx, server)
}

func (s *Service) Logs(ctx context.Context, serverID string, tail int) ([]LogLine, error) {
	if _, err := s.store.GetServer(ctx, serverID); err != nil {
		return nil, err
	}
	if tail <= 0 {
		tail = 200
	}
	return s.runtime.Logs(ctx, serverID, tail)
}

func (s *Service) Versions(ctx context.Context) ([]FRPCVersion, error) {
	return s.store.ListVersions(ctx)
}

func (s *Service) ActivateVersion(ctx context.Context, id string) (FRPCVersion, error) {
	version, err := s.store.GetVersion(ctx, id)
	if err != nil {
		return FRPCVersion{}, err
	}
	if err := s.store.SetActiveVersion(ctx, id); err != nil {
		return FRPCVersion{}, err
	}
	version.Active = true
	return version, nil
}

func (s *Service) InstallOnline(ctx context.Context, input FRPCInstallOnlineInput) (FRPCVersion, error) {
	if strings.TrimSpace(input.GithubProxy) == "" {
		settings, err := s.Settings(ctx)
		if err != nil {
			return FRPCVersion{}, err
		}
		input.GithubProxy = settings.GithubProxy
	}
	version, err := s.runtime.InstallOnline(ctx, input)
	if err != nil {
		return FRPCVersion{}, err
	}
	version.Active = true
	return s.store.AddVersion(ctx, version)
}

func (s *Service) CheckLatest(ctx context.Context, input LatestVersionInput) (LatestVersionResult, error) {
	githubProxy := strings.TrimSpace(input.GithubProxy)
	if githubProxy == "" {
		settings, err := s.Settings(ctx)
		if err != nil {
			return LatestVersionResult{}, err
		}
		githubProxy = settings.GithubProxy
	}
	latest, err := s.runtime.LatestVersion(ctx, githubProxy)
	if err != nil {
		return LatestVersionResult{}, err
	}
	return LatestVersionResult{Latest: latest}, nil
}

func (s *Service) InstallOffline(ctx context.Context, filename string, file io.Reader) (FRPCVersion, error) {
	version, err := s.runtime.InstallOffline(ctx, filename, file)
	if err != nil {
		return FRPCVersion{}, err
	}
	version.Active = true
	return s.store.AddVersion(ctx, version)
}

// ExportConfig 导出完整配置备份（含敏感字段）。备份文件由用户自行保管，
// 不做脱敏——脱敏后的备份无法完整恢复。
func (s *Service) ExportConfig(ctx context.Context) (ConfigBundle, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	versions, err := s.store.ListVersions(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	bundle := ConfigBundle{
		Version:          1,
		ExportedAt:       time.Now().Format(time.RFC3339),
		IncludeSensitive: true,
		GithubProxy:      settings.GithubProxy,
		Versions:         versions,
		Servers:          make([]ServerBundle, 0, len(servers)),
	}
	for _, server := range servers {
		bundle.Servers = append(bundle.Servers, ServerBundle{
			Server: server,
			Rules:  server.Rules,
		})
	}
	return bundle, nil
}

func (s *Service) ImportConfig(ctx context.Context, input ConfigImportInput) (ActionResult, error) {
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" {
		return ActionResult{}, invalidInput(errors.New("import mode must be merge or replace"))
	}
	if input.Bundle.Version == 0 {
		return ActionResult{}, invalidInput(errors.New("config bundle version is required"))
	}

	// 先完整校验整个 bundle，再做任何破坏性操作，避免无效备份清空现有配置。
	type importEntry struct {
		name   string
		server ServerInput
		rules  []ProxyRuleInput
	}
	entries := make([]importEntry, 0, len(input.Bundle.Servers))
	for _, item := range input.Bundle.Servers {
		serverInput := importServerInput(item.Server)
		if err := validateServer(serverInput); err != nil {
			return ActionResult{}, invalidInput(fmt.Errorf("server %q: %w", item.Server.Name, err))
		}
		entry := importEntry{name: item.Server.Name, server: serverInput, rules: make([]ProxyRuleInput, 0, len(item.Rules))}
		for _, rule := range item.Rules {
			ruleInput := importRuleInput(rule)
			if err := validateRule(ruleInput); err != nil {
				return ActionResult{}, invalidInput(fmt.Errorf("rule %q: %w", rule.Name, err))
			}
			entry.rules = append(entry.rules, ruleInput)
		}
		entries = append(entries, entry)
	}

	if mode == "replace" {
		servers, err := s.store.ListServers(ctx)
		if err != nil {
			return ActionResult{}, err
		}
		for _, server := range servers {
			if err := s.DeleteServer(ctx, server.ID); err != nil {
				return ActionResult{}, err
			}
		}
	}
	if err := s.store.SetSetting(ctx, "github_proxy", strings.TrimSpace(input.Bundle.GithubProxy)); err != nil {
		return ActionResult{}, err
	}
	for _, version := range input.Bundle.Versions {
		if strings.TrimSpace(version.Version) == "" || strings.TrimSpace(version.Path) == "" {
			continue
		}
		_, _ = s.store.AddVersion(ctx, version)
	}
	createdServers := 0
	createdRules := 0
	for _, entry := range entries {
		server, err := s.store.CreateServer(ctx, entry.server)
		if err != nil {
			return ActionResult{}, err
		}
		createdServers++
		for _, ruleInput := range entry.rules {
			if _, err := s.store.CreateRule(ctx, server.ID, ruleInput); err != nil {
				return ActionResult{}, err
			}
			createdRules++
		}
		_, _ = s.applyConfig(ctx, server.ID)
	}
	return ActionResult{
		OK:      true,
		Message: fmt.Sprintf("导入完成：%d 个服务器，%d 条规则", createdServers, createdRules),
	}, nil
}

func (s *Service) CurrentVersion(ctx context.Context) FRPCVersion {
	return s.currentVersion(ctx)
}

func (s *Service) currentVersion(ctx context.Context) FRPCVersion {
	version, err := s.store.ActiveVersion(ctx)
	if err == nil {
		return version
	}
	return FRPCVersion{Installed: false, Version: "-", Latest: "-", Path: ""}
}

// requireActiveVersion 返回当前激活的 frpc 版本，未安装时给出可操作的错误结果。
func (s *Service) requireActiveVersion(ctx context.Context) (FRPCVersion, ActionResult) {
	version, err := s.store.ActiveVersion(ctx)
	if err != nil || !version.Installed || version.Path == "" {
		return FRPCVersion{}, ActionResult{OK: false, Message: "frpc is not installed"}
	}
	return version, ActionResult{OK: true, Message: "frpc version selected"}
}

func (s *Service) syncProxyConfig(ctx context.Context, serverID string) ActionResult {
	server, err := s.store.GetServer(ctx, serverID)
	if err != nil {
		return errorResult(err)
	}
	if _, err := s.applyConfig(ctx, serverID); err != nil {
		return errorResult(err)
	}
	if !isRunningState(server.Status) {
		return ActionResult{OK: true, Message: "configuration rendered"}
	}
	if server.RestartRequired {
		return ActionResult{OK: true, Message: "configuration rendered; restart required"}
	}
	result := s.Reload(ctx, serverID)
	if !result.OK {
		_ = s.store.AddHealth(ctx, serverID, "warning", result.Message)
	}
	return result
}

func (s *Service) withRuntimeFields(ctx context.Context, server Server) Server {
	if isRunningState(server.Status) {
		process, err := s.store.GetProcess(ctx, server.ID)
		if err != nil {
			if s.restartPending(server.ID) {
				// 自动重启退避中本来就没有进程记录，保持 starting 状态即可。
				server.Uptime = "-"
			} else {
				server.Status = "error"
				server.Uptime = "-"
				_ = s.store.SetServerStatus(ctx, server.ID, "error")
				_ = s.store.AddHealth(ctx, server.ID, "warning", "运行状态缺少进程记录")
			}
		} else if !s.runtime.ProcessAlive(ctx, process.PID) {
			server.Status = "error"
			server.Uptime = "-"
			_ = s.store.DeleteProcess(ctx, server.ID)
			_ = s.store.SetServerStatus(ctx, server.ID, "error")
			_ = s.store.AddHealth(ctx, server.ID, "warning", "frpc 进程已退出")
		} else {
			server.Uptime = durationText(process.StartedAt)
		}
	} else {
		server.Uptime = "-"
	}
	if server.LastReloadAt == "" {
		server.LastReloadAt = "-"
	}
	if server.AuthToken != "" {
		server.AuthToken = maskSecret(server.AuthToken)
	}
	if server.AdminPassword != "" {
		server.AdminPassword = maskSecret(server.AdminPassword)
	}
	server.Rules = maskRuleSecrets(server.Rules)
	return server
}

func validateServer(input ServerInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("server name is required")
	}
	if strings.TrimSpace(input.ServerAddr) == "" {
		return errors.New("server address is required")
	}
	if input.ServerPort < 1 || input.ServerPort > 65535 {
		return errors.New("server port must be between 1 and 65535")
	}
	if input.AdminPort != 0 && (input.AdminPort < 1 || input.AdminPort > 65535) {
		return errors.New("admin port must be between 1 and 65535")
	}
	switch input.TransportProtocol {
	case "", "tcp", "kcp", "quic", "websocket":
	default:
		return errors.New("transport protocol must be tcp, kcp, quic, or websocket")
	}
	if input.MaxRestarts < 1 || input.MaxRestarts > 10 {
		return errors.New("max restarts must be between 1 and 10")
	}
	return nil
}

func validateRule(input ProxyRuleInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return errors.New("rule name is required")
	}
	if input.Type != "tcp" && input.Type != "udp" && input.Type != "http" && input.Type != "https" && input.Type != "stcp" && input.Type != "xtcp" {
		return errors.New("rule type must be tcp, udp, http, https, stcp, or xtcp")
	}
	if input.Type == "tcp" || input.Type == "udp" {
		if input.LocalPort < 1 || input.LocalPort > 65535 {
			return errors.New("local port must be between 1 and 65535")
		}
		if input.RemotePort < 1 || input.RemotePort > 65535 {
			return errors.New("remote port is required for tcp and udp")
		}
	}
	if input.Type == "http" || input.Type == "https" {
		if input.LocalPort < 1 || input.LocalPort > 65535 {
			return errors.New("local port must be between 1 and 65535")
		}
		if len(input.CustomDomains) == 0 {
			return errors.New("custom domain is required for http and https")
		}
		for _, header := range input.RequestHeaders {
			name, _, ok := strings.Cut(header, ":")
			if !ok {
				name, _, ok = strings.Cut(header, "=")
			}
			if !ok {
				return errors.New("request header must be in 'Name: Value' format")
			}
			if !IsValidHeaderName(strings.TrimSpace(name)) {
				return errors.New("request header name must contain only letters, digits, '-' or '_'")
			}
		}
	}
	if input.Type == "stcp" || input.Type == "xtcp" {
		switch input.Role {
		case "", "server":
			if strings.TrimSpace(input.SecretKey) == "" {
				return errors.New("secret key is required for stcp/xtcp server rules")
			}
			if input.LocalPort < 1 || input.LocalPort > 65535 {
				return errors.New("local port must be between 1 and 65535")
			}
		case "visitor":
			if strings.TrimSpace(input.SecretKey) == "" {
				return errors.New("secret key is required for stcp/xtcp visitor rules")
			}
			if strings.TrimSpace(input.ServerName) == "" {
				return errors.New("server name is required for stcp/xtcp visitors")
			}
			if input.BindPort < 1 || input.BindPort > 65535 {
				return errors.New("bind port is required for stcp/xtcp visitors")
			}
		default:
			return errors.New("stcp/xtcp role must be server or visitor")
		}
	}
	return nil
}

func normalizeServerDefaults(input ServerInput) ServerInput {
	input.Name = strings.TrimSpace(input.Name)
	input.ServerAddr = strings.TrimSpace(input.ServerAddr)
	input.AuthToken = strings.TrimSpace(input.AuthToken)
	input.TransportProtocol = strings.TrimSpace(input.TransportProtocol)
	if input.ServerPort == 0 {
		input.ServerPort = 7000
	}
	if input.TransportProtocol == "" {
		input.TransportProtocol = "tcp"
	}
	if input.MaxRestarts <= 0 {
		input.MaxRestarts = 3
	}
	return input
}

func normalizeRuleDefaults(input ProxyRuleInput) ProxyRuleInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Type = strings.ToLower(strings.TrimSpace(input.Type))
	input.LocalIP = strings.TrimSpace(input.LocalIP)
	input.SecretKey = strings.TrimSpace(input.SecretKey)
	input.Role = strings.ToLower(strings.TrimSpace(input.Role))
	input.ServerName = strings.TrimSpace(input.ServerName)
	input.BindAddr = strings.TrimSpace(input.BindAddr)
	input.BandwidthLimit = strings.TrimSpace(input.BandwidthLimit)
	input.HostHeaderRewrite = strings.TrimSpace(input.HostHeaderRewrite)
	input.HTTPUser = strings.TrimSpace(input.HTTPUser)
	input.HTTPPassword = strings.TrimSpace(input.HTTPPassword)
	input.CustomDomains = cleanStringList(input.CustomDomains)
	input.Locations = cleanStringList(input.Locations)
	input.RequestHeaders = cleanStringList(input.RequestHeaders)
	if input.LocalIP == "" {
		input.LocalIP = "127.0.0.1"
	}
	if input.BindAddr == "" {
		input.BindAddr = "127.0.0.1"
	}
	if (input.Type == "stcp" || input.Type == "xtcp") && input.Role == "" {
		input.Role = "server"
	}
	return input
}

// IsValidHeaderName reports whether name is a safe HTTP header token
// (RFC 7230 token subset) that can be embedded into a TOML key without quoting.
func IsValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
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

func maskRuleSecrets(rules []ProxyRule) []ProxyRule {
	if len(rules) == 0 {
		return rules
	}
	out := make([]ProxyRule, len(rules))
	copy(out, rules)
	for i := range out {
		if out[i].SecretKey != "" {
			out[i].SecretKey = maskSecret(out[i].SecretKey)
		}
		if out[i].HTTPPassword != "" {
			out[i].HTTPPassword = maskSecret(out[i].HTTPPassword)
		}
	}
	return out
}

func maskRuleSecret(rule ProxyRule) ProxyRule {
	if rule.SecretKey != "" {
		rule.SecretKey = maskSecret(rule.SecretKey)
	}
	if rule.HTTPPassword != "" {
		rule.HTTPPassword = maskSecret(rule.HTTPPassword)
	}
	return rule
}

func importServerInput(server Server) ServerInput {
	input := ServerInput{
		Name:              server.Name,
		ServerAddr:        server.ServerAddr,
		ServerPort:        server.ServerPort,
		AuthToken:         server.AuthToken,
		TransportProtocol: server.TransportProtocol,
		AutoStart:         server.AutoStart,
		AutoRestart:       server.AutoRestart,
		MaxRestarts:       server.MaxRestarts,
		AdminPort:         server.AdminPort,
		AdminUser:         server.AdminUser,
		AdminPassword:     server.AdminPassword,
	}
	if LooksMaskedSecret(input.AuthToken) {
		input.AuthToken = ""
	}
	if LooksMaskedSecret(input.AdminPassword) {
		input.AdminPassword = ""
	}
	return normalizeServerDefaults(input)
}

func importRuleInput(rule ProxyRule) ProxyRuleInput {
	input := ProxyRuleInput{
		Name:              rule.Name,
		Type:              rule.Type,
		LocalIP:           rule.LocalIP,
		LocalPort:         rule.LocalPort,
		RemotePort:        rule.RemotePort,
		CustomDomains:     rule.CustomDomains,
		Enabled:           rule.Enabled,
		SecretKey:         rule.SecretKey,
		Role:              rule.Role,
		ServerName:        rule.ServerName,
		BindAddr:          rule.BindAddr,
		BindPort:          rule.BindPort,
		UseEncryption:     rule.UseEncryption,
		UseCompression:    rule.UseCompression,
		BandwidthLimit:    rule.BandwidthLimit,
		Locations:         rule.Locations,
		HostHeaderRewrite: rule.HostHeaderRewrite,
		HTTPUser:          rule.HTTPUser,
		HTTPPassword:      rule.HTTPPassword,
		RequestHeaders:    rule.RequestHeaders,
	}
	if LooksMaskedSecret(input.SecretKey) {
		input.SecretKey = ""
	}
	if LooksMaskedSecret(input.HTTPPassword) {
		input.HTTPPassword = ""
	}
	return normalizeRuleDefaults(input)
}

// maskSecret 返回定长掩码，避免泄露密钥长度；保留首尾各 2 字符便于辨认。
func maskSecret(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:2] + "****" + value[len(value)-2:]
}

// LooksMaskedSecret 判断一个值是否是 maskSecret 输出的掩码（而非真实密钥），
// 是 API 层与 TOML 渲染共用的唯一判定标准。
func LooksMaskedSecret(value string) bool {
	return strings.Count(value, "*") >= 4
}

func durationText(startedAt string) string {
	started, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return startedAt
	}
	d := time.Since(started)
	if d < time.Minute {
		return d.Truncate(time.Second).String()
	}
	return d.Truncate(time.Minute).String()
}

func (s *Service) resetRestartAttempts(serverID string) {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()
	delete(s.restartAttempts, serverID)
	if timer := s.restartTimers[serverID]; timer != nil {
		timer.Stop()
	}
	delete(s.restartTimers, serverID)
}

// restartPending 报告该服务器是否处于自动重启的退避等待中。
func (s *Service) restartPending(serverID string) bool {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()
	return s.restartTimers[serverID] != nil
}

// stableRunDuration 是进程被视为“稳定运行”的最短时长；
// 稳定运行后的崩溃不累计到自动重启次数上。
const stableRunDuration = 5 * time.Minute

func restartBackoff(attempt int) time.Duration {
	if attempt <= 1 {
		return 5 * time.Second
	}
	delay := time.Duration(1<<(attempt-1)) * 5 * time.Second
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func exitErrorText(err error) string {
	if err == nil {
		return "process exited"
	}
	return err.Error()
}

func durationTextShort(value time.Duration) string {
	if value%time.Second == 0 {
		return fmt.Sprintf("%ds", int(value/time.Second))
	}
	return value.String()
}

func isRunningState(status string) bool {
	return status == "running" || status == "config_dirty" || status == "starting" || status == "reloading"
}

func errorResult(err error) ActionResult {
	if errors.Is(err, ErrNotFound) {
		return ActionResult{OK: false, Message: "resource not found"}
	}
	return ActionResult{OK: false, Message: err.Error()}
}

func invalidInput(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidInput, err)
}

func (s *Service) accessKeyConfigured(ctx context.Context) bool {
	if strings.TrimSpace(os.Getenv("FRPC_WEB_ACCESS_KEY")) != "" {
		return true
	}
	stored, err := s.store.GetSetting(ctx, "access_key_hash")
	return err == nil && strings.TrimSpace(stored) != ""
}

func (s *Service) verifyAccessKey(ctx context.Context, accessKey string) error {
	if envKey := strings.TrimSpace(os.Getenv("FRPC_WEB_ACCESS_KEY")); envKey != "" {
		if subtleEqual(envKey, accessKey) {
			return nil
		}
		return ErrInvalidCredentials
	}
	stored, err := s.store.GetSetting(ctx, "access_key_hash")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrBootstrapRequired
		}
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(accessKey)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// SessionTTL 是会话的有效期，Cookie MaxAge 与服务端过期时间共用此值。
const SessionTTL = 12 * time.Hour

func (s *Service) newSession(ctx context.Context, meta AuthMeta) (Session, error) {
	token, err := randomHex(32)
	if err != nil {
		return Session{}, err
	}
	now := time.Now()
	session := Session{
		IDHash:       hashSessionID(token),
		Token:        token,
		IP:           strings.TrimSpace(meta.IP),
		UserAgent:    strings.TrimSpace(meta.UserAgent),
		CreatedAt:    now.Format(time.RFC3339),
		LastAccessAt: now.Format(time.RFC3339),
		ExpiresAt:    now.Add(SessionTTL).Format(time.RFC3339),
	}
	session, err = s.store.CreateSession(ctx, session)
	if err != nil {
		return Session{}, err
	}
	session.Token = token
	return session, nil
}

func normalizeAccessKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 256 {
		return "", errors.New("access key must be 8-256 characters")
	}
	return value, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashSessionID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sessionExpired(expiresAt string) bool {
	expires, err := time.Parse(time.RFC3339, expiresAt)
	return err != nil || time.Now().After(expires)
}

func subtleEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

