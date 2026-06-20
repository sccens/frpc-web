package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 本文件实现「接管已有 frpc」的三块能力的 app 层逻辑：
//   1. 解析现成的 frpc 配置（frp v0.52+ 的 TOML，或旧版 INI）为面板的服务器模型；
//   2. 把发现的二进制登记为可用版本（RegisterBinary，发现逻辑在 frpc 包）；
//   3. 纳管一个正在运行的 frpc 进程（AdoptProcess）。
//
// 解析器刻意保持宽松：无法识别的键一律忽略，最终结果交由常规的
// validateServer/validateRule 校验，因此「部分识别」的配置会以普通校验错误
// 暴露，而不是 panic。

// maxImportConfigSize 限制单个被导入/读取的 frpc 配置文件大小。
const maxImportConfigSize = 1 << 20 // 1MB

// DiscoverFRPC 汇总系统中可登记的 frpc 二进制与正在运行的 frpc 进程，
// 并标注哪些进程已被本面板纳管（PID 命中 state.json 中的进程记录）。
func (s *Service) DiscoverFRPC(ctx context.Context) (FRPCDiscovery, error) {
	binaries := s.runtime.DiscoverBinaries()
	processes, err := s.runtime.DiscoverProcesses()
	if err != nil {
		return FRPCDiscovery{}, err
	}
	tracked := map[int]string{}
	if servers, listErr := s.store.ListServers(ctx); listErr == nil {
		for _, srv := range servers {
			if p, pErr := s.store.GetProcess(ctx, srv.ID); pErr == nil && p.PID > 0 {
				tracked[p.PID] = srv.ID
			}
		}
	}
	for i := range processes {
		if id, ok := tracked[processes[i].PID]; ok {
			processes[i].Managed = true
			processes[i].ServerID = id
		}
	}
	return FRPCDiscovery{Binaries: binaries, Processes: processes}, nil
}

// RegisterBinary 把系统中已存在的某个 frpc 二进制登记为当前激活版本，免去重新下载。
// 这是经过认证的管理员显式选择的二进制，会先用 `frpc --version` 验证其可用性；
// 其权限边界与「离线上传二进制」一致（管理员本就能让面板 exec 任意 frpc 二进制）。
func (s *Service) RegisterBinary(ctx context.Context, input RegisterBinaryInput) (FRPCVersion, error) {
	path := strings.TrimSpace(input.Path)
	if path == "" {
		return FRPCVersion{}, invalidInput(errors.New("binary path is required"))
	}
	version, err := s.runtime.RegisterBinary(path)
	if err != nil {
		return FRPCVersion{}, invalidInput(err)
	}
	version.Active = true
	return s.store.AddVersion(ctx, version)
}

// ImportFrpcConfig 把一段现成的 frpc 配置导入为一台新服务器（含其全部代理规则）。
func (s *Service) ImportFrpcConfig(ctx context.Context, input ImportFrpcConfigInput) (Server, error) {
	return s.createServerFromConfig(ctx, input.Name, input.Content, input.AutoStart)
}

// AdoptResult 是纳管运行中进程的结果。
type AdoptResult struct {
	Server  Server `json:"server"`
	Started bool   `json:"started"`
	Message string `json:"message"`
}

