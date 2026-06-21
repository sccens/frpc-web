package frpc

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sccens/frpc-web/internal/app"
)

// Runtime 在 v2.0 仅承载两类只读能力：读取 frpc 日志文件（Logs），以及访问 frpc
// admin API（ProxyStatus / Reload，见 admin.go）。进程的启停与配置渲染已移交
// systemd 与面板的「原文编辑」——Runtime 不再持有任何进程状态。
type Runtime struct {
	dataDir string
}

// New 保留 dataDir 入参以兼容历史调用方；当前不再用于推断路径。
func New(dataDir string) *Runtime {
	if dataDir == "" {
		dataDir = "frpc-web-data"
	}
	return &Runtime{dataDir: dataDir}
}

// maxLogTail 限制单次日志请求返回的行数上限，防止异常大的 tail 参数。
const maxLogTail = 5000

// Logs 读取指定日志文件的末尾 tail 行。logPath 通常来自配置文件的 log.to；
// 为空或文件不存在时返回空列表。
func (r *Runtime) Logs(_ context.Context, logPath string, tail int) ([]app.LogLine, error) {
	if strings.TrimSpace(logPath) == "" {
		return []app.LogLine{}, nil
	}
	lines, err := tailLines(logPath, tail)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []app.LogLine{}, nil
		}
		return nil, err
	}
	result := make([]app.LogLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, parseLogLine(line))
	}
	return result, nil
}

func tailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		n = 200
	}
	if n > maxLogTail {
		n = maxLogTail
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return []string{}, nil
	}

	const blockSize int64 = 8192
	var (
		pos       = info.Size()
		remainder string
		lines     []string
	)
	for pos > 0 && len(lines) <= n {
		readSize := blockSize
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		buf := make([]byte, readSize)
		if _, err := file.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		chunk := string(buf) + remainder
		parts := strings.Split(chunk, "\n")
		remainder = parts[0]
		for i := len(parts) - 1; i >= 1; i-- {
			if parts[i] == "" && pos+readSize == info.Size() && len(lines) == 0 {
				continue
			}
			lines = append(lines, parts[i])
			if len(lines) >= n {
				break
			}
		}
	}
	if len(lines) < n && remainder != "" {
		lines = append(lines, remainder)
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// parseLogLine 从 frpc 日志行中提取真实时间戳（格式 2006/01/02 15:04:05，
// 可带毫秒）。解析不出时间时 Time 留空，避免用请求时刻冒充日志时间。
func parseLogLine(line string) app.LogLine {
	entry := app.LogLine{Level: "info", Message: line}
	if len(line) >= 19 {
		if ts, err := time.Parse("2006/01/02 15:04:05", line[:19]); err == nil {
			entry.Time = ts.Format("15:04:05")
			rest := line[19:]
			if len(rest) > 1 && rest[0] == '.' {
				i := 1
				for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
					i++
				}
				rest = rest[i:]
			}
			entry.Message = strings.TrimSpace(rest)
		}
	}
	lower := strings.ToLower(entry.Message)
	switch {
	case strings.Contains(entry.Message, "[E]") || strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
		entry.Level = "error"
	case strings.Contains(entry.Message, "[W]") || strings.Contains(lower, "warn"):
		entry.Level = "warn"
	}
	return entry
}
