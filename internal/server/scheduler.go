package server

import (
	"database/sql"
	"log"
	"time"
)

func (a *App) startScheduler(stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			a.dispatchDueJobs()
		}
	}
}

func (a *App) dispatchDueJobs() {
	now := time.Now().UTC()
	rows, err := a.db.Query(`SELECT id, user_id, schedule_type, schedule_value
		FROM clockin_jobs WHERE enabled = 1 AND next_run_at <= ?
		ORDER BY next_run_at ASC LIMIT 20`, now)
	if err != nil {
		log.Printf("调度器查询任务失败: %v", err)
		return
	}
	defer rows.Close()

	type dueJob struct {
		ID, UserID                int64
		ScheduleType, ScheduleValue string
	}
	var jobs []dueJob
	for rows.Next() {
		var j dueJob
		if err := rows.Scan(&j.ID, &j.UserID, &j.ScheduleType, &j.ScheduleValue); err != nil {
			log.Printf("调度器读取任务失败: %v", err)
			return
		}
		jobs = append(jobs, j)
	}

	for _, job := range jobs {
		nextRun, err := calcNextRunAt(job.ScheduleType, job.ScheduleValue, now)
		if err != nil {
			log.Printf("计算下次执行时间失败 job=%d: %v", job.ID, err)
			continue
		}

		runID, status, message := a.executeClockinRun(job.UserID, &job.ID, "scheduler")
		_, err = a.db.Exec(`UPDATE clockin_jobs SET last_run_at = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
			time.Now().UTC(), nextRun.UTC(), time.Now().UTC(), job.ID)
		if err != nil {
			log.Printf("更新任务状态失败 job=%d: %v", job.ID, err)
		}
		log.Printf("调度任务执行完成 job=%d run=%d status=%s message=%s", job.ID, runID, status, message)
	}
}

func (a *App) countEnabledJobs() int {
	var count int
	if err := a.db.QueryRow(`SELECT COUNT(1) FROM clockin_jobs WHERE enabled = 1`).Scan(&count); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("读取启用任务数失败: %v", err)
		}
		return 0
	}
	return count
}