// AdoptProcess 纳管一个正在运行的 frpc 进程：先读取其配置文件导入为服务器，再按
// Mode 接管。为避免信任客户端传入的任意路径，这里会重新发现进程并以 PID 匹配，
// 取其真实的二进制路径与配置路径。
func (s *Service) AdoptProcess(ctx context.Context, input AdoptProcessInput) (AdoptResult, error) {
	if input.PID <= 0 {
		return AdoptResult{}, invalidInput(errors.New("process pid is required"))
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "restart"
	}
	if mode != "restart" && mode != "attach" {
		return AdoptResult{}, invalidInput(errors.New("adopt mode must be restart or attach"))
	}

	processes, err := s.runtime.DiscoverProcesses()
	if err != nil {
		return AdoptResult{}, err
	}
	var match *FRPCProcessCandidate
	for i := range processes {
		if processes[i].PID == input.PID {
			match = &processes[i]
			break
		}
	}
	if match == nil {
		return AdoptResult{}, invalidInput(fmt.Errorf("未找到 PID=%d 的 frpc 进程（可能已退出）", input.PID))
	}

	cfgPath := strings.TrimSpace(input.ConfigPath)
	if cfgPath == "" {
		cfgPath = strings.TrimSpace(match.ConfigPath)
	}
	if cfgPath == "" {
		return AdoptResult{}, invalidInput(errors.New("无法确定该进程的配置文件路径，请在发现结果中手动指定"))
	}
	content, err := readConfigFile(cfgPath)
	if err != nil {
		return AdoptResult{}, invalidInput(fmt.Errorf("读取配置文件失败: %w", err))
	}

	server, err := s.createServerFromConfig(ctx, input.Name, content, true)
	if err != nil {
		return AdoptResult{}, err
	}

	// 尽力把进程正在运行的二进制登记为激活版本（restart 模式重启时需要它）。
	if exe := strings.TrimSpace(match.Exe); exe != "" {
		if version, regErr := s.runtime.RegisterBinary(exe); regErr == nil {
			version.Active = true
			_, _ = s.store.AddVersion(ctx, version)
		}
	}

	if mode == "attach" {
		now := time.Now().Format(time.RFC3339)
		_ = s.store.UpsertProcess(ctx, ProcessInfo{
			ServerID:    server.ID,
			PID:         input.PID,
			FRPCVersion: s.currentVersion(ctx).Version,
			ConfigPath:  cfgPath,
			StartedAt:   now,
		})
		_ = s.store.SetServerStatus(ctx, server.ID, "running")
		s.runtime.Adopt(server.ID, input.PID)
		_ = s.store.AddHealth(ctx, server.ID, "info", "已附着到运行中的 frpc 进程（未重启）；原始 stdout 日志在面板内不可见。")
		adopted, _ := s.Server(ctx, server.ID)
		return AdoptResult{
			Server:  adopted,
			Started: true,
			Message: "已附着到运行中的 frpc 进程（未重启）。进程退出后面板会按配置接管自动重启。",
		}, nil
	}

	// restart 模式：停掉外部进程，再由面板用导入后的配置重新拉起。
	if _, result := s.requireActiveVersion(ctx); !result.OK {
		adopted, _ := s.Server(ctx, server.ID)
		return AdoptResult{
			Server:  adopted,
			Started: false,
			Message: "已导入配置，但没有可用的 frpc 二进制，无法重启接管。请先登记二进制，或改用 attach 模式。",
		}, nil
	}

	// 检查 systemd 管理和 admin API
	warnings := []string{}
	if match.SystemdManaged {
		unitInfo := ""
		if match.SystemdUnit != "" {
			unitInfo = fmt.Sprintf("（%s）", match.SystemdUnit)
		}
		warnings = append(warnings, fmt.Sprintf(
			"⚠️ 该进程由 systemd 托管%s。停止后 systemd 可能立即重启它，导致与面板冲突。"+
				"建议先手动停用服务：sudo systemctl disable --now %s",
			unitInfo, match.SystemdUnit))
	}
	if !match.HasAdminAPI {
		warnings = append(warnings,
			"⚠️ 配置文件中未启用 admin API（webServer），面板重启后可能无法完全控制进程。"+
			"面板会自动添加 admin API 配置。")
	}

	// 用外部 PID 构造一条进程记录，借 Stop 的 PID 分支结束它（会等待其真正退出，
	// 释放 admin 端口），随后由面板启动同一份配置。
	stopResult := s.runtime.Stop(ctx, server, ProcessInfo{ServerID: server.ID, PID: input.PID})
	if !stopResult.OK {
		adopted, _ := s.Server(ctx, server.ID)
		return AdoptResult{
			Server:  adopted,
			Started: false,
			Message: "已导入配置，但停止原进程失败：" + stopResult.Message,
		}, nil
	}
	startResult := s.Start(ctx, server.ID)
	adopted, _ := s.Server(ctx, server.ID)
	message := startResult.Message
	if startResult.OK {
		message = "✅ 已纳管：原进程已停止，面板已用导入的配置重新启动 frpc。"
		if len(warnings) > 0 {
			message += "\n\n" + strings.Join(warnings, "\n")
		}
	}
	return AdoptResult{Server: adopted, Started: startResult.OK, Message: message}, nil
}

