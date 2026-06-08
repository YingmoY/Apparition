package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendBarkNotificationUsesPushJSON(t *testing.T) {
	var gotPath, gotContentType string
	var gotPayload map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		if r.Method != http.MethodPost {
			t.Fatalf("Bark 应使用 POST: got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("解析 Bark 请求体失败: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "success"})
	}))
	defer ts.Close()

	err := sendBarkNotification(barkNotifyConfig{ServerURL: ts.URL, DeviceKey: "dev/key"}, "标题/含斜杠", "内容 含空格/斜杠")
	if err != nil {
		t.Fatalf("发送 Bark 通知失败: %v", err)
	}
	if gotPath != "/push" {
		t.Fatalf("Bark 请求路径错误: got %s", gotPath)
	}
	if gotContentType != "application/json; charset=utf-8" {
		t.Fatalf("Bark Content-Type 错误: got %s", gotContentType)
	}
	if gotPayload["device_key"] != "dev/key" || gotPayload["title"] != "标题/含斜杠" || gotPayload["body"] != "内容 含空格/斜杠" {
		t.Fatalf("Bark JSON 请求体错误: %#v", gotPayload)
	}
}
