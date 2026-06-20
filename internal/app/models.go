package app

type Dashboard struct {
	Summary     Summary       `json:"summary"`
	Servers     []Server      `json:"servers"`
	Health      []HealthEvent `json:"health"`
	CurrentFRPC FRPCVersion   `json:"currentFrpc"`
	Settings    Settings      `json:"settings"`
}

type Summary struct {
	TotalServers   int `json:"totalServers"`
	RunningServers int `json:"runningServers"`
	ProxyRules     int `json:"proxyRules"`
	OpenEvents     int `json:"openEvents"`
}

type Settings struct {
	Addr                    string `json:"addr"`
	GithubProxy             string `json:"githubProxy"`
	AutoBackupEnabled       bool   `json:"autoBackupEnabled"`
	AutoBackupIntervalHours int    `json:"autoBackupIntervalHours"`
	AutoBackupMaxFiles      int    `json:"autoBackupMaxFiles"`
	LastAutoBackupAt        string `json:"lastAutoBackupAt,omitempty"`
}

// SettingsInput 的自动备份字段用指针区分「未提交」与「显式设置」，
// 旧客户端只更新 githubProxy 时不会把备份设置重置为零值。
type SettingsInput struct {
	GithubProxy             string `json:"githubProxy"`
	AutoBackupEnabled       *bool  `json:"autoBackupEnabled,omitempty"`
	AutoBackupIntervalHours *int   `json:"autoBackupIntervalHours,omitempty"`
	AutoBackupMaxFiles      *int   `json:"autoBackupMaxFiles,omitempty"`
}

type AuthInput struct {
	AccessKey string `json:"accessKey"`
}

type AuthStatus struct {
	Authenticated bool `json:"authenticated"`
	// MustChangePassword 表示当前仍在使用出厂初始密钥，登录后必须先改密。
	// 仅在已认证的响应里置为 true，避免向匿名访问者泄露“仍是默认密钥”。
	MustChangePassword bool `json:"mustChangePassword"`
}

type AuthMeta struct {
	IP        string
	UserAgent string
}

type AccessKeyInput struct {
	CurrentAccessKey string `json:"currentAccessKey"`
	NewAccessKey     string `json:"newAccessKey"`
}

type Session struct {
	ID           string `json:"id"`
	IDHash       string `json:"-"`
	Token        string `json:"-"`
	IP           string `json:"ip"`
	UserAgent    string `json:"userAgent"`
	CreatedAt    string `json:"createdAt"`
	LastAccessAt string `json:"lastAccessAt"`
	ExpiresAt    string `json:"expiresAt"`
}

