package frpc

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sccens/frpc-web/internal/app"
)

func TestProxyStatusParsesAdminAPI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "frpc-web" || pass != "admin-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/status" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{
			"tcp": [{"name":"ssh","type":"tcp","status":"running","err":"","local_addr":"127.0.0.1:22","plugin":"","remote_addr":"1.2.3.4:6022"}],
			"http": [{"name":"web","type":"http","status":"start error","err":"port already used","local_addr":"127.0.0.1:8080","plugin":"","remote_addr":""}]
		}`))
	}))
	defer ts.Close()

	host, portText, err := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	if err != nil {
		t.Fatalf("parse test server addr: %v", err)
	}
	port, _ := strconv.Atoi(portText)
	server := app.Server{AdminAddr: host, AdminPort: port, AdminUser: "frpc-web", AdminPassword: "admin-secret"}

	runtime := New(t.TempDir())
	statuses, err := runtime.ProxyStatus(context.Background(), server)
	if err != nil {
		t.Fatalf("proxy status: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("statuses = %d, want 2", len(statuses))
	}
	// 结果应按名称排序：ssh 在 web 前。
	if statuses[0].Name != "ssh" || statuses[0].Phase != "running" || statuses[0].RemoteAddr != "1.2.3.4:6022" {
		t.Fatalf("unexpected first status: %#v", statuses[0])
	}
	if statuses[1].Name != "web" || statuses[1].Phase != "start error" || statuses[1].Err != "port already used" {
		t.Fatalf("unexpected second status: %#v", statuses[1])
	}

	server.AdminPassword = "wrong"
	if _, err := runtime.ProxyStatus(context.Background(), server); err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Fatalf("wrong password error = %v, want auth failure", err)
	}
}
