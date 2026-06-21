package frpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/sccens/frpc-web/internal/app"
)

// adminClient 访问本机回环地址上的 frpc admin API（webServer）。
var adminClient = &http.Client{Timeout: 3 * time.Second}

// adminProxyStatus 对应 frp client /api/status 响应中的单条 proxy 状态，
// 字段自 v0.31 起保持稳定（v0.69 仅追加了 source 字段，此处不需要）。
type adminProxyStatus struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Err        string `json:"err"`
	LocalAddr  string `json:"local_addr"`
	Plugin     string `json:"plugin"`
	RemoteAddr string `json:"remote_addr"`
}

// ProxyStatus 调用 frpc admin API 的 GET /api/status，返回按名称排序的 proxy 实时状态。
func (r *Runtime) ProxyStatus(ctx context.Context, server app.Server) ([]app.ProxyStatus, error) {
	addr := server.AdminAddr
	if addr == "" {
		addr = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s/api/status", net.JoinHostPort(addr, strconv.Itoa(server.AdminPort)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if server.AdminUser != "" {
		req.SetBasicAuth(server.AdminUser, server.AdminPassword)
	}
	resp, err := adminClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("frpc 管理接口不可达: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("frpc 管理接口认证失败，请检查服务端的管理账号设置")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("frpc 管理接口返回 %s", resp.Status)
	}

	// 响应按 proxy 类型分组：{"tcp":[...],"http":[...]}，这里拍平成单个列表。
	var grouped map[string][]adminProxyStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&grouped); err != nil {
		return nil, fmt.Errorf("解析 frpc 管理接口响应失败: %w", err)
	}
	statuses := make([]app.ProxyStatus, 0, len(grouped)*2)
	for _, group := range grouped {
		for _, item := range group {
			statuses = append(statuses, app.ProxyStatus{
				Name:       item.Name,
				Type:       item.Type,
				Phase:      item.Status,
				Err:        item.Err,
				LocalAddr:  item.LocalAddr,
				Plugin:     item.Plugin,
				RemoteAddr: item.RemoteAddr,
			})
		}
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	return statuses, nil
}

// Reload 调用 frpc admin API 的 GET /api/reload，让 frpc 重读其启动时（-c）指定的
// 配置文件。注意：它重载的是 frpc 进程自己的配置文件路径，不是本面板扫描到的路径；
// 若两者不一致，reload 不会报错也不会生效，调用方需自行对齐校验。
func (r *Runtime) Reload(ctx context.Context, server app.Server) error {
	addr := server.AdminAddr
	if addr == "" {
		addr = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s/api/reload", net.JoinHostPort(addr, strconv.Itoa(server.AdminPort)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if server.AdminUser != "" {
		req.SetBasicAuth(server.AdminUser, server.AdminPassword)
	}
	resp, err := adminClient.Do(req)
	if err != nil {
		return fmt.Errorf("frpc 管理接口不可达: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("frpc 管理接口认证失败，请检查配置中的 webServer 账号")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("frpc 管理接口返回 %s", resp.Status)
	}
	return nil
}
