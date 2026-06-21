package app

import (
	"context"
	"sync"
	"time"
)

// proxyStatusTimeout 限制单个 frpc admin API 请求时长，避免个别实例卡住拖慢整体轮询。
const proxyStatusTimeout = 3 * time.Second

// ProxiesStatus 并发收集所有 frpc 实例的 proxy 实时状态（供拓扑页轮询）。
// 进程未运行或 admin API 不可达不构成整体失败，按服务器逐个返回。
func (s *Service) ProxiesStatus(ctx context.Context) ([]ServerProxyStatus, error) {
	servers := s.scanner.Servers()
	results := make([]ServerProxyStatus, len(servers))
	var wg sync.WaitGroup
	for i, server := range servers {
		results[i] = ServerProxyStatus{
			ServerID: server.ID,
			Proxies:  []ProxyStatus{},
		}
		if server.AdminPort == 0 {
			results[i].Error = "未配置 admin API"
			continue
		}
		wg.Add(1)
		go func(i int, server Server) {
			defer wg.Done()
			callCtx, cancel := context.WithTimeout(ctx, proxyStatusTimeout)
			defer cancel()
			proxies, err := s.runtime.ProxyStatus(callCtx, server)
			if err != nil {
				results[i].Error = err.Error()
				return
			}
			results[i].Running = true
			results[i].Proxies = proxies
		}(i, server)
	}
	wg.Wait()
	return results, nil
}
