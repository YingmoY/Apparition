package server

import (
	"database/sql"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func ensureDefaultAdmin(db *sql.DB, cfg ServerConfig) error {
	if cfg.Admin.Username == "" {
		return fmt.Errorf("管理员用户名不能为空")
	}
	if cfg.Admin.PasswordHash == "" {
		return fmt.Errorf("管理员密码哈希不能为空")
	}

	var existingID int64
	err := db.QueryRow(`SELECT id FROM users WHERE role = 'admin' LIMIT 1`).Scan(&existingID)
	if err == nil {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("检查管理员用户失败: %w", err)
	}

	now := time.Now().UTC()
	_, err = db.Exec(`
		INSERT INTO users (email, password_hash, nickname, role, must_change_password, status, created_at, updated_at)
		VALUES (?, ?, ?, 'admin', ?, 'active', ?, ?)
	`, cfg.Admin.Username, cfg.Admin.PasswordHash, "System Admin", boolToInt(cfg.Admin.MustChangePassword), now, now)
	if err != nil {
		return fmt.Errorf("写入默认管理员失败: %w", err)
	}

	return nil
}

func verifyAdminPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
