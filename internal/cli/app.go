package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/YingmoY/Apparition/internal/core"
)

const defaultLogFileMode = 0644

type Options struct {
	Command    string
	ConfigPath string
	CookiePath string
	LogPath    string
}

func Run(args []string) int {
	options, err := Parse(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	cleanup, err := configureLogging(options.LogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置日志失败: %v\n", err)
		return 1
	}
	defer cleanup()

	service, err := core.NewService(options.ConfigPath, options.CookiePath)
	if err != nil {
		log.Printf("加载配置失败: %v", err)
		return 1
	}

	switch options.Command {
	case "login":
		if err := service.Login(); err != nil {
			log.Printf("登录失败: %v", err)
			return 1
		}
	case "test":
		if err := service.EnsureCookies(); err != nil {
			log.Print(err)
			return 1
		}
		if err := service.TestAPI(); err != nil {
			log.Printf("测试失败: %v", err)
			return 1
		}
	default:
		if err := service.EnsureCookies(); err != nil {
			log.Print(err)
			return 1
		}
		result := service.RunClockIn()
		fmt.Printf("执行结果: Success=%v, Message=%s\n", result.Success, result.Message)
		if !result.Success {
			return 1
		}
	}

	return 0
}

func Parse(args []string) (Options, error) {
	options := Options{
		Command:    "run",
		ConfigPath: core.DefaultConfigPath,
	}

	if len(args) > 0 {
		switch args[0] {
		case "run", "login", "test":
			options.Command = args[0]
			args = args[1:]
		case "help":
			return Options{}, fmt.Errorf(usage())
		}
	}

	var parseOutput bytes.Buffer
	flagSet := flag.NewFlagSet(options.Command, flag.ContinueOnError)
	flagSet.SetOutput(&parseOutput)
	flagSet.StringVar(&options.ConfigPath, "config", core.DefaultConfigPath, "配置文件路径")
	flagSet.StringVar(&options.CookiePath, "cookie", "", "Cookie 文件路径，默认读取配置中的 cookie_file_path")
	flagSet.StringVar(&options.LogPath, "log", "", "日志文件路径，留空则仅输出到控制台")

	if err := flagSet.Parse(args); err != nil {
		message := strings.TrimSpace(parseOutput.String())
		if message == "" {
			message = err.Error()
		}
		return Options{}, fmt.Errorf("%s\n\n%s", message, usage())
	}

	if flagSet.NArg() > 0 {
		return Options{}, fmt.Errorf("存在未识别参数: %s\n\n%s", strings.Join(flagSet.Args(), " "), usage())
	}

	return options, nil
}

func usage() string {
	return strings.TrimSpace(`用法:
  apparition [run|login|test] [--config path] [--cookie path] [--log path]

命令:
  run    执行签到，默认命令
  login  强制扫码登录并更新 Cookie
  test   校验 Cookie 后执行接口测试

参数:
  --config  配置文件路径，默认 config.json
  --cookie  Cookie 文件路径，默认读取配置中的 cookie_file_path
  --log     日志输出文件路径，例如 logs/apparition.log`)
}

func configureLogging(logPath string) (func(), error) {
	log.SetFlags(log.LstdFlags)
	log.SetOutput(os.Stdout)

	if strings.TrimSpace(logPath) == "" {
		return func() {}, nil
	}

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

	log.SetOutput(io.MultiWriter(os.Stdout, file))
	log.Printf("日志输出已写入 %s", cleanPath)

	return func() {
		log.SetOutput(os.Stdout)
		_ = file.Close()
	}, nil
}
