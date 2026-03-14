package server

import (
	"fmt"
	"os"
	"path/filepath"
)

type RuntimePaths struct {
	BaseDir    string
	DataDir    string
	ConfigPath string
	DBPath     string
	LogDir     string
	LogPath    string
}

func resolveRuntimePaths() (RuntimePaths, error) {
	exe, err := os.Executable()
	if err != nil {
		return RuntimePaths{}, fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	baseDir := filepath.Dir(exe)
	dataDir := filepath.Join(baseDir, "data")

	return RuntimePaths{
		BaseDir:    baseDir,
		DataDir:    dataDir,
		ConfigPath: filepath.Join(dataDir, "server-config.json"),
		DBPath:     filepath.Join(dataDir, "data.db"),
		LogDir:     filepath.Join(dataDir, "logs"),
		LogPath:    filepath.Join(dataDir, "logs", "server.log"),
	}, nil
}

func ensureRuntimeDirectories(paths RuntimePaths) error {
	dirs := []string{paths.DataDir, paths.LogDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return nil
}
