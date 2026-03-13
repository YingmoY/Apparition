package core

import (
	"bytes"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	wpsLoginURL = "https://account.wps.cn/"
	wpsQRAPI    = "https://account.wps.cn/api/v3/miniprogram/code/img"
	wpsPollAPI  = "https://qr.wps.cn/api/v3/channel/wait"
	wpsUsersAPI = "https://account.wps.cn/api/v3/login/users"
	wpsLoginAPI = "https://account.wps.cn/passport/secure/api/login"
	wpsMPAppID  = "wx5b97b0686831c076"
)

type WPSAuthService struct {
	client    *http.Client
	channelID string
	csrfToken string
}

func NewWPSAuthService() (*WPSAuthService, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("创建 Cookie Jar 失败: %w", err)
	}

	return &WPSAuthService{
		client: &http.Client{
			Jar:     jar,
			Timeout: defaultHTTPTimeout,
		},
	}, nil
}

func (s *WPSAuthService) Start() (string, error) {
	log.Println("启动 WPS 登录流程")

	request, err := http.NewRequest(http.MethodGet, wpsLoginURL, nil)
	if err != nil {
		return "", err
	}
	s.setCommonHeaders(request)
	if _, err := s.client.Do(request); err != nil {
		return "", fmt.Errorf("访问登录页失败: %w", err)
	}

	s.csrfToken = generateCSRFToken()
	accountURL, _ := url.Parse("https://account.wps.cn")
	s.client.Jar.SetCookies(accountURL, []*http.Cookie{{
		Name:   "csrf",
		Value:  s.csrfToken,
		Path:   "/",
		Domain: ".wps.cn",
	}})

	qrData, err := json.Marshal(map[string]any{
		"qrShowAgreement": true,
		"keeponline":      1,
		"from":            "",
	})
	if err != nil {
		return "", fmt.Errorf("序列化二维码请求失败: %w", err)
	}

	qrURL, _ := url.Parse(wpsQRAPI)
	query := qrURL.Query()
	query.Set("action", "verify")
	query.Set("mpappid", wpsMPAppID)
	query.Set("data", string(qrData))
	query.Set("_", strconv.FormatInt(time.Now().UnixMilli(), 10))
	qrURL.RawQuery = query.Encode()

	request, err = http.NewRequest(http.MethodGet, qrURL.String(), nil)
	if err != nil {
		return "", err
	}
	s.setCommonHeaders(request)
	resp, err := s.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("获取二维码失败: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Result    string `json:"result"`
		ChannelID string `json:"channel_id"`
		URL       string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析二维码响应失败: %w", err)
	}
	if result.Result != "ok" {
		return "", fmt.Errorf("获取二维码失败: %+v", result)
	}

	s.channelID = result.ChannelID
	log.Printf("获取二维码成功, channel_id: %s", s.channelID)
	return result.URL, nil
}

func (s *WPSAuthService) WaitForScan(timeout time.Duration) (string, error) {
	if s.channelID == "" {
		return "", fmt.Errorf("请先调用 Start() 获取二维码")
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		pollTimeout := 90 * time.Second
		if remaining+time.Second < pollTimeout {
			pollTimeout = remaining + time.Second
		}

		pollURL := fmt.Sprintf("%s?channel_id=%s", wpsPollAPI, url.QueryEscape(s.channelID))
		client := &http.Client{Jar: s.client.Jar, Timeout: pollTimeout}

		request, err := http.NewRequest(http.MethodGet, pollURL, nil)
		if err != nil {
			return "", err
		}
		s.setCommonHeaders(request)

		resp, err := client.Do(request)
		if err != nil {
			log.Printf("轮询异常: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			log.Printf("读取轮询响应失败: %v", readErr)
			time.Sleep(2 * time.Second)
			continue
		}

		var result struct {
			Result string `json:"result"`
			State  string `json:"state"`
			Data   string `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			log.Printf("解析轮询响应失败: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		switch result.State {
		case "pending":
			continue
		case "notified":
			var inner struct {
				Data struct {
					Status string `json:"status"`
					SSID   string `json:"ssid"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(result.Data), &inner); err != nil {
				continue
			}

			switch inner.Data.Status {
			case "scan":
				log.Println("用户已扫码，等待确认")
			case "finish":
				log.Println("扫码确认成功")
				return inner.Data.SSID, nil
			}
		default:
			if result.Result != "" && result.Result != "ok" {
				return "", fmt.Errorf("轮询返回错误: %s", string(body))
			}
		}
	}

	return "", fmt.Errorf("等待扫码超时")
}

