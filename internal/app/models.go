package app

// v2.0 数据模型：面板不再管理进程，server 列表来自对磁盘上 frpc 配置文件的
// 扫描解析（见 configscan.go），不持久化到 state.json。这里只保留展示、认证、
// 备份导出/导入、自身更新所需的类型。

type Settings struct {
	Addr        string `json:"addr"`
	GithubProxy string `json:"githubProxy"`
}

// SettingsInput 仅保留下载代理一项；其余设置已移除。
type SettingsInput struct {
	GithubProxy string `json:"githubProxy"`
}

type FrpsTarget struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"intervalSeconds"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type FrpsTargetInput struct {
	Name            string `json:"name"`
	URL             string `json:"url"`
	Username        string `json:"username"`
	Password        string `json:"password"`
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

type FrpsTargetView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	Username        string `json:"username,omitempty"`
	HasPassword     bool   `json:"hasPassword"`
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int    `json:"intervalSeconds"`
	Status          string `json:"status"`
	LastError       string `json:"lastError,omitempty"`
	LastScrapedAt   string `json:"lastScrapedAt,omitempty"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
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

// Server 是一台 frpc 实例的只读视图，由扫描某个 frpc 配置文件得到。
// 一文件 = 一 server；ID 是配置文件绝对路径的稳定哈希（见 configscan.go）。
type Server struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	ConfigPath        string `json:"configPath"`
	ServerAddr        string `json:"serverAddr"`
	ServerPort        int    `json:"serverPort"`
	AuthToken         string `json:"authToken,omitempty"`
	TransportProtocol string `json:"transportProtocol"`
	// Status 取值：running（admin API 可达）/ stopped（不可达）/ no-admin（配置无 webServer）/ error（解析失败）。
	Status        string `json:"status"`
	AdminAddr     string `json:"adminAddr"`
	AdminPort     int    `json:"adminPort"`
	AdminUser     string `json:"adminUser,omitempty"`
	AdminPassword string `json:"adminPassword,omitempty"`
	// LogPath 解析自配置的 log.to；为空表示该实例未配置文件日志。
	LogPath    string      `json:"logPath,omitempty"`
	Writable   bool        `json:"writable"`
	ProxyCount int         `json:"proxyCount"`
	Rules      []ProxyRule `json:"rules,omitempty"`
}

// ProxyRule 是一条代理规则的只读视图，由解析配置文件的 [[proxies]]/[[visitors]] 得到。
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
}

type HealthEvent struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	ServerID  string `json:"serverId"`
	Server    string `json:"server"`
	Message   string `json:"message"`
	CreatedAt string `json:"createdAt"`
}

// ConfigFile 是磁盘上一个 frpc 配置文件的路径与原文。
type ConfigFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Writable bool   `json:"writable"`
	IsToml   bool   `json:"isToml"`
}

// ConfigBundle 是导出/导入的备份包：一组配置文件的路径与原文。
type ConfigBundle struct {
	Version     int          `json:"version"`
	ExportedAt  string       `json:"exportedAt"`
	Files       []ConfigFile `json:"files"`
	GithubProxy string       `json:"githubProxy,omitempty"`
}

type ConfigImportInput struct {
	Bundle ConfigBundle `json:"bundle"`
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

type FrpsTrafficPoint struct {
	Time           string  `json:"time"`
	TrafficInRate  float64 `json:"trafficInRate"`
	TrafficOutRate float64 `json:"trafficOutRate"`
}

type FrpsProxyMetric struct {
	Name            string  `json:"name"`
	Type            string  `json:"type"`
	ConnectionCount int     `json:"connectionCount"`
	TrafficIn       int64   `json:"trafficIn"`
	TrafficOut      int64   `json:"trafficOut"`
	TrafficInRate   float64 `json:"trafficInRate"`
	TrafficOutRate  float64 `json:"trafficOutRate"`
}

type FrpsTargetTestResult struct {
	OK              bool   `json:"ok"`
	Status          string `json:"status"`
	Message         string `json:"message"`
	ClientCount     int    `json:"clientCount"`
	ProxyCount      int    `json:"proxyCount"`
	ConnectionCount int    `json:"connectionCount"`
	TrafficIn       int64  `json:"trafficIn"`
	TrafficOut      int64  `json:"trafficOut"`
}

type FrpsTargetMetrics struct {
	Target          FrpsTargetView     `json:"target"`
	ClientCount     int                `json:"clientCount"`
	ProxyCount      int                `json:"proxyCount"`
	ConnectionCount int                `json:"connectionCount"`
	TrafficIn       int64              `json:"trafficIn"`
	TrafficOut      int64              `json:"trafficOut"`
	TrafficInRate   float64            `json:"trafficInRate"`
	TrafficOutRate  float64            `json:"trafficOutRate"`
	Proxies         []FrpsProxyMetric  `json:"proxies"`
	History         []FrpsTrafficPoint `json:"history"`
}

type FrpsTotals struct {
	TargetCount     int     `json:"targetCount"`
	OnlineCount     int     `json:"onlineCount"`
	OfflineCount    int     `json:"offlineCount"`
	DisabledCount   int     `json:"disabledCount"`
	ClientCount     int     `json:"clientCount"`
	ProxyCount      int     `json:"proxyCount"`
	ConnectionCount int     `json:"connectionCount"`
	TrafficIn       int64   `json:"trafficIn"`
	TrafficOut      int64   `json:"trafficOut"`
	TrafficInRate   float64 `json:"trafficInRate"`
	TrafficOutRate  float64 `json:"trafficOutRate"`
}

type FrpsMetricsOverview struct {
	Targets []FrpsTargetMetrics `json:"targets"`
	Totals  FrpsTotals          `json:"totals"`
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
