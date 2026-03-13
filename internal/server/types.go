package server

import "time"

const (
	defaultDataDirName      = "data"
	defaultConfigFileName   = "server-config.json"
	defaultDBFileName       = "data.db"
	defaultLogDirName       = "logs"
	defaultLogFileName      = "server.log"
	defaultAdminUsername    = "admin"
	defaultAdminPassword    = "admin"
	defaultServerListenHost = "0.0.0.0"
	defaultServerListenPort = 5680
)

type ServerConfig struct {
	Server           ServerSection           `json:"server"`
	Admin            AdminSection            `json:"admin"`
	Security         SecuritySection         `json:"security"`
	SMTP             SMTPSection             `json:"smtp"`
	NotifierDefaults NotifierDefaultsSection `json:"notifier_defaults"`
	GotifyGlobal     GotifyGlobalSection     `json:"gotify_global"`
	BarkGlobal       BarkGlobalSection       `json:"bark_global"`
}

type ServerSection struct {
	Host            string `json:"host"`
	Port            int    `json:"port"`
	ReadTimeoutSec  int    `json:"read_timeout_sec"`
	WriteTimeoutSec int    `json:"write_timeout_sec"`
	IdleTimeoutSec  int    `json:"idle_timeout_sec"`
}

type AdminSection struct {
	Username           string `json:"username"`
	PasswordHash       string `json:"password_hash"`
	MustChangePassword bool   `json:"must_change_password"`
}

type SecuritySection struct {
	SessionTTLHours       int  `json:"session_ttl_hours"`
	RememberMeTTLDays     int  `json:"remember_me_ttl_days"`
	CSRFEnabled           bool `json:"csrf_enabled"`
	LoginRateLimitPerMin  int  `json:"login_rate_limit_per_minute"`
	EmailSendLimitPerHour int  `json:"email_send_limit_per_hour"`
}

type SMTPSection struct {
	Enabled   bool   `json:"enabled"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	FromName  string `json:"from_name"`
	FromEmail string `json:"from_email"`
	TLSMode   string `json:"tls_mode"`
}

type NotifierDefaultsSection struct {
	OnClockInSuccess bool `json:"on_clockin_success"`
	OnClockInFailed  bool `json:"on_clockin_failed"`
	OnAuthSecurity   bool `json:"on_auth_security"`
}

type GotifyGlobalSection struct {
	Enabled         bool   `json:"enabled"`
	ServerURL       string `json:"server_url"`
	AppToken        string `json:"app_token"`
	DefaultPriority int    `json:"default_priority"`
}

type BarkGlobalSection struct {
	Enabled      bool   `json:"enabled"`
	ServerURL    string `json:"server_url"`
	DeviceKey    string `json:"device_key"`
	DefaultSound string `json:"default_sound"`
	DefaultIcon  string `json:"default_icon"`
}

type RuntimePaths struct {
	BaseDir    string
	DataDir    string
	ConfigPath string
	DBPath     string
	LogDir     string
	LogPath    string
}

type BootstrapState struct {
	FirstRun                bool
	AdminMustChangePassword bool
	DBReady                 bool
	ServerStartedAt         time.Time
}