func (s *WPSAuthService) Login(ssid string) ([]Cookie, error) {
	usersURL, _ := url.Parse(wpsUsersAPI)
	query := usersURL.Query()
	query.Set("ssid", ssid)
	query.Set("filter_rule", "normal")
	query.Set("check_rule", "second_phone")
	usersURL.RawQuery = query.Encode()

	request, err := http.NewRequest(http.MethodGet, usersURL.String(), nil)
	if err != nil {
		return nil, err
	}
	s.setCommonHeaders(request)

	resp, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("获取用户列表失败: %w", err)
	}
	defer resp.Body.Close()

	var usersData struct {
		Result string `json:"result"`
		Users  []struct {
			UserID   json.Number `json:"userid"`
			Nickname string      `json:"nickname"`
		} `json:"users"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&usersData); err != nil {
		return nil, fmt.Errorf("解析用户列表失败: %w", err)
	}
	if usersData.Result != "ok" {
		return nil, fmt.Errorf("获取用户列表失败: %s", usersData.Result)
	}
	if len(usersData.Users) == 0 {
		return nil, fmt.Errorf("未找到关联用户")
	}

	log.Printf("找到用户: %s", usersData.Users[0].Nickname)

	userIDs := make([]json.Number, 0, len(usersData.Users))
	for _, user := range usersData.Users {
		userIDs = append(userIDs, user.UserID)
	}

	payload, err := json.Marshal(map[string]any{
		"user_ids":    userIDs,
		"ssid":        ssid,
		"ssid_sign":   "",
		"public_key":  "",
		"from":        "",
		"page":        "v1/miniprogramcode",
		"slv":         "none",
		"append_only": false,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化登录请求失败: %w", err)
	}

	request, err = http.NewRequest(http.MethodPost, wpsLoginAPI, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRFToken", s.csrfToken)
	request.ContentLength = int64(len(payload))
	s.setCommonHeaders(request)

	loginResp, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	defer loginResp.Body.Close()

	var loginResult struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&loginResult); err != nil {
		return nil, fmt.Errorf("解析登录响应失败: %w", err)
	}
	if loginResult.Result != "ok" {
		return nil, fmt.Errorf("登录失败: %s", loginResult.Result)
	}

	log.Println("登录成功，正在收集 Cookies")
	s.visitKDocsDomains()
	return s.collectCookies(), nil
}

func (s *WPSAuthService) Run(timeout time.Duration) ([]Cookie, error) {
	qrURL, err := s.Start()
	if err != nil {
		return nil, err
	}

	fmt.Println()
	fmt.Println("==================================================")
	fmt.Println("请用微信扫描以下二维码链接对应的图片:")
	fmt.Println(qrURL)
	fmt.Println("==================================================")
	fmt.Println()

	ssid, err := s.WaitForScan(timeout)
	if err != nil {
		return nil, err
	}

	return s.Login(ssid)
}

func SaveCookies(cookies []Cookie, filePath string) error {
	data, err := json.MarshalIndent(CookieData{Cookies: cookies}, "", "    ")
	if err != nil {
		return fmt.Errorf("序列化 Cookies 失败: %w", err)
	}

	cleanPath := filepath.Clean(filePath)
	if dir := filepath.Dir(cleanPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建 Cookie 目录失败: %w", err)
		}
	}

	if err := os.WriteFile(cleanPath, data, 0600); err != nil {
		return fmt.Errorf("写入 Cookie 文件失败: %w", err)
	}

	log.Printf("Cookies 已保存到 %s (%d 条)", cleanPath, len(cookies))
	return nil
}

func generateCSRFToken() string {
	const chars = "ABCDEFGHJKMNPQRSTWXYZabcdefhijkmnprstwxyz2345678"
	randomBytes := make([]byte, 32)
	if _, err := crand.Read(randomBytes); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}

	token := make([]byte, len(randomBytes))
	for index, value := range randomBytes {
		token[index] = chars[int(value)%len(chars)]
	}

	return string(token)
}

func (s *WPSAuthService) visitKDocsDomains() {
	for _, domain := range []string{"https://account.kdocs.cn/", "https://f.kdocs.cn/"} {
		request, err := http.NewRequest(http.MethodGet, domain, nil)
		if err != nil {
			continue
		}
		s.setCommonHeaders(request)
		resp, err := s.client.Do(request)
		if err == nil {
			resp.Body.Close()
		}
	}
}

func (s *WPSAuthService) collectCookies() []Cookie {
	domains := []string{
		"https://account.wps.cn",
		"https://www.wps.cn",
		"https://account.kdocs.cn",
		"https://f.kdocs.cn",
		"https://f-api.kdocs.cn",
	}

	seen := make(map[string]struct{})
	cookies := make([]Cookie, 0)

	for _, domain := range domains {
		parsedURL, err := url.Parse(domain)
		if err != nil {
			continue
		}

		for _, cookie := range s.client.Jar.Cookies(parsedURL) {
			key := fmt.Sprintf("%s|%s|%s|%s", cookie.Name, cookie.Value, cookie.Domain, cookie.Path)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}

			expires := float64(-1)
			if !cookie.Expires.IsZero() {
				expires = float64(cookie.Expires.Unix())
			}

			domainName := cookie.Domain
			if domainName == "" {
				domainName = parsedURL.Hostname()
			}

			cookies = append(cookies, Cookie{
				Name:     cookie.Name,
				Value:    cookie.Value,
				Domain:   domainName,
				Path:     cookie.Path,
				Expires:  expires,
				HTTPOnly: cookie.HttpOnly,
				Secure:   cookie.Secure,
				SameSite: "None",
			})
		}
	}

	return cookies
}

func (s *WPSAuthService) setCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Referer", wpsLoginURL)
}
