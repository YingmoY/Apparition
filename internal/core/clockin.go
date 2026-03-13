package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	kdocsAuthCheckAPI  = "https://account.kdocs.cn/p/auth/check"
	kdocsCampaignAPI   = "https://f-api.kdocs.cn/ksform/api/v3/campaign/%s"
	kdocsPrecheckAPI   = "https://f-api.kdocs.cn/ksform/api/v3/campaign/%s/precheck"
	kdocsPresetKeyAPI  = "https://f-api.kdocs.cn/ksform/api/v3/campaign/%s/preset/key/check"
	kdocsAnswersAPI    = "https://f-api.kdocs.cn/ksform/api/v3/campaign/%s/answers/list"
	kdocsFormReferer   = "https://f.kdocs.cn/ksform/cw/w/%s"
	defaultHTTPTimeout = 15 * time.Second
)

type ClockInClient struct {
	Config           Config
	CookieData       CookieData
	CampaignID       string
	HTTPClient       *http.Client
	UserInfo         *AuthResponse
	ClockInFieldID   string
	CommitOptionID   string
	CommitOptionText string
}

func NewClockInClient(config Config, cookieData CookieData) (*ClockInClient, error) {
	campaignID, err := ExtractCampaignID(config.TargetURL)
	if err != nil {
		return nil, err
	}

	return &ClockInClient{
		Config:     config,
		CookieData: cookieData,
		CampaignID: campaignID,
		HTTPClient: &http.Client{Timeout: defaultHTTPTimeout},
	}, nil
}

func NewClockInClientFromFiles(configPath, cookiePath string) (*ClockInClient, error) {
	config, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	cookieData, err := LoadCookieData(cookiePath)
	if err != nil {
		return nil, err
	}

	return NewClockInClient(config, cookieData)
}

func (c *ClockInClient) makeRequest(method, url string, body any) (*http.Response, error) {
	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
		requestBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		return nil, err
	}

	csrfToken := ""
	for _, cookie := range c.CookieData.Cookies {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		if cookie.Name == "csrf" {
			csrfToken = cookie.Value
		}
	}

	req.Header.Set("User-Agent", c.Config.UserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", c.Config.AcceptLanguage)
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Origin", "https://f.kdocs.cn")
	req.Header.Set("Referer", fmt.Sprintf(kdocsFormReferer, c.CampaignID))
	req.Header.Set("X-CSRF-Token", csrfToken)

	return c.HTTPClient.Do(req)
}

func (c *ClockInClient) CheckAuth() bool {
	payload := map[string]any{"_t": time.Now().UnixMilli()}

	resp, err := c.makeRequest(http.MethodPost, kdocsAuthCheckAPI, payload)
	if err != nil {
		log.Printf("认证异常: %v", err)
		return false
	}
	defer resp.Body.Close()

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		log.Printf("解析认证响应失败: %v", err)
		return false
	}

	if hasUserID(authResp.UserID) {
		c.UserInfo = &authResp
		log.Printf("认证成功: %s", authResp.Nickname)
		return true
	}

	log.Println("认证失败: Cookie 已过期")
	return false
}

func (c *ClockInClient) GetFormInfo() (*FormResponse, error) {
	url := fmt.Sprintf(kdocsCampaignAPI, c.CampaignID)
	resp, err := c.makeRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var formResp FormResponse
	if err := json.NewDecoder(resp.Body).Decode(&formResp); err != nil {
		return nil, err
	}
	if formResp.Code != 0 {
		return nil, fmt.Errorf("获取表单失败: code %d", formResp.Code)
	}

	log.Printf("表单: %s", formResp.Data.Name)

	for questionID, question := range formResp.Data.QuestionMap {
		if question.Type == "clockinInfo" {
			c.ClockInFieldID = questionID
			log.Printf("打卡字段ID: %s", questionID)
			break
		}
	}

	options := formResp.Data.Setting.BaseSetting.CommitConfig.Options
	if len(options) > 0 {
		c.CommitOptionID = options[0].ID
		c.CommitOptionText = options[0].Text
		log.Printf("提交选项ID: %s (%s)", c.CommitOptionID, c.CommitOptionText)
	}

	return &formResp, nil
}