// createServerFromConfig 解析配置并创建服务器与其规则。创建前先整体校验，
// 避免在中途失败时留下半套配置。
func (s *Service) createServerFromConfig(ctx context.Context, name, content string, autoStart bool) (Server, error) {
	if int64(len(content)) > maxImportConfigSize {
		return Server{}, invalidInput(errors.New("配置内容过大"))
	}
	serverInput, rules, err := parseFrpcConfig(content)
	if err != nil {
		return Server{}, invalidInput(err)
	}
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		serverInput.Name = trimmed
	}
	if strings.TrimSpace(serverInput.Name) == "" {
		serverInput.Name = "导入的 frpc"
	}
	serverInput.AutoStart = autoStart
	serverInput.AutoRestart = true
	serverInput = normalizeServerDefaults(serverInput)
	if err := validateServer(serverInput); err != nil {
		return Server{}, invalidInput(err)
	}

	normalized := make([]ProxyRuleInput, 0, len(rules))
	for _, rule := range rules {
		rule = normalizeRuleDefaults(rule)
		if err := validateRule(rule); err != nil {
			return Server{}, invalidInput(fmt.Errorf("代理 %q: %w", rule.Name, err))
		}
		normalized = append(normalized, rule)
	}

	server, err := s.store.CreateServer(ctx, serverInput)
	if err != nil {
		return Server{}, err
	}
	for _, rule := range normalized {
		if _, err := s.store.CreateRule(ctx, server.ID, rule); err != nil {
			return Server{}, err
		}
	}
	if _, err := s.applyConfig(ctx, server.ID); err != nil {
		return Server{}, err
	}
	return s.Server(ctx, server.ID)
}

func readConfigFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("配置路径是一个目录")
	}
	if info.Size() > maxImportConfigSize {
		return "", errors.New("配置文件过大")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseFrpcConfig 把 frpc 配置原文解析为 ServerInput + 规则列表。
// 含 [common] 段的按旧版 INI 解析，否则按 TOML。
func parseFrpcConfig(content string) (ServerInput, []ProxyRuleInput, error) {
	var (
		server ServerInput
		rules  []ProxyRuleInput
	)
	if looksLikeINI(content) {
		server, rules = parseFrpcINI(content)
	} else {
		server, rules = parseFrpcTOML(content)
	}
	if strings.TrimSpace(server.ServerAddr) == "" && len(rules) == 0 {
		return ServerInput{}, nil, errors.New("未能从内容中解析出 frpc 配置（serverAddr 与代理均为空）")
	}
	return server, rules, nil
}

func looksLikeINI(content string) bool {
	for _, raw := range strings.Split(content, "\n") {
		if strings.EqualFold(strings.TrimSpace(raw), "[common]") {
			return true
		}
	}
	return false
}

// frpcTable 是配置中一个作用域（顶层、或某个 [[proxies]]/[[visitors]] 元素）下
// 的键值集合。键统一以小写存储以便大小写无关查询，rawKeys 保留原始大小写
// （供提取 requestHeaders 的头名称时还原大小写）。
type frpcTable struct {
	scalars map[string]string
	arrays  map[string][]string
	rawKeys map[string]string
}

func newFrpcTable() *frpcTable {
	return &frpcTable{
		scalars: map[string]string{},
		arrays:  map[string][]string{},
		rawKeys: map[string]string{},
	}
}

// setTOML 解析 TOML 右值（可能是数组或带引号的标量）后存入。
func (t *frpcTable) setTOML(key, raw string) {
	lk := strings.ToLower(key)
	t.rawKeys[lk] = key
	if list, ok := parseTOMLArray(raw); ok {
		t.arrays[lk] = list
		return
	}
	t.scalars[lk] = parseTOMLScalar(raw)
}

// setRaw 直接存入未加工的标量值（INI 值不带引号）。
func (t *frpcTable) setRaw(key, val string) {
	lk := strings.ToLower(key)
	t.rawKeys[lk] = key
	t.scalars[lk] = strings.TrimSpace(val)
}

func (t *frpcTable) str(key string) string { return t.scalars[strings.ToLower(key)] }

func (t *frpcTable) int(key string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(t.scalars[strings.ToLower(key)]))
	return n
}

func (t *frpcTable) bool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(t.scalars[strings.ToLower(key)]))
	return v == "true" || v == "1"
}

