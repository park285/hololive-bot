package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/kapu/hololive-api/internal/planes/bot/durableadmin"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	os.Exit(durableadmin.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
