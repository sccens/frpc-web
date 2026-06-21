package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultFrpsIntervalSeconds = 5
	minFrpsIntervalSeconds     = 2
	maxFrpsIntervalSeconds     = 300
	frpsScrapeTimeout          = 5 * time.Second
	frpsHistoryLimit           = 360
)

type FrpsMonitor struct {
	loadTargets func(context.Context) ([]FrpsTarget, error)
	client      *http.Client

	mu      sync.RWMutex
	states  map[string]*frpsMonitorState
	refresh chan struct{}
}

type frpsMonitorState struct {
	Status        string
	LastError     string
	LastScrapedAt string
	LastAttemptAt time.Time
	Sample        frpsMetricSample
	Previous      frpsMetricSample
	History       []FrpsTrafficPoint
}

type frpsMetricSample struct {
	ScrapedAt       time.Time
	ClientCount     int
	ProxyCount      int
	ConnectionCount int
	TrafficIn       int64
	TrafficOut      int64
	TrafficInRate   float64
	TrafficOutRate  float64
	Proxies         []FrpsProxyMetric
}

type prometheusSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

func NewFrpsMonitor(loadTargets func(context.Context) ([]FrpsTarget, error)) *FrpsMonitor {
	return &FrpsMonitor{
		loadTargets: loadTargets,
		client:      &http.Client{Timeout: frpsScrapeTimeout},
		states:      map[string]*frpsMonitorState{},
		refresh:     make(chan struct{}, 1),
	}
}

func (m *FrpsMonitor) Run(ctx context.Context) {
	m.scrapeDue(ctx, true)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.scrapeDue(ctx, false)
		case <-m.refresh:
			m.scrapeDue(ctx, true)
		}
	}
}

func (m *FrpsMonitor) RefreshNow(ctx context.Context) {
	select {
	case m.refresh <- struct{}{}:
	default:
	}
}

func (m *FrpsMonitor) RemoveTarget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, id)
}

func (m *FrpsMonitor) TargetViews(targets []FrpsTarget) []FrpsTargetView {
	out := make([]FrpsTargetView, 0, len(targets))
	for _, target := range targets {
		out = append(out, m.TargetView(target))
	}
	return out
}

func (m *FrpsMonitor) TargetView(target FrpsTarget) FrpsTargetView {
	m.mu.RLock()
	state := m.states[target.ID]
	view := targetViewFromTarget(target, state)
	m.mu.RUnlock()
	return view
}

func (m *FrpsMonitor) Overview(targets []FrpsTarget) FrpsMetricsOverview {
	result := FrpsMetricsOverview{
		Targets: make([]FrpsTargetMetrics, 0, len(targets)),
	}
	for _, target := range targets {
		metrics := m.TargetMetrics(target)
		result.Targets = append(result.Targets, metrics)
		result.Totals.TargetCount++
		switch metrics.Target.Status {
		case "online":
			result.Totals.OnlineCount++
		case "disabled":
			result.Totals.DisabledCount++
		default:
			result.Totals.OfflineCount++
		}
		result.Totals.ClientCount += metrics.ClientCount
		result.Totals.ProxyCount += metrics.ProxyCount
		result.Totals.ConnectionCount += metrics.ConnectionCount
		result.Totals.TrafficIn += metrics.TrafficIn
		result.Totals.TrafficOut += metrics.TrafficOut
		result.Totals.TrafficInRate += metrics.TrafficInRate
		result.Totals.TrafficOutRate += metrics.TrafficOutRate
	}
	return result
}

func (m *FrpsMonitor) TargetMetrics(target FrpsTarget) FrpsTargetMetrics {
	m.mu.RLock()
	state := m.states[target.ID]
	view := targetViewFromTarget(target, state)
	var sample frpsMetricSample
	proxies := []FrpsProxyMetric{}
	history := []FrpsTrafficPoint{}
	if state != nil {
		sample = state.Sample
		proxies = append(proxies, sample.Proxies...)
		history = append(history, state.History...)
	}
	m.mu.RUnlock()
	return FrpsTargetMetrics{
		Target:          view,
		ClientCount:     sample.ClientCount,
		ProxyCount:      sample.ProxyCount,
		ConnectionCount: sample.ConnectionCount,
		TrafficIn:       sample.TrafficIn,
		TrafficOut:      sample.TrafficOut,
		TrafficInRate:   sample.TrafficInRate,
		TrafficOutRate:  sample.TrafficOutRate,
		Proxies:         proxies,
		History:         history,
	}
}

func (m *FrpsMonitor) scrapeDue(ctx context.Context, force bool) {
	if m.loadTargets == nil {
		return
	}
	targets, err := m.loadTargets(ctx)
	if err != nil {
		return
	}
	for _, target := range targets {
		if !target.Enabled {
			m.markDisabled(target.ID)
			continue
		}
		if !force && !m.shouldScrape(target) {
			continue
		}
		go m.scrapeOne(ctx, target)
	}
}

