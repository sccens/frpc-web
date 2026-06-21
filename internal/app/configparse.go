package app

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

// 本文件把一段 frpc 配置原文（frp v0.52+ 的 TOML，或旧版 INI）解析为面板展示用的
// Server + []ProxyRule。解析器刻意保持宽松：无法识别的键一律忽略，结果交由调用方
// 决定如何展示；只有“连 serverAddr 与代理都解析不出”时才返回错误。
//
// v2.0 起解析结果不再用于生成配置（面板不写 frpc.toml 结构化字段，只做原文编辑），
// 因此这里不做字段归一化与强校验——尽量原样还原磁盘上的内容。

// ParseFrpcConfig 把 frpc 配置原文解析为 Server + 规则列表。
// 含 [common] 段的按旧版 INI 解析，否则按 TOML。
func ParseFrpcConfig(content string) (Server, []ProxyRule, error) {
	var (
		server Server
		rules  []ProxyRule
	)
	if looksLikeINI(content) {
		server, rules = parseFrpcINI(content)
	} else {
		server, rules = parseFrpcTOML(content)
	}
	if strings.TrimSpace(server.ServerAddr) == "" && len(rules) == 0 {
		return Server{}, nil, errors.New("未能从内容中解析出 frpc 配置（serverAddr 与代理均为空）")
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

func parseFrpcTOML(content string) (Server, []ProxyRule) {
	root := newFrpcTable()
	var proxies, visitors []*frpcTable
	cur := root    // 标量键的写入目标
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

	server := Server{
		ServerAddr:        root.str("serverAddr"),
		ServerPort:        root.int("serverPort"),
		AuthToken:         root.str("auth.token"),
		TransportProtocol: root.str("transport.protocol"),
		AdminAddr:         defaultString(root.str("webServer.addr"), "127.0.0.1"),
		AdminPort:         root.int("webServer.port"),
		AdminUser:         root.str("webServer.user"),
		AdminPassword:     root.str("webServer.password"),
		LogPath:           root.str("log.to"),
	}
	rules := make([]ProxyRule, 0, len(proxies)+len(visitors))
	for _, p := range proxies {
		rules = append(rules, proxyRuleFromTable(p, ""))
	}
	for _, v := range visitors {
		rules = append(rules, proxyRuleFromTable(v, "visitor"))
	}
	return server, rules
}

func parseFrpcINI(content string) (Server, []ProxyRule) {
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

	var server Server
	if common := sections["common"]; common != nil {
		server = Server{
			ServerAddr:        common.str("server_addr"),
			ServerPort:        common.int("server_port"),
			TransportProtocol: common.str("protocol"),
			AdminAddr:         defaultString(common.str("admin_addr"), "127.0.0.1"),
			AdminPort:         common.int("admin_port"),
			AdminUser:         common.str("admin_user"),
			AdminPassword:     common.str("admin_pwd"),
			LogPath:           common.str("log_file"),
		}
		server.AuthToken = common.str("token")
		if server.AuthToken == "" {
			server.AuthToken = common.str("authentication_token")
		}
	}
	var rules []ProxyRule
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
func proxyRuleFromTable(t *frpcTable, forceRole string) ProxyRule {
	role := t.str("role")
	if forceRole != "" {
		role = forceRole
	}
	return ProxyRule{
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
func proxyRuleFromINISection(name string, t *frpcTable) ProxyRule {
	return ProxyRule{
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
		parts              []string
		b                  strings.Builder
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

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
