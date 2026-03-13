package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/YingmoY/Apparition/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := server.NewApp()
	if err != nil {
		log.Fatalf("初始化服务失败: %v", err)
	}

	if err := app.Run(ctx); err != nil {
		log.Fatalf("服务运行失败: %v", err)
	}
}