// list 返回数组值；若该键以标量形式存储（如 INI 的逗号分隔串或单个域名），
// 则按逗号拆分回退。
func (t *frpcTable) list(key string) []string {
	lk := strings.ToLower(key)
	if a, ok := t.arrays[lk]; ok {
		return a
	}
	if s := strings.TrimSpace(t.scalars[lk]); s != "" {
		return splitComma(s)
	}
	return nil
}

// requestHeaders 提取请求头设置，支持 TOML 的 requestHeaders.set.<Name> 与
// 旧版 INI 的 header_set_<Name> / header_<Name>，还原成 "Name: Value"。
func (t *frpcTable) requestHeaders() []string {
	var out []string
	for lk, value := range t.scalars {
		orig := t.rawKeys[lk]
		switch {
		case strings.HasPrefix(lk, "requestheaders.set."):
			out = append(out, orig[len("requestheaders.set."):]+": "+value)
		case strings.HasPrefix(lk, "header_set_"):
			out = append(out, orig[len("header_set_"):]+": "+value)
		case strings.HasPrefix(lk, "header_"):
			out = append(out, orig[len("header_"):]+": "+value)
		}
	}
	sort.Strings(out)
	return out
}

func parseFrpcTOML(content string) (ServerInput, []ProxyRuleInput) {
	root := newFrpcTable()
	var proxies, visitors []*frpcTable
	cur := root  // 标量键的写入目标
	rootPrefix := "" // 当 cur==root 时，活动的 [single.table] 前缀

	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(stripTOMLComment(lines[i]))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
			name := strings.TrimSpace(line[2 : len(line)-2])
			tbl := newFrpcTable()
			switch strings.ToLower(name) {
			case "proxies":
				proxies = append(proxies, tbl)
			case "visitors":
				visitors = append(visitors, tbl)
			}
			cur = tbl
			rootPrefix = ""
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			rootPrefix = strings.TrimSpace(line[1 : len(line)-1])
			cur = root
			continue
		}
		key, raw, ok := cutKeyValue(line)
		if !ok {
			continue
		}
		// 多行数组：右值以 '[' 开头但本行未闭合时，继续吞并后续行直到出现 ']'。
		if trimmed := strings.TrimSpace(raw); strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed, "]") {
			for i+1 < len(lines) && !strings.Contains(raw, "]") {
				i++
				raw += " " + strings.TrimSpace(stripTOMLComment(lines[i]))
			}
		}
		fullKey := key
		if cur == root && rootPrefix != "" {
			fullKey = rootPrefix + "." + key
		}
		cur.setTOML(fullKey, raw)
	}

	server := ServerInput{
		ServerAddr:        root.str("serverAddr"),
		ServerPort:        root.int("serverPort"),
		AuthToken:         root.str("auth.token"),
		TransportProtocol: root.str("transport.protocol"),
		AdminPort:         root.int("webServer.port"),
		AdminUser:         root.str("webServer.user"),
		AdminPassword:     root.str("webServer.password"),
	}
	rules := make([]ProxyRuleInput, 0, len(proxies)+len(visitors))
	for _, p := range proxies {
		rules = append(rules, proxyRuleFromTable(p, ""))
	}
	for _, v := range visitors {
		rules = append(rules, proxyRuleFromTable(v, "visitor"))
	}
	return server, rules
}

func parseFrpcINI(content string) (ServerInput, []ProxyRuleInput) {
	sections := map[string]*frpcTable{}
	var order []string
	cur := ""
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			cur = strings.TrimSpace(line[1 : len(line)-1])
			if sections[cur] == nil {
				sections[cur] = newFrpcTable()
				order = append(order, cur)
			}
			continue
		}
		if cur == "" {
			continue
		}
		key, val, ok := cutKeyValue(line)
		if !ok {
			continue
		}
		sections[cur].setRaw(key, val)
	}

	var server ServerInput
	if common := sections["common"]; common != nil {
		server = ServerInput{
			ServerAddr:        common.str("server_addr"),
			ServerPort:        common.int("server_port"),
			TransportProtocol: common.str("protocol"),
			AdminPort:         common.int("admin_port"),
			AdminUser:         common.str("admin_user"),
			AdminPassword:     common.str("admin_pwd"),
		}
		server.AuthToken = common.str("token")
		if server.AuthToken == "" {
			server.AuthToken = common.str("authentication_token")
		}
	}
	var rules []ProxyRuleInput
	for _, name := range order {
		if strings.EqualFold(name, "common") {
			continue
		}
		rules = append(rules, proxyRuleFromINISection(name, sections[name]))
	}
	return server, rules
}

