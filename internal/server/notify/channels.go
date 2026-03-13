package notify

import "context"

type Event struct {
	UserID    int64
	EventType string
	Title     string
	Body      string
	Level     string
}

type EmailChannelConfig struct {
	Enabled     bool   `json:"enabled"`
	ToEmail     string `json:"to_email"`
	SubjectPref string `json:"subject_prefix"`
}

type GotifyChannelConfig struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	AppToken  string `json:"app_token"`
	Priority  int    `json:"priority"`
}

type BarkChannelConfig struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	DeviceKey string `json:"device_key"`
	Group     string `json:"group"`
	Sound     string `json:"sound"`
	Icon      string `json:"icon"`
}

type ChannelSender interface {
	Name() string
	Send(ctx context.Context, event Event) error
}