func (m *FrpsMonitor) shouldScrape(target FrpsTarget) bool {
	interval := time.Duration(target.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultFrpsIntervalSeconds * time.Second
	}
	m.mu.RLock()
	state := m.states[target.ID]
	m.mu.RUnlock()
	return state == nil || time.Since(state.LastAttemptAt) >= interval
}

func (m *FrpsMonitor) markDisabled(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureStateLocked(id)
	state.Status = "disabled"
	state.LastError = ""
}

func (m *FrpsMonitor) scrapeOne(ctx context.Context, target FrpsTarget) {
	started := time.Now()
	m.mu.Lock()
	state := m.ensureStateLocked(target.ID)
	state.LastAttemptAt = started
	m.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, frpsScrapeTimeout)
	defer cancel()
	body, err := m.fetchMetrics(callCtx, target)
	if err != nil {
		m.markError(target.ID, err)
		return
	}
	sample, err := parseFrpsMetrics(body, time.Now())
	if err != nil {
		m.markError(target.ID, err)
		return
	}
	m.storeSample(target.ID, sample)
}

func (m *FrpsMonitor) fetchMetrics(ctx context.Context, target FrpsTarget) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(target.URL, "/")+"/metrics", nil)
	if err != nil {
		return "", err
	}
	if target.Username != "" {
		req.SetBasicAuth(target.Username, target.Password)
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", errorsInvalidFrpsAuth()
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("frps metrics returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func errorsInvalidFrpsAuth() error {
	return fmt.Errorf("frps metrics authentication failed")
}

func (m *FrpsMonitor) markError(id string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureStateLocked(id)
	state.Status = "offline"
	state.LastError = err.Error()
}

func (m *FrpsMonitor) storeSample(id string, sample frpsMetricSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureStateLocked(id)
	sample = withRates(sample, state.Sample)
	state.Previous = state.Sample
	state.Sample = sample
	state.Status = "online"
	state.LastError = ""
	state.LastScrapedAt = sample.ScrapedAt.Format(time.RFC3339)
	state.History = append(state.History, FrpsTrafficPoint{
		Time:           sample.ScrapedAt.Format(time.RFC3339),
		TrafficInRate:  sample.TrafficInRate,
		TrafficOutRate: sample.TrafficOutRate,
	})
	if len(state.History) > frpsHistoryLimit {
		state.History = append([]FrpsTrafficPoint(nil), state.History[len(state.History)-frpsHistoryLimit:]...)
	}
}

func (m *FrpsMonitor) ensureStateLocked(id string) *frpsMonitorState {
	state := m.states[id]
	if state == nil {
		state = &frpsMonitorState{Status: "pending"}
		m.states[id] = state
	}
	return state
}

func targetViewFromTarget(target FrpsTarget, state *frpsMonitorState) FrpsTargetView {
	status := "pending"
	lastError := ""
	lastScrapedAt := ""
	if !target.Enabled {
		status = "disabled"
	} else if state != nil {
		status = state.Status
		lastError = state.LastError
		lastScrapedAt = state.LastScrapedAt
	}
	return FrpsTargetView{
		ID:              target.ID,
		Name:            target.Name,
		URL:             target.URL,
		Username:        target.Username,
		HasPassword:     target.Password != "",
		Enabled:         target.Enabled,
		IntervalSeconds: target.IntervalSeconds,
		Status:          status,
		LastError:       lastError,
		LastScrapedAt:   lastScrapedAt,
		CreatedAt:       target.CreatedAt,
		UpdatedAt:       target.UpdatedAt,
	}
}

func withRates(current, previous frpsMetricSample) frpsMetricSample {
	if previous.ScrapedAt.IsZero() || current.ScrapedAt.Sub(previous.ScrapedAt) <= 0 {
		return current
	}
	seconds := current.ScrapedAt.Sub(previous.ScrapedAt).Seconds()
	current.TrafficInRate = counterRate(current.TrafficIn, previous.TrafficIn, seconds)
	current.TrafficOutRate = counterRate(current.TrafficOut, previous.TrafficOut, seconds)

	previousByKey := map[string]FrpsProxyMetric{}
	for _, proxy := range previous.Proxies {
		previousByKey[frpsProxyKey(proxy.Type, proxy.Name)] = proxy
	}
	for i := range current.Proxies {
		prev := previousByKey[frpsProxyKey(current.Proxies[i].Type, current.Proxies[i].Name)]
		current.Proxies[i].TrafficInRate = counterRate(current.Proxies[i].TrafficIn, prev.TrafficIn, seconds)
		current.Proxies[i].TrafficOutRate = counterRate(current.Proxies[i].TrafficOut, prev.TrafficOut, seconds)
	}
	return current
}

func counterRate(current, previous int64, seconds float64) float64 {
	if seconds <= 0 || current < previous {
		return 0
	}
	return float64(current-previous) / seconds
}

func parseFrpsMetrics(text string, scrapedAt time.Time) (frpsMetricSample, error) {
	sample := frpsMetricSample{ScrapedAt: scrapedAt}
	proxies := map[string]*FrpsProxyMetric{}
	var proxyCountByType, detailedProxyCount int
	var totalConnectionsSet, totalTrafficInSet, totalTrafficOutSet bool

	for _, line := range strings.Split(text, "\n") {
		metric, ok := parsePrometheusLine(line)
		if !ok {
			continue
		}
		switch metric.Name {
		case "frp_server_client_counts":
			sample.ClientCount = int(metric.Value)
		case "frp_server_proxy_counts":
			if metric.Labels["name"] == "" && metric.Labels["proxy_name"] == "" {
				proxyCountByType += int(metric.Value)
			}
		case "frp_server_proxy_counts_detailed":
			if metric.Value > 0 {
				detailedProxyCount++
			}
			proxy := proxyMetric(proxies, metric.Labels)
			proxy.Type = labelValue(metric.Labels, "type", "proxy_type")
		case "frp_server_connection_counts":
			if hasProxyLabels(metric.Labels) {
				proxyMetric(proxies, metric.Labels).ConnectionCount = int(metric.Value)
			} else {
				totalConnectionsSet = true
				sample.ConnectionCount = int(metric.Value)
			}
		case "frp_server_traffic_in":
			if hasProxyLabels(metric.Labels) {
				proxyMetric(proxies, metric.Labels).TrafficIn = int64(metric.Value)
			} else {
				totalTrafficInSet = true
				sample.TrafficIn = int64(metric.Value)
			}
		case "frp_server_traffic_out":
			if hasProxyLabels(metric.Labels) {
				proxyMetric(proxies, metric.Labels).TrafficOut = int64(metric.Value)
			} else {
				totalTrafficOutSet = true
				sample.TrafficOut = int64(metric.Value)
			}
		}
	}

	for _, proxy := range proxies {
		sample.Proxies = append(sample.Proxies, *proxy)
		if !totalConnectionsSet {
			sample.ConnectionCount += proxy.ConnectionCount
		}
		if !totalTrafficInSet {
			sample.TrafficIn += proxy.TrafficIn
		}
		if !totalTrafficOutSet {
			sample.TrafficOut += proxy.TrafficOut
		}
	}
	if sample.Proxies == nil {
		sample.Proxies = []FrpsProxyMetric{}
	}
	sort.Slice(sample.Proxies, func(i, j int) bool {
		left := sample.Proxies[i].TrafficIn + sample.Proxies[i].TrafficOut
		right := sample.Proxies[j].TrafficIn + sample.Proxies[j].TrafficOut
		if left == right {
			return sample.Proxies[i].Name < sample.Proxies[j].Name
		}
		return left > right
	})
	if detailedProxyCount > 0 {
		sample.ProxyCount = detailedProxyCount
	} else if proxyCountByType > 0 {
		sample.ProxyCount = proxyCountByType
	} else {
		sample.ProxyCount = len(sample.Proxies)
	}
	return sample, nil
}

func parsePrometheusLine(line string) (prometheusSample, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return prometheusSample{}, false
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return prometheusSample{}, false
	}
	nameAndLabels := fields[0]
	value, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return prometheusSample{}, false
	}
	name := nameAndLabels
	labels := map[string]string{}
	if start := strings.IndexByte(nameAndLabels, '{'); start >= 0 && strings.HasSuffix(nameAndLabels, "}") {
		name = nameAndLabels[:start]
		labels = parsePrometheusLabels(nameAndLabels[start+1 : len(nameAndLabels)-1])
	}
	return prometheusSample{Name: name, Labels: labels, Value: value}, true
}

