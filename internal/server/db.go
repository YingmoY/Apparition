package server

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

func openDatabase(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}

	return db, nil
}

func migrateDatabase(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			nickname TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'user',
			must_change_password INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			last_login_at DATETIME NULL
		);`,
		`CREATE TABLE IF NOT EXISTS email_verifications (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			purpose TEXT NOT NULL,
			code_hash TEXT NOT NULL,
			expire_at DATETIME NOT NULL,
			consumed_at DATETIME NULL,
			request_ip TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_email_verifications_email_purpose_created
		 ON email_verifications(email, purpose, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_email_verifications_expire_at
		 ON email_verifications(expire_at);`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			user_agent TEXT NOT NULL,
			client_ip TEXT NOT NULL,
			expire_at DATETIME NOT NULL,
			revoked_at DATETIME NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_expire ON sessions(user_id, expire_at);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expire ON sessions(expire_at);`,
		`CREATE TABLE IF NOT EXISTS wps_login_sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			channel_id TEXT NOT NULL,
			qr_url TEXT NOT NULL,
			qr_generated_at DATETIME NOT NULL,
			status TEXT NOT NULL,
			ssid TEXT NULL,
			expire_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_wps_login_sessions_status_expire ON wps_login_sessions(status, expire_at);`,
		`CREATE INDEX IF NOT EXISTS idx_wps_login_sessions_user_created ON wps_login_sessions(user_id, created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS user_cookies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL UNIQUE,
			cookie_json TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'wps_qr',
			valid INTEGER NOT NULL DEFAULT 1,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE TABLE IF NOT EXISTS clockin_profiles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL UNIQUE,
			target_url TEXT NOT NULL,
			input_name TEXT NOT NULL,
			longitude REAL NOT NULL,
			latitude REAL NOT NULL,
			formatted_address TEXT NOT NULL,
			user_agent TEXT NOT NULL,
			locale TEXT NOT NULL DEFAULT 'zh-CN',
			accept_language TEXT NOT NULL DEFAULT 'zh-CN,zh;q=0.9',
			verify_cookies TEXT NOT NULL DEFAULT 'enable',
			timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE TABLE IF NOT EXISTS clockin_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			schedule_type TEXT NOT NULL,
			schedule_value TEXT NOT NULL,
			next_run_at DATETIME NOT NULL,
			last_run_at DATETIME NULL,
			retry_policy TEXT NOT NULL DEFAULT 'immediate_once',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_clockin_jobs_enabled_next_run ON clockin_jobs(enabled, next_run_at);`,
		`CREATE INDEX IF NOT EXISTS idx_clockin_jobs_user_id ON clockin_jobs(user_id);`,
		`CREATE TABLE IF NOT EXISTS clockin_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			job_id INTEGER NULL,
			trigger_type TEXT NOT NULL,
			status TEXT NOT NULL,
			message TEXT NOT NULL,
			started_at DATETIME NOT NULL,
			finished_at DATETIME NOT NULL,
			run_date TEXT NOT NULL,
			error_code TEXT NULL,
			extra_json TEXT NOT NULL DEFAULT '{}',
			FOREIGN KEY(user_id) REFERENCES users(id),
			FOREIGN KEY(job_id) REFERENCES clockin_jobs(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_clockin_runs_user_started ON clockin_runs(user_id, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_clockin_runs_job_started ON clockin_runs(job_id, started_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_clockin_runs_run_date_user ON clockin_runs(run_date, user_id);`,
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			channel_type TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			config_json TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			UNIQUE(user_id, channel_type),
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE TABLE IF NOT EXISTS notification_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			run_id INTEGER NULL,
			event_type TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			level TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id),
			FOREIGN KEY(run_id) REFERENCES clockin_runs(id)
		);`,
		`CREATE TABLE IF NOT EXISTS notification_deliveries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			channel_type TEXT NOT NULL,
			status TEXT NOT NULL,
			response_code INTEGER NULL,
			response_body TEXT NOT NULL DEFAULT '',
			retry_count INTEGER NOT NULL DEFAULT 0,
			sent_at DATETIME NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY(event_id) REFERENCES notification_events(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notification_deliveries_event_channel ON notification_deliveries(event_id, channel_type);`,
		`CREATE INDEX IF NOT EXISTS idx_notification_deliveries_status_created ON notification_deliveries(status, created_at);`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NULL,
			actor_type TEXT NOT NULL,
			action TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			summary TEXT NOT NULL,
			detail_json TEXT NOT NULL DEFAULT '{}',
			request_id TEXT NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_user_created ON audit_logs(user_id, created_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("执行迁移失败: %w", err)
		}
	}

	return nil
}
