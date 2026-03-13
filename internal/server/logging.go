package server

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const defaultLogFileMode = 0644

func configureLogging(logPath string) (func(), error) {
	cleanPath := filepath.Clean(logPath)
	if dir := filepath.Dir(cleanPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}
	}

	file, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, defaultLogFileMode)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件失败: %w", err)
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetOutput(io.MultiWriter(os.Stdout, file))

	return func() {
		log.SetOutput(os.Stdout)
		_ = file.Close()
	}, nil
}
