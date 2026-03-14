package server

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

func openDatabase(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
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
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id                   INTEGER PRIMARY KEY AUTOINCREMENT,
			email                TEXT NOT NULL UNIQUE,
			password_hash        TEXT NOT NULL,
			nickname             TEXT NOT NULL DEFAULT '',
			role                 TEXT NOT NULL DEFAULT 'user',
			status               TEXT NOT NULL DEFAULT 'active',
			must_change_password INTEGER NOT NULL DEFAULT 0,
			created_at           DATETIME NOT NULL,
			updated_at           DATETIME NOT NULL,
			last_login_at        DATETIME NULL
		)`,
		`CREATE TABLE IF NOT EXISTS email_verifications (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			email       TEXT NOT NULL,
			code_hash   TEXT NOT NULL,
			expire_at   DATETIME NOT NULL,
			consumed_at DATETIME NULL,
			request_ip  TEXT NOT NULL,
			created_at  DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ev_email_created ON email_verifications(email, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL REFERENCES users(id),
			token_hash TEXT NOT NULL UNIQUE,
			user_agent TEXT NOT NULL,
			client_ip  TEXT NOT NULL,
			expire_at  DATETIME NOT NULL,
			revoked_at DATETIME NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expire ON sessions(expire_at)`,
		`CREATE TABLE IF NOT EXISTS user_cookies (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id     INTEGER NOT NULL UNIQUE REFERENCES users(id),
			cookie_json TEXT NOT NULL,
			source      TEXT NOT NULL DEFAULT 'wps_qr',
			valid       INTEGER NOT NULL DEFAULT 1,
			updated_at  DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS clockin_profiles (
			id                INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id           INTEGER NOT NULL UNIQUE REFERENCES users(id),
			target_url        TEXT NOT NULL,
			input_name        TEXT NOT NULL,
			longitude         REAL NOT NULL,
			latitude          REAL NOT NULL,
			formatted_address TEXT NOT NULL,
			user_agent        TEXT NOT NULL DEFAULT 'Mozilla/5.0',
			locale            TEXT NOT NULL DEFAULT 'zh-CN',
			accept_language   TEXT NOT NULL DEFAULT 'zh-CN,zh;q=0.9',
			verify_cookies    TEXT NOT NULL DEFAULT 'enable',
			timezone          TEXT NOT NULL DEFAULT 'Asia/Shanghai',
			created_at        DATETIME NOT NULL,
			updated_at        DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS clockin_jobs (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id        INTEGER NOT NULL UNIQUE REFERENCES users(id),
			enabled        INTEGER NOT NULL DEFAULT 0,
			cron_expr      TEXT NOT NULL DEFAULT '',
			last_run_at    DATETIME NULL,
			created_at     DATETIME NOT NULL,
			updated_at     DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS clockin_runs (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id),
			job_id       INTEGER NULL REFERENCES clockin_jobs(id),
			trigger_type TEXT NOT NULL,
			status       TEXT NOT NULL,
			message      TEXT NOT NULL,
			started_at   DATETIME NOT NULL,
			finished_at  DATETIME NOT NULL,
			run_date     TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_user_started ON clockin_runs(user_id, started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_date_user ON clockin_runs(run_date, user_id)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id     INTEGER NULL REFERENCES users(id),
			actor_type  TEXT NOT NULL,
			action      TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id   TEXT NOT NULL,
			summary     TEXT NOT NULL,
			detail_json TEXT NOT NULL DEFAULT '{}',
			created_at  DATETIME NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_user ON audit_logs(user_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS notification_channels (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id      INTEGER NOT NULL REFERENCES users(id),
			channel_type TEXT NOT NULL,
			enabled      INTEGER NOT NULL DEFAULT 0,
			config_json  TEXT NOT NULL DEFAULT '{}',
			notify_events TEXT NOT NULL DEFAULT 'login,clockin_success,clockin_failed',
			created_at   DATETIME NOT NULL,
			updated_at   DATETIME NOT NULL,
			UNIQUE(user_id, channel_type)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("执行迁移失败: %w\nSQL: %s", err, s)
		}
	}

	// Safe column additions for existing databases
	safeAddColumn(db, "notification_channels", "notify_events", "TEXT NOT NULL DEFAULT 'login,clockin_success,clockin_failed'")

	return nil
}

func safeAddColumn(db *sql.DB, table, column, colDef string) {
	_, _ = db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef))
}
