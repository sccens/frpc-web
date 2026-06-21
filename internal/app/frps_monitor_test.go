package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestParseFrpsMetricsAndRates(t *testing.T) {
	firstText := `
# HELP frp_server_client_counts client counts
frp_server_client_counts 2
frp_server_proxy_counts{type="tcp"} 2
frp_server_proxy_counts_detailed{name="ssh",type="tcp"} 1
frp_server_proxy_counts_detailed{name="web",type="http"} 1
frp_server_connection_counts{name="ssh",type="tcp"} 3
frp_server_connection_counts{name="web",type="http"} 2
frp_server_traffic_in{name="ssh",type="tcp"} 100
frp_server_traffic_out{name="ssh",type="tcp"} 200
frp_server_traffic_in{name="web",type="http"} 50
frp_server_traffic_out{name="web",type="http"} 70
`
	secondText := `
frp_server_client_counts 3
frp_server_proxy_counts{type="tcp"} 2
frp_server_proxy_counts_detailed{name="ssh",type="tcp"} 1
frp_server_proxy_counts_detailed{name="web",type="http"} 1
frp_server_connection_counts{name="ssh",type="tcp"} 4
frp_server_connection_counts{name="web",type="http"} 2
frp_server_traffic_in{name="ssh",type="tcp"} 220
frp_server_traffic_out{name="ssh",type="tcp"} 260
frp_server_traffic_in{name="web",type="http"} 90
frp_server_traffic_out{name="web",type="http"} 110
`
	first, err := parseFrpsMetrics(firstText, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("parse first: %v", err)
	}
	second, err := parseFrpsMetrics(secondText, time.Unix(110, 0))
	if err != nil {
		t.Fatalf("parse second: %v", err)
	}
	rated := withRates(second, first)
	if rated.ClientCount != 3 || rated.ProxyCount != 2 || rated.ConnectionCount != 6 {
		t.Fatalf("unexpected counters: %#v", rated)
	}
	if rated.TrafficIn != 310 || rated.TrafficOut != 370 {
		t.Fatalf("unexpected traffic totals: in=%d out=%d", rated.TrafficIn, rated.TrafficOut)
	}
	if rated.TrafficInRate != 16 || rated.TrafficOutRate != 10 {
		t.Fatalf("unexpected rates: in=%v out=%v", rated.TrafficInRate, rated.TrafficOutRate)
	}
	if len(rated.Proxies) != 2 || rated.Proxies[0].Name != "ssh" {
		t.Fatalf("unexpected proxy ordering: %#v", rated.Proxies)
	}
	if rated.Proxies[0].TrafficInRate != 12 || rated.Proxies[0].TrafficOutRate != 6 {
		t.Fatalf("unexpected proxy rates: %#v", rated.Proxies[0])
	}
}

func TestFrpsTargetCRUDMasksPasswordAndKeepsExistingPassword(t *testing.T) {
	ctx := context.Background()
	store := &updateTestStore{}
	svc := NewService(Options{Store: store, Runtime: &frpsTestRuntime{}, Addr: "127.0.0.1:8080"})

	created, err := svc.CreateFrpsTarget(ctx, FrpsTargetInput{
		Name:            "edge-frps",
		URL:             "http://127.0.0.1:7500",
		Username:        "admin",
		Password:        "secret",
		Enabled:         true,
		IntervalSeconds: 5,
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if created.ID == "" || !created.HasPassword || created.Status != "pending" {
		t.Fatalf("unexpected created view: %#v", created)
	}

	updated, err := svc.UpdateFrpsTarget(ctx, created.ID, FrpsTargetInput{
		Name:            "edge-frps-renamed",
		URL:             "http://127.0.0.1:7500/",
		Username:        "admin2",
		Password:        "",
		Enabled:         true,
		IntervalSeconds: 10,
	})
	if err != nil {
		t.Fatalf("update target: %v", err)
	}
	if updated.Name != "edge-frps-renamed" || updated.URL != "http://127.0.0.1:7500" || !updated.HasPassword {
		t.Fatalf("unexpected updated view: %#v", updated)
	}

	targets, err := svc.loadFrpsTargets(ctx)
	if err != nil {
		t.Fatalf("load targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Password != "secret" {
		t.Fatalf("existing password not retained: %#v", targets)
	}

	if err := svc.DeleteFrpsTarget(ctx, created.ID); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	targets, err = svc.loadFrpsTargets(ctx)
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if len(targets) != 0 {
		t.Fatalf("targets after delete: %#v", targets)
	}
}

func TestFrpsTargetMetricsUsesEmptySlicesBeforeFirstScrape(t *testing.T) {
	monitor := NewFrpsMonitor(nil)
	metrics := monitor.TargetMetrics(FrpsTarget{
		ID:              "frps-1",
		Name:            "edge-frps",
		URL:             "http://127.0.0.1:7500",
		Enabled:         true,
		IntervalSeconds: 5,
	})
	if metrics.Proxies == nil || metrics.History == nil {
		t.Fatalf("proxies/history should be empty slices, got proxies=%#v history=%#v", metrics.Proxies, metrics.History)
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}
	if string(raw["proxies"]) != "[]" || string(raw["history"]) != "[]" {
		t.Fatalf("proxies/history should marshal as empty arrays, got %s", data)
	}
}

type frpsTestRuntime struct{}

func (frpsTestRuntime) Logs(context.Context, string, int) ([]LogLine, error) { return nil, nil }

func (frpsTestRuntime) ProxyStatus(context.Context, Server) ([]ProxyStatus, error) {
	return nil, nil
}

func (frpsTestRuntime) Reload(context.Context, Server) error { return nil }
