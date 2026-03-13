package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const defaultLoginTimeout = 120 * time.Second

type Service struct {
	ConfigPath string
	CookiePath string
	Config     Config
}

func NewService(configPath, cookieOverride string) (*Service, error) {
	cleanConfigPath := filepath.Clean(configPath)
	config, err := LoadConfig(cleanConfigPath)
	if err != nil {
		return nil, err
	}

	return &Service{
		ConfigPath: cleanConfigPath,
		CookiePath: ResolveCookiePath(cleanConfigPath, config.CookieFilePath, cookieOverride),
		Config:     config,
	}, nil
}

func (s *Service) EnsureCookies() error {
	valid, err := s.cookiesAreValid()
	if err != nil {
		log.Printf("校验 Cookie 失败: %v", err)
	}
	if valid {
		return nil
	}

	log.Println("Cookie 不存在或已过期，启动扫码登录")
	if err := s.Login(); err != nil {
		return fmt.Errorf("自动登录失败: %w", err)
	}

	log.Println("登录完成，继续执行")
	return nil
}

func (s *Service) Login() error {
	authService, err := NewWPSAuthService()
	if err != nil {
		return err
	}

	cookies, err := authService.Run(defaultLoginTimeout)
	if err != nil {
		return err
	}

	return SaveCookies(cookies, s.CookiePath)
}

func (s *Service) RunClockIn() ClockInResult {
	client, err := NewClockInClientFromFiles(s.ConfigPath, s.CookiePath)
	if err != nil {
		return ClockInResult{Success: false, Message: fmt.Sprintf("初始化失败: %v", err)}
	}

	return client.Run()
}

func (s *Service) TestAPI() error {
	client, err := NewClockInClientFromFiles(s.ConfigPath, s.CookiePath)
	if err != nil {
		return err
	}

	return client.TestAPI()
}

func (s *Service) cookiesAreValid() (bool, error) {
	if _, err := os.Stat(s.CookiePath); err != nil {
		if os.IsNotExist(err) {
			log.Println("Cookie 文件不存在")
			return false, nil
		}
		return false, err
	}

	client, err := NewClockInClientFromFiles(s.ConfigPath, s.CookiePath)
	if err != nil {
		return false, err
	}

	return client.CheckAuth(), nil
}