// proxyRuleFromTable 从 TOML 的 [[proxies]]/[[visitors]] 元素构造规则。
// forceRole 非空时（visitor）强制角色。
func proxyRuleFromTable(t *frpcTable, forceRole string) ProxyRuleInput {
	role := t.str("role")
	if forceRole != "" {
		role = forceRole
	}
	return ProxyRuleInput{
		Name:              t.str("name"),
		Type:              t.str("type"),
		LocalIP:           t.str("localIP"),
		LocalPort:         t.int("localPort"),
		RemotePort:        t.int("remotePort"),
		CustomDomains:     t.list("customDomains"),
		Locations:         t.list("locations"),
		SecretKey:         t.str("secretKey"),
		Role:              role,
		ServerName:        t.str("serverName"),
		BindAddr:          t.str("bindAddr"),
		BindPort:          t.int("bindPort"),
		UseEncryption:     t.bool("transport.useEncryption"),
		UseCompression:    t.bool("transport.useCompression"),
		BandwidthLimit:    t.str("transport.bandwidthLimit"),
		HostHeaderRewrite: t.str("hostHeaderRewrite"),
		HTTPUser:          t.str("httpUser"),
		HTTPPassword:      t.str("httpPassword"),
		RequestHeaders:    t.requestHeaders(),
		Enabled:           true,
	}
}

// proxyRuleFromINISection 从旧版 INI 的一个代理段构造规则；段名即代理名。
func proxyRuleFromINISection(name string, t *frpcTable) ProxyRuleInput {
	return ProxyRuleInput{
		Name:              name,
		Type:              t.str("type"),
		LocalIP:           t.str("local_ip"),
		LocalPort:         t.int("local_port"),
		RemotePort:        t.int("remote_port"),
		CustomDomains:     t.list("custom_domains"),
		Locations:         t.list("locations"),
		SecretKey:         t.str("sk"),
		Role:              t.str("role"),
		ServerName:        t.str("server_name"),
		BindAddr:          t.str("bind_addr"),
		BindPort:          t.int("bind_port"),
		UseEncryption:     t.bool("use_encryption"),
		UseCompression:    t.bool("use_compression"),
		BandwidthLimit:    t.str("bandwidth_limit"),
		HostHeaderRewrite: t.str("host_header_rewrite"),
		HTTPUser:          t.str("http_user"),
		HTTPPassword:      t.str("http_pwd"),
		RequestHeaders:    t.requestHeaders(),
		Enabled:           true,
	}
}

// cutKeyValue 在首个 '=' 处拆分 "key = value"。
func cutKeyValue(line string) (string, string, bool) {
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

// stripTOMLComment 去除 TOML 行内 '#' 注释，但不动引号内的 '#'。
func stripTOMLComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

// parseTOMLArray 解析形如 ["a", "b"] 的字符串数组；非数组返回 ok=false。
func parseTOMLArray(raw string) ([]string, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil, false
	}
	inner := raw[1 : len(raw)-1]
	parts := splitTopLevelComma(inner)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, unquoteValue(p))
		}
	}
	return out, true
}

func parseTOMLScalar(raw string) string {
	return unquoteValue(strings.TrimSpace(raw))
}

func unquoteValue(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' {
			if unquoted, err := strconv.Unquote(s); err == nil {
				return unquoted
			}
			return s[1 : len(s)-1]
		}
		if s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// splitTopLevelComma 按逗号拆分，但忽略引号内的逗号。
func splitTopLevelComma(s string) []string {
	var (
		parts            []string
		b                strings.Builder
		inSingle, inDouble bool
	)
	for _, r := range s {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			b.WriteRune(r)
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
			b.WriteRune(r)
		case ',':
			if inSingle || inDouble {
				b.WriteRune(r)
			} else {
				parts = append(parts, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	parts = append(parts, b.String())
	return parts
}

func splitComma(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(unquoteValue(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}