type AuditLog struct {
	ID           string `json:"id"`
	IP           string `json:"ip"`
	UserAgent    string `json:"userAgent"`
	Action       string `json:"action"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	Result       string `json:"result"`
	Error        string `json:"error,omitempty"`
	CreatedAt    string `json:"createdAt"`
}

type AuditLogInput struct {
	IP           string
	UserAgent    string
	Action       string
	ResourceType string
	ResourceID   string
	Result       string
	Error        string
}

type AuditLogQuery struct {
	Page     int
	PageSize int
	Action   string
	Result   string
}

type AuditLogPage struct {
	Items    []AuditLog `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

type LatestVersionInput struct {
	GithubProxy string `json:"githubProxy"`
}

type LatestVersionResult struct {
	Latest string `json:"latest"`
}

type Server struct {
	ID                string      `json:"id"`
	Name              string      `json:"name"`
	ServerAddr        string      `json:"serverAddr"`
	ServerPort        int         `json:"serverPort"`
	AuthToken         string      `json:"authToken,omitempty"`
	TransportProtocol string      `json:"transportProtocol"`
	Status            string      `json:"status"`
	AutoStart         bool        `json:"autoStart"`
	AutoRestart       bool        `json:"autoRestart"`
	MaxRestarts       int         `json:"maxRestarts"`
	ProxyCount        int         `json:"proxyCount"`
	Uptime            string      `json:"uptime"`
	LastReloadAt      string      `json:"lastReloadAt"`
	RestartRequired   bool        `json:"restartRequired"`
	AdminAddr         string      `json:"adminAddr"`
	AdminPort         int         `json:"adminPort"`
	AdminUser         string      `json:"adminUser,omitempty"`
	AdminPassword     string      `json:"adminPassword,omitempty"`
	CreatedAt         string      `json:"createdAt"`
	UpdatedAt         string      `json:"updatedAt"`
	Rules             []ProxyRule `json:"rules,omitempty"`
}

type ServerInput struct {
	Name              string `json:"name"`
	ServerAddr        string `json:"serverAddr"`
	ServerPort        int    `json:"serverPort"`
	AuthToken         string `json:"authToken"`
	TransportProtocol string `json:"transportProtocol"`
	AutoStart         bool   `json:"autoStart"`
	AutoRestart       bool   `json:"autoRestart"`
	MaxRestarts       int    `json:"maxRestarts"`
	AdminPort         int    `json:"adminPort"`
	AdminUser         string `json:"adminUser,omitempty"`
	AdminPassword     string `json:"adminPassword,omitempty"`
}

type ProxyRule struct {
	ID                string   `json:"id"`
	ServerID          string   `json:"serverId"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	LocalIP           string   `json:"localIp"`
	LocalPort         int      `json:"localPort"`
	RemotePort        int      `json:"remotePort,omitempty"`
	CustomDomains     []string `json:"customDomains,omitempty"`
	Enabled           bool     `json:"enabled"`
	SecretKey         string   `json:"secretKey,omitempty"`
	Role              string   `json:"role,omitempty"`
	ServerName        string   `json:"serverName,omitempty"`
	BindAddr          string   `json:"bindAddr,omitempty"`
	BindPort          int      `json:"bindPort,omitempty"`
	UseEncryption     bool     `json:"useEncryption"`
	UseCompression    bool     `json:"useCompression"`
	BandwidthLimit    string   `json:"bandwidthLimit,omitempty"`
	Locations         []string `json:"locations,omitempty"`
	HostHeaderRewrite string   `json:"hostHeaderRewrite,omitempty"`
	HTTPUser          string   `json:"httpUser,omitempty"`
	HTTPPassword      string   `json:"httpPassword,omitempty"`
	RequestHeaders    []string `json:"requestHeaders,omitempty"`
	CreatedAt         string   `json:"createdAt"`
	UpdatedAt         string   `json:"updatedAt"`
}

type ProxyRuleInput struct {
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	LocalIP           string   `json:"localIp"`
	LocalPort         int      `json:"localPort"`
	RemotePort        int      `json:"remotePort"`
	CustomDomains     []string `json:"customDomains"`
	Enabled           bool     `json:"enabled"`
	SecretKey         string   `json:"secretKey,omitempty"`
	Role              string   `json:"role,omitempty"`
	ServerName        string   `json:"serverName,omitempty"`
	BindAddr          string   `json:"bindAddr,omitempty"`
	BindPort          int      `json:"bindPort,omitempty"`
	UseEncryption     bool     `json:"useEncryption"`
	UseCompression    bool     `json:"useCompression"`
	BandwidthLimit    string   `json:"bandwidthLimit,omitempty"`
	Locations         []string `json:"locations,omitempty"`
	HostHeaderRewrite string   `json:"hostHeaderRewrite,omitempty"`
	HTTPUser          string   `json:"httpUser,omitempty"`
	HTTPPassword      string   `json:"httpPassword,omitempty"`
	RequestHeaders    []string `json:"requestHeaders,omitempty"`
}

type HealthEvent struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	ServerID  string `json:"serverId"`
	Server    string `json:"server"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

type FRPCVersion struct {
	ID        string `json:"id"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Latest    string `json:"latest"`
	Path      string `json:"path"`
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
	Source    string `json:"source"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"createdAt"`
}

type FRPCInstallOnlineInput struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	GithubProxy string `json:"githubProxy"`
}

// FRPCBinaryCandidate 是在系统 PATH 及常见安装目录中发现的、可直接登记使用的
// frpc 二进制（典型来源：apt、官方安装脚本、手动放到 /usr/local/bin）。
type FRPCBinaryCandidate struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	// Managed 表示该路径位于本面板的受管目录内（已通过在线/离线安装登记过）。
	Managed bool `json:"managed"`
}

// FRPCProcessCandidate 是发现的、当前正在运行的 frpc 进程。可能由 systemd、
// supervisor 或手动启动，不一定是本面板拉起的。
type FRPCProcessCandidate struct {
	PID        int    `json:"pid"`
	Exe        string `json:"exe"`        // 进程对应的二进制路径，部分平台/权限下可能为空
	ConfigPath string `json:"configPath"` // 命令行 -c/--config 指定的配置文件，可能为空
	// Managed 表示该 PID 已被本面板纳管（命中 state.json 中的进程记录）。
	Managed  bool   `json:"managed"`
	ServerID string `json:"serverId,omitempty"` // 已纳管时对应的服务器 ID
}

// FRPCDiscovery 汇总一次「发现已有 frpc」的结果：可登记的二进制 + 运行中的进程。
type FRPCDiscovery struct {
	Binaries  []FRPCBinaryCandidate  `json:"binaries"`
	Processes []FRPCProcessCandidate `json:"processes"`
}

// RegisterBinaryInput 把系统中已存在的某个 frpc 二进制登记为可用版本，免去重新下载。
type RegisterBinaryInput struct {
	Path string `json:"path"`
}

// ImportFrpcConfigInput 把一段现成的 frpc 配置（frp v0.52+ 的 TOML 或旧版 INI）
// 解析并导入为一台新的服务器（含其全部代理规则）。
type ImportFrpcConfigInput struct {
	Name      string `json:"name"`      // 可选；留空时自动命名
	Content   string `json:"content"`   // 配置文件原文
	AutoStart bool   `json:"autoStart"` // 导入后是否随面板自动启动
}

// AdoptProcessInput 纳管一个正在运行的 frpc 进程：先读取其配置文件导入为服务器，
// 再按 Mode 接管该进程。
//
// Mode 取值：
//   - "restart"（默认）：停掉外部进程，由面板用导入后的配置重新拉起。
//     纳管后日志、重载、自动重启等能力完整，代价是一次短暂的隧道重连。
//   - "attach"：直接附着到现有进程，不重启。隧道零中断，但面板拿不到原始
//     stdout/stderr 日志，重载依赖 admin API。
type AdoptProcessInput struct {
	PID        int    `json:"pid"`
	ConfigPath string `json:"configPath"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
}

type ProcessInfo struct {
	ServerID    string `json:"serverId"`
	PID         int    `json:"pid"`
	FRPCVersion string `json:"frpcVersion"`
	ConfigPath  string `json:"configPath"`
	LogPath     string `json:"logPath"`
	StartedAt   string `json:"startedAt"`
}

type ConfigBundle struct {
	Version          int            `json:"version"`
	ExportedAt       string         `json:"exportedAt"`
	IncludeSensitive bool           `json:"includeSensitive"`
	Servers          []ServerBundle `json:"servers"`
	Versions         []FRPCVersion  `json:"versions,omitempty"`
	GithubProxy      string         `json:"githubProxy,omitempty"`
	// 旧版本导出文件包含日志刷新设置；保留字段以便旧备份仍可导入，值会被忽略。
	LogAutoRefresh     bool `json:"logAutoRefresh,omitempty"`
	LogRefreshInterval int  `json:"logRefreshInterval,omitempty"`
}

type ServerBundle struct {
	Server Server      `json:"server"`
	Rules  []ProxyRule `json:"rules"`
}

type ConfigImportInput struct {
	Mode   string       `json:"mode"`
	Bundle ConfigBundle `json:"bundle"`
}

type BackupFile struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"createdAt"`
}

type BackupRestoreInput struct {
	Mode string `json:"mode"`
}

// ProxyStatus 是 frpc admin API /api/status 返回的单条 proxy 实时状态。
// Phase 取值来自 frp：new / wait start / start error / running / check failed / closed。
type ProxyStatus struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Phase      string `json:"phase"`
	Err        string `json:"err,omitempty"`
	LocalAddr  string `json:"localAddr,omitempty"`
	Plugin     string `json:"plugin,omitempty"`
	RemoteAddr string `json:"remoteAddr,omitempty"`
}

type ServerProxyStatus struct {
	ServerID string        `json:"serverId"`
	Running  bool          `json:"running"`
	Error    string        `json:"error,omitempty"`
	Proxies  []ProxyStatus `json:"proxies"`
}

type LogLine struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type ActionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
}

type ConfigPreview struct {
	ConfigPath string `json:"configPath"`
	Content    string `json:"content"`
}
