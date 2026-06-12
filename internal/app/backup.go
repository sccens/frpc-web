package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const (
	settingAutoBackupEnabled  = "auto_backup_enabled"
	settingAutoBackupInterval = "auto_backup_interval_hours"
	settingAutoBackupMaxFiles = "auto_backup_max_files"
	// last_run 记录最近一次调度检查（含因内容未变而跳过的），用于计算下次备份时间；
	// last_at 只记录真正写出文件的时刻，用于界面展示。
	settingAutoBackupLastRun  = "auto_backup_last_run"
	settingAutoBackupLastAt   = "auto_backup_last_at"
	settingAutoBackupLastHash = "auto_backup_last_hash"
)

const (
	defaultAutoBackupIntervalHours = 24
	maxAutoBackupIntervalHours     = 24 * 30
	defaultAutoBackupMaxFiles      = 7
	maxAutoBackupMaxFiles          = 100
	// 调度循环按分钟检查设置，而不是按设定间隔休眠，间隔修改无需重启即可生效。
	autoBackupCheckInterval = time.Minute
	backupDirName           = "backups"
)

var backupNameRe = regexp.MustCompile(`^auto-backup-\d{8}-\d{6}(-\d+)?\.json$`)

// RunAutoBackup 阻塞运行自动备份调度循环，直到 ctx 取消。
func (s *Service) RunAutoBackup(ctx context.Context) {
	ticker := time.NewTicker(autoBackupCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.AutoBackupCheck(ctx)
		}
	}
}

// AutoBackupCheck 在自动备份启用且距上次检查超过设定间隔时执行一次备份；
// 备份失败记入健康事件，不中断调度。
func (s *Service) AutoBackupCheck(ctx context.Context) {
	settings, err := s.Settings(ctx)
	if err != nil || !settings.AutoBackupEnabled {
		return
	}
	lastRun := s.settingString(ctx, settingAutoBackupLastRun)
	if last, err := time.Parse(time.RFC3339, lastRun); err == nil {
		if time.Since(last) < time.Duration(settings.AutoBackupIntervalHours)*time.Hour {
			return
		}
	}
	if _, err := s.performBackup(ctx, false, settings.AutoBackupMaxFiles); err != nil {
		_ = s.store.AddHealth(ctx, "", "warning", "自动备份失败: "+err.Error())
	}
}

// BackupNow 立即写出一份备份，不做内容去重。
func (s *Service) BackupNow(ctx context.Context) (BackupFile, error) {
	settings, err := s.Settings(ctx)
	if err != nil {
		return BackupFile{}, err
	}
	return s.performBackup(ctx, true, settings.AutoBackupMaxFiles)
}

// performBackup 导出配置写入备份目录并清理超量旧备份。
// force 为 false 时与上一次备份内容一致则跳过，返回零值 BackupFile。
func (s *Service) performBackup(ctx context.Context, force bool, maxFiles int) (BackupFile, error) {
	bundle, err := s.ExportConfig(ctx)
	if err != nil {
		return BackupFile{}, err
	}
	hash := bundleHash(bundle)
	now := time.Now()
	_ = s.store.SetSetting(ctx, settingAutoBackupLastRun, now.Format(time.RFC3339))
	if !force && hash != "" {
		if lastHash := s.settingString(ctx, settingAutoBackupLastHash); lastHash == hash {
			return BackupFile{}, nil
		}
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return BackupFile{}, err
	}
	dir := s.backupDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BackupFile{}, err
	}
	name, err := nextBackupName(dir, now)
	if err != nil {
		return BackupFile{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		return BackupFile{}, err
	}
	_ = s.store.SetSetting(ctx, settingAutoBackupLastAt, now.Format(time.RFC3339))
	_ = s.store.SetSetting(ctx, settingAutoBackupLastHash, hash)
	s.pruneBackups(dir, maxFiles)
	return BackupFile{Name: name, Size: int64(len(data)), CreatedAt: now.Format(time.RFC3339)}, nil
}

func (s *Service) ListBackups(ctx context.Context) ([]BackupFile, error) {
	entries, err := readBackupEntries(s.backupDir())
	if err != nil {
		return nil, err
	}
	files := make([]BackupFile, 0, len(entries))
	for _, entry := range entries {
		files = append(files, BackupFile{
			Name:      entry.name,
			Size:      entry.size,
			CreatedAt: entry.modTime.Format(time.RFC3339),
		})
	}
	return files, nil
}

// ReadBackup 返回指定备份文件内容；文件名先经白名单校验，杜绝路径穿越。
func (s *Service) ReadBackup(ctx context.Context, name string) ([]byte, error) {
	if !backupNameRe.MatchString(name) {
		return nil, invalidInput(errors.New("invalid backup file name"))
	}
	data, err := os.ReadFile(filepath.Join(s.backupDir(), name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

func (s *Service) RestoreBackup(ctx context.Context, name string, mode string) (ActionResult, error) {
	data, err := s.ReadBackup(ctx, name)
	if err != nil {
		return ActionResult{}, err
	}
	var bundle ConfigBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return ActionResult{}, invalidInput(fmt.Errorf("备份文件解析失败: %v", err))
	}
	return s.ImportConfig(ctx, ConfigImportInput{Mode: mode, Bundle: bundle})
}

func (s *Service) backupDir() string {
	return filepath.Join(s.store.DataDir(), backupDirName)
}

// bundleHash 计算导出内容指纹。ExportedAt 每次导出必然不同，参与哈希会让去重失效，置空后再哈希。
func bundleHash(bundle ConfigBundle) string {
	bundle.ExportedAt = ""
	data, err := json.Marshal(bundle)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// nextBackupName 生成带时间戳的文件名；同一秒内多次备份追加序号避免覆盖。
func nextBackupName(dir string, now time.Time) (string, error) {
	base := "auto-backup-" + now.Format("20060102-150405")
	name := base + ".json"
	for i := 1; ; i++ {
		_, err := os.Stat(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
		if i > 99 {
			return "", errors.New("too many backups in one second")
		}
		name = fmt.Sprintf("%s-%d.json", base, i)
	}
}

// pruneBackups 按修改时间保留最新 maxFiles 个备份，只处理符合命名规则的文件。
func (s *Service) pruneBackups(dir string, maxFiles int) {
	if maxFiles < 1 {
		maxFiles = 1
	}
	entries, err := readBackupEntries(dir)
	if err != nil || len(entries) <= maxFiles {
		return
	}
	for _, entry := range entries[maxFiles:] {
		_ = os.Remove(filepath.Join(dir, entry.name))
	}
}

type backupEntry struct {
	name    string
	size    int64
	modTime time.Time
}

// readBackupEntries 列出备份目录中符合命名规则的文件，按修改时间从新到旧排序。
func readBackupEntries(dir string) ([]backupEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []backupEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	entries := make([]backupEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if entry.IsDir() || !backupNameRe.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entries = append(entries, backupEntry{name: entry.Name(), size: info.Size(), modTime: info.ModTime()})
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].modTime.Equal(entries[j].modTime) {
			return entries[i].modTime.After(entries[j].modTime)
		}
		return entries[i].name > entries[j].name
	})
	return entries, nil
}
