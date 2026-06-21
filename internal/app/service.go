package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Store 是 service 依赖的持久化接口（实现见 internal/storage）。
// v2.0 仅持久化面板自身数据：设置、会话、健康事件、审计日志；
// server 列表不再持久化，由 ConfigScanner 扫描磁盘配置文件得到。
type Store interface {
	DataDir() string
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
	CreateSession(ctx context.Context, session Session) (Session, error)
	GetSessionByHash(ctx context.Context, idHash string) (Session, error)
	TouchSession(ctx context.Context, idHash string) error
	RevokeSessionByHash(ctx context.Context, idHash string) error
	RevokeAllSessions(ctx context.Context) error
	ListHealth(ctx context.Context) ([]HealthEvent, error)
	AddHealth(ctx context.Context, serverID, level, message string) error
	AddAudit(ctx context.Context, input AuditLogInput) error
	ListAuditLogs(ctx context.Context, query AuditLogQuery) (AuditLogPage, error)
	ClearAuditLogs(ctx context.Context) error
}

// Runtime 是 service 依赖的 frpc 运行时能力（实现见 internal/frpc）。
// v2.0 只保留只读能力：读日志、查 admin API 状态、触发 admin API 热重载。
type Runtime interface {
	Logs(ctx context.Context, logPath string, tail int) ([]LogLine, error)
	ProxyStatus(ctx context.Context, server Server) ([]ProxyStatus, error)
	Reload(ctx context.Context, server Server) error
}

var (
	ErrInvalidInput           = errors.New("invalid input")
	ErrInvalidCredentials     = errors.New("invalid access key")
	ErrUnauthorized           = errors.New("unauthorized")
	ErrPasswordChangeRequired = errors.New("password change required")
	ErrNotFound               = errors.New("resource not found")
)

// DefaultAccessKey 是出厂初始访问密钥。首次以它登录后会被强制要求改密；
// 一旦设置了自己的密码，它立即失效。可用环境变量 FRPC_WEB_ACCESS_KEY
// 覆盖为自定义初始密钥（同样是“初始/一次性”语义，设密后即失效）。
const DefaultAccessKey = "FrpcWeb-Init-9527"

type Options struct {
	Store   Store
	Runtime Runtime
	Addr    string
	Version string
}

type Service struct {
	store   Store
	runtime Runtime
	scanner *ConfigScanner
	addr    string
	version string
}

func NewService(opts Options) *Service {
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "dev"
	}
	scanner := NewConfigScanner(opts.Store.DataDir(), opts.Runtime)
	return &Service{
		store:   opts.Store,
		runtime: opts.Runtime,
		scanner: scanner,
		addr:    opts.Addr,
		version: version,
	}
}

// StartScanner 启动后台配置扫描与状态探测循环，阻塞至 ctx 取消。
func (s *Service) StartScanner(ctx context.Context) {
	s.scanner.Run(ctx)
}

// RefreshScan 触发一次配置扫描与状态探测（供测试与「保存后立即刷新」使用）。
func (s *Service) RefreshScan(ctx context.Context) {
	s.scanner.RefreshNow(ctx)
}

// ——— 认证 ———

func (s *Service) AuthStatus(ctx context.Context) (AuthStatus, error) {
	// 系统始终存在初始密钥（出厂默认或 env 覆盖），是否需要强制改密取决于
	// 用户是否已设置自己的密码。
	return AuthStatus{
		MustChangePassword: !s.hasUserPassword(ctx),
	}, nil
}

// RequiresPasswordChange 报告系统当前是否仍在使用初始密钥（尚未设置用户密码）。
// 中间件据此把仍持“初始密钥会话”的请求限制为只能改密。
func (s *Service) RequiresPasswordChange(ctx context.Context) bool {
	return !s.hasUserPassword(ctx)
}

