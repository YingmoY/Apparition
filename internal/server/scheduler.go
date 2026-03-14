package server

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type cronScheduler struct {
	c  *cron.Cron
	mu sync.Mutex
}

func newCronScheduler() *cronScheduler {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return &cronScheduler{
		c: cron.New(cron.WithLocation(loc), cron.WithSeconds()),
	}
}

func (s *cronScheduler) start() {
	s.c.Start()
}

func (s *cronScheduler) stop() {
	s.c.Stop()
}

func (a *App) initScheduler() {
	a.cron = newCronScheduler()
	a.loadAllCronJobs()
	a.cron.start()
}

func (a *App) loadAllCronJobs() {
	a.cron.mu.Lock()
	defer a.cron.mu.Unlock()

	// Remove all existing entries
	for _, entry := range a.cron.c.Entries() {
		a.cron.c.Remove(entry.ID)
	}

	rows, err := a.db.Query(`SELECT user_id, cron_expr FROM clockin_jobs WHERE enabled = 1 AND cron_expr != ''`)
	if err != nil {
		log.Printf("加载定时任务失败: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var userID int64
		var cronExpr string
		if err := rows.Scan(&userID, &cronExpr); err != nil {
			continue
		}
		uid := userID // capture for closure
		_, err := a.cron.c.AddFunc(cronExpr, func() {
			log.Printf("cron 触发打卡任务: user=%d", uid)
			runID, status, message := a.executeClockinRun(uid, "scheduler")
			log.Printf("cron 任务完成: user=%d run=%d status=%s message=%s", uid, runID, status, message)
		})
		if err != nil {
			log.Printf("添加 cron 任务失败 user=%d expr=%s: %v", userID, cronExpr, err)
			continue
		}
		count++
	}
	log.Printf("已加载 %d 个定时任务", count)
}

func (a *App) reloadCron() {
	a.loadAllCronJobs()
}

func (a *App) validateCronExpr(expr string) error {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("解析失败: %w", err)
	}
	// Verify it produces a reasonable next time
	next := sched.Next(time.Now().In(loc))
	if next.IsZero() {
		return fmt.Errorf("无法计算下次执行时间")
	}
	return nil
}

func (a *App) countEnabledJobs() int {
	var count int
	_ = a.db.QueryRow(`SELECT COUNT(1) FROM clockin_jobs WHERE enabled = 1 AND cron_expr != ''`).Scan(&count)
	return count
}
