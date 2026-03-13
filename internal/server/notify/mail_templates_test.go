package notify

import "testing"

func TestRenderMailContent(t *testing.T) {
	content, err := RenderMailContent("verify_register", MailTemplateData{
		AppName:       "Apparition",
		Code:          "123456",
		ExpireMinutes: 10,
		RequestIP:     "127.0.0.1",
		RequestTime:   "2026-03-13 12:00:00",
		SupportEmail:  "support@example.com",
	})
	if err != nil {
		t.Fatalf("render template failed: %v", err)
	}
	if content.Subject == "" || content.TextBody == "" || content.HTMLBody == "" {
		t.Fatal("rendered content should not be empty")
	}
}
