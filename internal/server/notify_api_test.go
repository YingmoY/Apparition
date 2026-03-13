package server

import "testing"

func TestParseEnabled(t *testing.T) {
	if !parseEnabled(map[string]any{}) {
		t.Fatal("default enabled should be true")
	}
	if parseEnabled(map[string]any{"enabled": false}) {
		t.Fatal("enabled=false should be false")
	}
	if !parseEnabled(map[string]any{"enabled": 1.0}) {
		t.Fatal("enabled=1 should be true")
	}
}

func TestValidateNotifyConfig(t *testing.T) {
	app := &App{}
	if err := app.validateNotifyConfig("email", map[string]any{"to_email": "user@example.com"}); err != nil {
		t.Fatalf("email config should be valid: %v", err)
	}
	if err := app.validateNotifyConfig("gotify", map[string]any{"server_url": "http://localhost", "app_token": "abc"}); err != nil {
		t.Fatalf("gotify config should be valid: %v", err)
	}
	if err := app.validateNotifyConfig("bark", map[string]any{"server_url": "https://api.day.app", "device_key": "key"}); err != nil {
		t.Fatalf("bark config should be valid: %v", err)
	}
}
