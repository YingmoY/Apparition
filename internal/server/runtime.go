package server

import (
	"fmt"
	"os"
	"path/filepath"
)

func resolveRuntimePaths() (RuntimePaths, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return RuntimePaths{}, fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	baseDir := filepath.Dir(filepath.Clean(executablePath))
	dataDir := filepath.Join(baseDir, defaultDataDirName)
	logDir := filepath.Join(dataDir, defaultLogDirName)

	return RuntimePaths{
		BaseDir:    baseDir,
		DataDir:    dataDir,
		ConfigPath: filepath.Join(dataDir, defaultConfigFileName),
		DBPath:     filepath.Join(dataDir, defaultDBFileName),
		LogDir:     logDir,
		LogPath:    filepath.Join(logDir, defaultLogFileName),
	}, nil
}

func ensureRuntimeDirectories(paths RuntimePaths) error {
	for _, dir := range []string{paths.DataDir, paths.LogDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}
	}

	return nil
}
