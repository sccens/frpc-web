package frpc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/sccens/frpc-web/internal/app"
)

// DiscoverBinaries 扫描 PATH 与常见安装目录，找出系统中已存在、可直接登记使用的
// frpc 二进制（apt、官方安装脚本、手动放置等）。每个候选都会以 `--version`
// 验证确实是 frpc。位于本面板受管目录内的二进制会被标注 Managed。
func (r *Runtime) DiscoverBinaries() []app.FRPCBinaryCandidate {
	managedRoot, _ := filepath.Abs(filepath.Join(r.dataDir, "bin"))
	seen := map[string]bool{}
	out := []app.FRPCBinaryCandidate{}
	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		if seen[abs] {
			return
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return
		}
		version, err := binaryVersion(abs)
		if err != nil {
			return
		}
		seen[abs] = true
		out = append(out, app.FRPCBinaryCandidate{
			Path:    abs,
			Version: version,
			Managed: managedRoot != "" && pathWithin(managedRoot, abs),
		})
	}

	// 优先扫描 frpc-web 管理的 bin 目录
	if managedRoot != "" {
		if entries, err := os.ReadDir(managedRoot); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasPrefix(entry.Name(), "frpc") {
					add(filepath.Join(managedRoot, entry.Name()))
				}
			}
		}
	}

	for _, dir := range candidateBinaryDirs() {
		add(filepath.Join(dir, binaryName()))
	}
	return out
}

// RegisterBinary 校验给定路径确为可用的 frpc 二进制，并返回对应的版本记录
// （Source="system"，Path 指向该二进制原地，Start 时直接 exec 它）。
func (r *Runtime) RegisterBinary(path string) (app.FRPCVersion, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return app.FRPCVersion{}, errors.New("binary path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return app.FRPCVersion{}, err
	}
	if info.IsDir() {
		return app.FRPCVersion{}, errors.New("路径是一个目录")
	}
	if info.Mode()&0o111 == 0 {
		return app.FRPCVersion{}, errors.New("文件没有可执行权限")
	}
	version, err := binaryVersion(abs)
	if err != nil {
		return app.FRPCVersion{}, fmt.Errorf("不是有效的 frpc 二进制: %w", err)
	}
	return app.FRPCVersion{
		Version:   version,
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
		Path:      abs,
		Source:    "system",
		Installed: true,
	}, nil
}

// DiscoverProcesses 列出当前正在运行的 frpc 进程（含本面板拉起的，由上层按 PID
// 标注 Managed）。Linux 读 /proc，其余平台退回 ps。
func (r *Runtime) DiscoverProcesses() ([]app.FRPCProcessCandidate, error) {
	if runtime.GOOS == "linux" {
		if procs, err := discoverProcessesProc(); err == nil {
			return procs, nil
		}
	}
	return discoverProcessesPS(), nil
}

func discoverProcessesProc() ([]app.FRPCProcessCandidate, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	out := []app.FRPCProcessCandidate{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := splitNUL(string(raw))
		if len(args) == 0 || !isFrpcArgv0(args[0]) {
			continue
		}
		exe, _ := os.Readlink(filepath.Join("/proc", entry.Name(), "exe"))
		configPath := configPathFromArgs(args)

		// 检测 systemd 管理
		systemdManaged, systemdUnit := isSystemdManaged(pid)

		// 检测 admin API 配置
		hasAdminAPI, adminAddr := checkAdminAPI(configPath)

		out = append(out, app.FRPCProcessCandidate{
			PID:             pid,
			Exe:             exe,
			ConfigPath:      configPath,
			SystemdManaged:  systemdManaged,
			SystemdUnit:     systemdUnit,
			HasAdminAPI:     hasAdminAPI,
			AdminAPIAddress: adminAddr,
		})
	}
	return out, nil
}

func discoverProcessesPS() []app.FRPCProcessCandidate {
	// -ww 避免命令行被宽度截断；输出 "pid args..."。
	output, err := exec.Command("ps", "-axww", "-o", "pid=,args=").Output()
	if err != nil {
		return []app.FRPCProcessCandidate{}
	}
	self := os.Getpid()
	out := []app.FRPCProcessCandidate{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == self {
			continue
		}
		args := fields[1:]
		if !isFrpcArgv0(args[0]) {
			continue
		}
		exe := args[0]
		if !filepath.IsAbs(exe) {
			exe = ""
		}
		out = append(out, app.FRPCProcessCandidate{
			PID:        pid,
			Exe:        exe,
			ConfigPath: configPathFromArgs(args),
		})
	}
	return out
}

