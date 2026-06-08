package server

import (
	"database/sql"
	"testing"
	"time"
)

func TestSubmitScheduledRunDedupesSameUserAndRunDate(t *testing.T) {
	a := &App{
		pendingRuns: make(map[string]struct{}),
		schedStop:   make(chan struct{}),
		schedQueue:  make(chan scheduledRun, 2),
	}
	defer close(a.schedStop)

	scheduledAt := time.Date(2026, 6, 8, 8, 0, 0, 0, schedulerLocation())
	if !a.submitScheduledRun(42, "scheduler", scheduledAt) {
		t.Fatal("首次定时任务应入队成功")
	}
	if a.submitScheduledRun(42, "scheduler_calibration", scheduledAt.Add(30*time.Second)) {
		t.Fatal("同一用户同一打卡日期的补偿任务不应重复入队")
	}

	queued := <-a.schedQueue
	if queued.runDate != "20260608" {
		t.Fatalf("runDate 错误: got %s", queued.runDate)
	}
}

func TestExecuteClockinRunForDateSkipsExistingScheduledRun(t *testing.T) {
	db := newTestDB(t)
	now := time.Date(2026, 6, 8, 1, 0, 0, 0, time.UTC)
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, nickname, role, status, created_at, updated_at)
		VALUES (1, 'u@example.com', 'hash', 'u', 'user', 'active', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("插入用户失败: %v", err)
	}
	_, err = db.Exec(`INSERT INTO clockin_runs (user_id, job_id, trigger_type, status, message, started_at, finished_at, run_date)
		VALUES (1, NULL, 'scheduler', 'success', 'ok', ?, ?, '20260608')`, now, now)
	if err != nil {
		t.Fatalf("插入执行记录失败: %v", err)
	}

	a := &App{db: db}
	runID, status, message := a.executeClockinRunForDate(1, "scheduler_calibration", "20260608")
	if runID != 0 || status != "skipped" {
		t.Fatalf("应跳过重复定时任务: runID=%d status=%s message=%s", runID, status, message)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM clockin_runs WHERE user_id = 1`).Scan(&count); err != nil {
		t.Fatalf("查询执行记录失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("跳过重复任务不应插入新执行记录: got %d", count)
	}
}

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openDatabase(t.TempDir() + "/apparition.db")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrateDatabase(db); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}
	return db
}