func parsePrometheusLabels(raw string) map[string]string {
	labels := map[string]string{}
	for _, part := range splitPrometheusLabels(raw) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		labels[key] = value
	}
	return labels
}

func splitPrometheusLabels(raw string) []string {
	var parts []string
	var b strings.Builder
	escaped := false
	inQuote := false
	for _, r := range raw {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			b.WriteRune(r)
			escaped = true
		case r == '"':
			b.WriteRune(r)
			inQuote = !inQuote
		case r == ',' && !inQuote:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	parts = append(parts, b.String())
	return parts
}

func proxyMetric(proxies map[string]*FrpsProxyMetric, labels map[string]string) *FrpsProxyMetric {
	name := labelValue(labels, "name", "proxy_name")
	typ := labelValue(labels, "type", "proxy_type")
	key := frpsProxyKey(typ, name)
	proxy := proxies[key]
	if proxy == nil {
		proxy = &FrpsProxyMetric{Name: name, Type: typ}
		proxies[key] = proxy
	}
	return proxy
}

func hasProxyLabels(labels map[string]string) bool {
	return labelValue(labels, "name", "proxy_name") != "" || labelValue(labels, "type", "proxy_type") != ""
}

func labelValue(labels map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(labels[key]); value != "" {
			return value
		}
	}
	return ""
}

func frpsProxyKey(typ, name string) string {
	return typ + "\x00" + name
}