func (s *Service) Login(ctx context.Context, input AuthInput, meta AuthMeta) (Session, error) {
	accessKey := strings.TrimSpace(input.AccessKey)
	if accessKey == "" {
		return Session{}, ErrInvalidCredentials
	}
	// 已设置用户密码：只认存储的 bcrypt 哈希，初始密钥（默认/env）此后一律失效。
	if s.hasUserPassword(ctx) {
		if err := s.verifyUserPassword(ctx, accessKey); err != nil {
			return Session{}, ErrInvalidCredentials
		}
		return s.newSession(ctx, meta)
	}
	// 尚未设置用户密码：校验初始密钥；成功后 AuthStatus.MustChangePassword 仍为 true，
	// 由中间件强制其在改密前不能调用其他业务接口。
	if subtleEqual(initialAccessKey(), accessKey) {
		return s.newSession(ctx, meta)
	}
	return Session{}, ErrInvalidCredentials
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
	next, err := validateNewPassword(input.NewAccessKey)
	if err != nil {
		return invalidInput(err)
	}
	// 已设置用户密码时，常规改密需校验当前密码；首次设置（仍是初始密钥）则
	// 由有效会话本身作为凭证——调用方此时已用初始密钥登录并持有会话。
	if s.hasUserPassword(ctx) {
		if err := s.verifyUserPassword(ctx, strings.TrimSpace(input.CurrentAccessKey)); err != nil {
			return ErrInvalidCredentials
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, "access_key_hash", string(hash)); err != nil {
		return err
	}
	// 改密后吊销所有会话：初始密钥/旧密码连同其会话立即失效，用户用新密码重新登录。
	return s.store.RevokeAllSessions(ctx)
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

// validateNewPassword 校验用户自设的新密码策略：8-20 位，且同时包含大写字母、
// 小写字母、数字，仅允许字母与数字。初始密钥（默认/env）不受此策略约束。
func validateNewPassword(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < 8 || len(value) > 20 {
		return "", errors.New("密码长度需为 8-20 位")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			return "", errors.New("密码只能包含字母和数字")
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return "", errors.New("密码必须同时包含大写字母、小写字母和数字")
	}
	return value, nil
}

// initialAccessKey 返回当前生效的初始密钥：env 覆盖优先，否则用出厂默认。
// 仅在用户尚未设置自己的密码时用于登录校验，设密后即失效。
func initialAccessKey() string {
	if v := strings.TrimSpace(os.Getenv("FRPC_WEB_ACCESS_KEY")); v != "" {
		return v
	}
	return DefaultAccessKey
}

// hasUserPassword 报告用户是否已设置自己的密码（即已脱离初始密钥）。
func (s *Service) hasUserPassword(ctx context.Context) bool {
	stored, err := s.store.GetSetting(ctx, "access_key_hash")
	return err == nil && strings.TrimSpace(stored) != ""
}

// verifyUserPassword 用 bcrypt 校验已设置的用户密码。
func (s *Service) verifyUserPassword(ctx context.Context, accessKey string) error {
	stored, err := s.store.GetSetting(ctx, "access_key_hash")
	if err != nil || strings.TrimSpace(stored) == "" {
		return ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(accessKey)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
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

// ——— 审计 ———

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

// ——— 设置 ———

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

// ——— 配置监控（只读 server 列表来自 ConfigScanner） ———

func (s *Service) Servers(ctx context.Context) ([]Server, error) {
	servers := s.scanner.Servers()
	for i := range servers {
		servers[i] = s.maskServer(servers[i])
	}
	return servers, nil
}

func (s *Service) Server(ctx context.Context, id string) (Server, error) {
	server, ok := s.scanner.Server(id)
	if !ok {
		return Server{}, ErrNotFound
	}
	return s.maskServer(server), nil
}

// maskServer 对敏感字段做掩码（展示用）。配置原文编辑接口不受此影响。
func (s *Service) maskServer(server Server) Server {
	if server.AuthToken != "" {
		server.AuthToken = maskSecret(server.AuthToken)
	}
	if server.AdminPassword != "" {
		server.AdminPassword = maskSecret(server.AdminPassword)
	}
	for i := range server.Rules {
		if server.Rules[i].SecretKey != "" {
			server.Rules[i].SecretKey = maskSecret(server.Rules[i].SecretKey)
		}
		if server.Rules[i].HTTPPassword != "" {
			server.Rules[i].HTTPPassword = maskSecret(server.Rules[i].HTTPPassword)
		}
	}
	return server
}

// maskSecret 返回定长掩码，避免泄露密钥长度；保留首尾各 2 字符便于辨认。
func maskSecret(value string) string {
	if len(value) <= 8 {
		return "****"
	}
	return value[:2] + "****" + value[len(value)-2:]
}

// ——— 配置文件读写与热重载 ———

// ConfigFiles 列出扫描到的配置文件元信息。
func (s *Service) ConfigFiles(ctx context.Context) ([]ConfigFile, error) {
	servers := s.scanner.Servers()
	files := make([]ConfigFile, 0, len(servers))
	for _, server := range servers {
		files = append(files, ConfigFile{
			Path:     server.ConfigPath,
			Writable: server.Writable,
			IsToml:   isTomlFile(server.ConfigPath),
		})
	}
	return files, nil
}

// ReadConfigFile 读取指定 server 配置文件原文。
func (s *Service) ReadConfigFile(ctx context.Context, id string) (ConfigFile, error) {
	server, ok := s.scanner.Server(id)
	if !ok {
		return ConfigFile{}, ErrNotFound
	}
	content, err := os.ReadFile(server.ConfigPath)
	if err != nil {
		return ConfigFile{}, err
	}
	return ConfigFile{
		Path:     server.ConfigPath,
		Content:  string(content),
		Writable: server.Writable,
		IsToml:   isTomlFile(server.ConfigPath),
	}, nil
}

// SaveConfigFile 原子写入配置文件原文，随后触发重新扫描。
func (s *Service) SaveConfigFile(ctx context.Context, id, content string) (ActionResult, error) {
	server, ok := s.scanner.Server(id)
	if !ok {
		return ActionResult{}, ErrNotFound
	}
	if !server.Writable {
		return ActionResult{OK: false, Message: "配置文件不可写：frpc-web 进程对该文件无写权限。请在部署时开启可写，或手动编辑文件。"}, nil
	}
	if err := atomicWriteFile(server.ConfigPath, []byte(content), 0o600); err != nil {
		return ActionResult{}, err
	}
	s.scanner.RefreshNow(ctx)
	return ActionResult{OK: true, Message: "配置已保存。点击「热重载」让 frpc 重读配置，或重启 frpc 服务。"}, nil
}

// ReloadViaAdmin 通过 frpc admin API 触发热重载（frpc 重读其启动时的配置文件）。
func (s *Service) ReloadViaAdmin(ctx context.Context, id string) ActionResult {
	server, ok := s.scanner.Server(id)
	if !ok {
		return ActionResult{OK: false, Message: "未找到该配置对应的实例"}
	}
	if server.AdminPort == 0 {
		return ActionResult{OK: false, Message: "该配置未启用 admin API（webServer 段），无法热重载。请手动重启 frpc 服务。"}
	}
	callCtx, cancel := context.WithTimeout(ctx, configStatusTimeout)
	defer cancel()
	if err := s.runtime.Reload(callCtx, server); err != nil {
		return ActionResult{OK: false, Message: err.Error()}
	}
	return ActionResult{OK: true, Message: "已请求 frpc 热重载配置"}
}

func isTomlFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".toml")
}

// atomicWriteFile 写临时文件后 rename，避免崩溃时留下半写文件。
// 若目标已存在且是符号链接则拒绝，防止通过扫描目录内的链接把内容写到任意位置。
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("refusing to write: target is a symbolic link")
		}
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".frpc-web-cfg-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

// ——— 配置导出/导入 ———

// ExportConfig 导出所有扫描到的配置文件原文（含明文密钥，用户自行保管）。
func (s *Service) ExportConfig(ctx context.Context) (ConfigBundle, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return ConfigBundle{}, err
	}
	servers := s.scanner.Servers()
	files := make([]ConfigFile, 0, len(servers))
	for _, server := range servers {
		content, err := os.ReadFile(server.ConfigPath)
		if err != nil {
			continue
		}
		files = append(files, ConfigFile{
			Path:    server.ConfigPath,
			Content: string(content),
		})
	}
	return ConfigBundle{
		Version:     1,
		ExportedAt:  time.Now().Format(time.RFC3339),
		Files:       files,
		GithubProxy: settings.GithubProxy,
	}, nil
}

