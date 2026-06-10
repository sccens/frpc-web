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
	Addr        string `json:"addr"`
	GithubProxy string `json:"githubProxy"`
}

type SettingsInput struct {
	GithubProxy string `json:"githubProxy"`
}

type AuthInput struct {
	AccessKey string `json:"accessKey"`
}

type AuthStatus struct {
	Bootstrapped  bool `json:"bootstrapped"`
	Authenticated bool `json:"authenticated"`
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

type Stats struct {
	Summary   StatsSummary  `json:"summary"`
	Servers   []ServerStats `json:"servers"`
	Proxies   []ProxyStats  `json:"proxies"`
	Errors    []StatsError  `json:"errors"`
	SampledAt string        `json:"sampledAt"`
}

type StatsSummary struct {
	TotalServers     int   `json:"totalServers"`
	RunningServers   int   `json:"runningServers"`
	ProxyRules       int   `json:"proxyRules"`
	OnlineProxies    int   `json:"onlineProxies"`
	ErrorProxies     int   `json:"errorProxies"`
	TrafficAvailable bool  `json:"trafficAvailable"`
	TotalTrafficIn   int64 `json:"totalTrafficIn"`
	TotalTrafficOut  int64 `json:"totalTrafficOut"`
}

type ServerStats struct {
	ServerID         string `json:"serverId"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	AdminPort        int    `json:"adminPort"`
	ProxyCount       int    `json:"proxyCount"`
	OnlineProxies    int    `json:"onlineProxies"`
	ErrorProxies     int    `json:"errorProxies"`
	TrafficAvailable bool   `json:"trafficAvailable"`
	TrafficIn        int64  `json:"trafficIn"`
	TrafficOut       int64  `json:"trafficOut"`
	Error            string `json:"error,omitempty"`
	SampledAt        string `json:"sampledAt"`
}

type ProxyStats struct {
	ServerID         string `json:"serverId"`
	ServerName       string `json:"serverName"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	LocalAddr        string `json:"localAddr"`
	RemoteAddr       string `json:"remoteAddr"`
	TrafficAvailable bool   `json:"trafficAvailable"`
	TrafficIn        int64  `json:"trafficIn"`
	TrafficOut       int64  `json:"trafficOut"`
	Error            string `json:"error,omitempty"`
}

type StatsError struct {
	ServerID   string `json:"serverId"`
	ServerName string `json:"serverName"`
	ProxyName  string `json:"proxyName,omitempty"`
	Message    string `json:"message"`
}

type AdminStatus struct {
	Proxies []AdminProxyStatus
}

type AdminProxyStatus struct {
	Name             string
	Type             string
	Status           string
	LocalAddr        string
	RemoteAddr       string
	TrafficAvailable bool
	TrafficIn        int64
	TrafficOut       int64
	Error            string
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