// isFrpcArgv0 判断 argv[0] 是否是 frpc 本体（排除 frpc-web 自身与 frps）。
func isFrpcArgv0(argv0 string) bool {
	base := filepath.Base(strings.TrimSpace(argv0))
	return base == "frpc" || base == "frpc.exe"
}

// configPathFromArgs 从命令行参数中提取 -c/--config 指定的配置路径。
func configPathFromArgs(args []string) string {
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "-c" || args[i] == "--config":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(args[i], "-c="):
			return strings.TrimPrefix(args[i], "-c=")
		case strings.HasPrefix(args[i], "--config="):
			return strings.TrimPrefix(args[i], "--config=")
		}
	}
	return ""
}

func splitNUL(s string) []string {
	out := []string{}
	for _, part := range strings.Split(s, "\x00") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func candidateBinaryDirs() []string {
	var dirs []string
	if p := os.Getenv("PATH"); p != "" {
		dirs = append(dirs, filepath.SplitList(p)...)
	}
	dirs = append(dirs,
		"/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin",
		"/opt/frp", "/usr/local/frp", "/opt/frpc",
	)
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	return dirs
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "frpc.exe"
	}
	return "frpc"
}

// pathWithin 报告 target 是否位于 dir 目录内（两者均应为绝对路径）。
func pathWithin(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// isSystemdManaged 检测进程是否由 systemd 托管
func isSystemdManaged(pid int) (bool, string) {
	if runtime.GOOS != "linux" {
		return false, ""
	}

	// 方法1：检查 cgroup 是否包含 systemd
	cgroupPath := filepath.Join("/proc", strconv.Itoa(pid), "cgroup")
	if data, err := os.ReadFile(cgroupPath); err == nil {
		content := string(data)
		if strings.Contains(content, "systemd") || strings.Contains(content, ".service") {
			// 尝试提取服务单元名称
			for _, line := range strings.Split(content, "\n") {
				if strings.Contains(line, ".service") {
					// 格式类似：0::/system.slice/frpc.service
					parts := strings.Split(line, "/")
					for _, part := range parts {
						if strings.HasSuffix(part, ".service") {
							return true, part
						}
					}
					return true, ""
				}
			}
			return true, ""
		}
	}

	// 方法2：尝试用 systemctl status PID 检查（需要 systemctl 可用）
	if _, err := exec.LookPath("systemctl"); err == nil {
		cmd := exec.Command("systemctl", "status", strconv.Itoa(pid))
		if output, err := cmd.CombinedOutput(); err == nil {
			content := string(output)
			// 查找类似 "● frpc.service" 的行
			for _, line := range strings.Split(content, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "●") && strings.HasSuffix(line, ".service") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						return true, parts[1]
					}
				}
			}
		}
	}

	return false, ""
}

// checkAdminAPI 解析配置文件，检查是否配置了 admin API（webServer）
func checkAdminAPI(configPath string) (bool, string) {
	if configPath == "" {
		return false, ""
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return false, ""
	}

	text := string(content)

	// 检查 TOML 格式的 webServer 配置
	if strings.Contains(text, "[webServer]") || strings.Contains(text, "webServer.") {
		// 尝试提取地址和端口
		var addr, port string
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "webServer.addr") || strings.HasPrefix(line, "addr") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					addr = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				}
			}
			if strings.HasPrefix(line, "webServer.port") || strings.HasPrefix(line, "port") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					port = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				}
			}
		}
		if addr == "" {
			addr = "127.0.0.1"
		}
		if port == "" {
			port = "7400"
		}
		return true, addr + ":" + port
	}

	// 检查 INI 格式的 admin_addr 和 admin_port
	if strings.Contains(text, "admin_addr") || strings.Contains(text, "admin_port") {
		var addr, port string
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "admin_addr") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					addr = strings.TrimSpace(parts[1])
				}
			}
			if strings.HasPrefix(line, "admin_port") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					port = strings.TrimSpace(parts[1])
				}
			}
		}
		if addr == "" {
			addr = "127.0.0.1"
		}
		if port == "" {
			port = "7400"
		}
		return true, addr + ":" + port
	}

	return false, ""
}