// ImportConfig 把 bundle 里每个配置文件原文写回其路径，仅允许写到扫描路径范围内。
// 不可写或越界的跳过并报告。
func (s *Service) ImportConfig(ctx context.Context, input ConfigImportInput) (ActionResult, error) {
	if input.Bundle.Version == 0 {
		return ActionResult{}, invalidInput(errors.New("config bundle version is required"))
	}
	written, skipped := 0, 0
	for _, file := range input.Bundle.Files {
		path := strings.TrimSpace(file.Path)
		if path == "" || !isConfigFileName(path) {
			// 只允许写配置文件（.toml/.ini），杜绝把 state.json 或任意文件覆盖掉。
			skipped++
			continue
		}
		if !withinScanPaths(s.scanner.Paths(), path) {
			skipped++
			continue
		}
		if err := atomicWriteFile(path, []byte(file.Content), 0o600); err != nil {
			skipped++
			continue
		}
		written++
	}
	if gp := strings.TrimSpace(input.Bundle.GithubProxy); gp != "" {
		_ = s.store.SetSetting(ctx, "github_proxy", gp)
	}
	s.scanner.RefreshNow(ctx)
	msg := fmt.Sprintf("导入完成：写入 %d 个配置文件", written)
	if skipped > 0 {
		msg += fmt.Sprintf("，跳过 %d 个（路径为空、不在扫描范围内或不可写）", skipped)
	}
	return ActionResult{OK: written > 0, Message: msg}, nil
}

// withinScanPaths 报告 target 是否落在任一扫描路径（目录或文件）之内。
func withinScanPaths(paths []string, target string) bool {
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	for _, p := range paths {
		base, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(base, abs)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// ——— 日志 ———

func (s *Service) Logs(ctx context.Context, id string, tail int) ([]LogLine, error) {
	server, ok := s.scanner.Server(id)
	if !ok {
		return nil, ErrNotFound
	}
	if tail <= 0 {
		tail = 200
	}
	if tail > maxLogTail {
		tail = maxLogTail
	}
	return s.runtime.Logs(ctx, server.LogPath, tail)
}

// maxLogTail 限制单次日志请求返回的行数上限，防止异常大的 tail 参数。
const maxLogTail = 5000

// ——— 辅助 ———

func invalidInput(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidInput, err)
}