func (c *ClockInClient) Precheck() (*GenericResponse, error) {
	resp, err := c.makeRequest(http.MethodPost, fmt.Sprintf(kdocsPrecheckAPI, c.CampaignID), map[string]any{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GenericResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ClockInClient) CheckPresetKey(keyValue string) (bool, string, error) {
	payload := map[string]any{"key": keyValue}
	resp, err := c.makeRequest(http.MethodPost, fmt.Sprintf(kdocsPresetKeyAPI, c.CampaignID), payload)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()

	var result struct {
		Code   int    `json:"code"`
		Result string `json:"result"`
		Data   struct {
			KeyID string `json:"keyId"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, "", err
	}
	if result.Code == 0 {
		log.Printf("姓名验证成功: %s", keyValue)
		return true, result.Data.KeyID, nil
	}

	log.Printf("姓名验证失败: %s", result.Result)
	return false, "", nil
}

func (c *ClockInClient) CheckTodayAnswer() (bool, error) {
	payload := map[string]any{
		"page":     1,
		"pageSize": 5,
	}

	resp, err := c.makeRequest(http.MethodPost, fmt.Sprintf(kdocsAnswersAPI, c.CampaignID), payload)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var result AnswersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, err
	}

	if result.Code == 0 {
		today := time.Now().Format("20060102")
		for _, answer := range result.Data.Answers {
			if strings.HasPrefix(answer.Aid, today) {
				log.Printf("今日已打卡: %s", answer.Aid)
				return true, nil
			}
		}
	}

	return false, nil
}

func (c *ClockInClient) SubmitClockIn(keyID string) (*GenericResponse, error) {
	if c.ClockInFieldID == "" {
		return nil, fmt.Errorf("未获取到打卡字段ID")
	}
	if c.CommitOptionID == "" {
		return nil, fmt.Errorf("未获取到提交选项ID")
	}

	log.Printf("定位: %s (%.6f, %.6f)", c.Config.FormattedAddress, c.Config.Longitude, c.Config.Latitude)

	payload := map[string]any{
		"answerJson": map[string]any{
			"answers": map[string]any{
				c.ClockInFieldID: map[string]any{
					"type": "clockinInfo",
					"clockinInfoValue": map[string]any{
						"clockinLocation": map[string]any{
							"type":          "input",
							"strValue":      c.Config.FormattedAddress,
							"isManualInput": false,
						},
						"clockinName": map[string]any{
							"type":          "input",
							"strValue":      c.Config.InputName,
							"isManualInput": false,
						},
					},
				},
			},
			"consumeTime": 10,
			"answersProperty": map[string]any{
				"presetKeyId":    keyID,
				"presetKeyValue": c.Config.InputName,
				"commitInfo": map[string]any{
					"optionId":   c.CommitOptionID,
					"optionText": c.CommitOptionText,
				},
				"clockinInfo": map[string]any{
					"clockinStatus":          "normal",
					"outOfPeriodDescription": "",
				},
			},
		},
		"_t": time.Now().UnixMilli(),
	}

	resp, err := c.makeRequest(http.MethodPost, fmt.Sprintf(kdocsCampaignAPI, c.CampaignID), payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GenericResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *ClockInClient) Run() ClockInResult {
	log.Println(strings.Repeat("=", 50))
	log.Println("金山表单打卡 - Go 版")
	log.Printf("时间: %s", time.Now().Format("2006-01-02 15:04:05"))
	log.Println(strings.Repeat("=", 50))

	log.Println("[步骤 1/6] 认证检查")
	if !c.CheckAuth() {
		return ClockInResult{Success: false, Message: "认证失败，请重新登录"}
	}

	log.Println("[步骤 2/6] 获取表单信息")
	if _, err := c.GetFormInfo(); err != nil {
		return ClockInResult{Success: false, Message: fmt.Sprintf("无法获取表单信息: %v", err)}
	}

	log.Println("[步骤 3/6] 预检查")
	precheck, err := c.Precheck()
	if err != nil {
		return ClockInResult{Success: false, Message: fmt.Sprintf("预检查失败: %v", err)}
	}
	if precheck.Code != 0 {
		message := precheck.Result
		if strings.Contains(message, "时间") || strings.Contains(message, "周期") {
			log.Printf("预检查中断: %s", message)
			return ClockInResult{Success: false, Message: message}
		}
	}
	log.Println("预检查通过")

	log.Println("[步骤 4/6] 验证姓名")
	success, keyID, err := c.CheckPresetKey(c.Config.InputName)
	if err != nil {
		return ClockInResult{Success: false, Message: fmt.Sprintf("姓名验证异常: %v", err)}
	}
	if !success {
		return ClockInResult{Success: false, Message: "姓名验证失败，请检查 input_name 配置"}
	}

	log.Println("[步骤 5/6] 检查打卡状态")
	alreadyClocked, err := c.CheckTodayAnswer()
	if err != nil {
		log.Printf("检查打卡记录失败: %v", err)
	}
	if alreadyClocked {
		log.Println("今日已打卡，无需重复提交")
		return ClockInResult{Success: true, Message: "今日已打卡"}
	}

	log.Println("[步骤 6/6] 提交打卡")
	result, err := c.SubmitClockIn(keyID)
	if err != nil {
		return ClockInResult{Success: false, Message: fmt.Sprintf("提交失败: %v", err)}
	}
	if result.Code == 0 {
		log.Println(strings.Repeat("=", 50))
		log.Println("打卡成功")
		log.Println(strings.Repeat("=", 50))
		return ClockInResult{Success: true, Message: "打卡成功"}
	}

	message := result.Result
	if message == "" {
		message = "未知错误"
	}
	log.Printf("打卡失败: %s", message)
	return ClockInResult{Success: false, Message: message}
}

func (c *ClockInClient) TestAPI() error {
	log.Println(strings.Repeat("=", 50))
	log.Println("API 测试模式")
	log.Println(strings.Repeat("=", 50))

	log.Println("[1] 认证测试")
	c.CheckAuth()

	log.Println("[2] 表单信息测试")
	if _, err := c.GetFormInfo(); err != nil {
		log.Printf("获取表单信息失败: %v", err)
	}

	log.Println("[3] 位置信息测试")
	log.Printf("GCJ02: %.6f, %.6f", c.Config.Longitude, c.Config.Latitude)
	log.Printf("地址: %s", c.Config.FormattedAddress)

	log.Println("[4] 姓名验证测试")
	if _, _, err := c.CheckPresetKey(c.Config.InputName); err != nil {
		log.Printf("姓名验证异常: %v", err)
	}

	log.Println("[5] 打卡记录测试")
	if _, err := c.CheckTodayAnswer(); err != nil {
		log.Printf("打卡记录测试异常: %v", err)
	}

	return nil
}

func hasUserID(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	default:
		return fmt.Sprint(typed) != ""
	}
}
