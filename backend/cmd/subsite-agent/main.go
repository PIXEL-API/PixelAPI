package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Wei-Shaw/sub2api/internal/subsite/agent"
)

func main() {
	configPath := flag.String("config", "", "path to subsite agent yaml config")
	flag.Parse()

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := agent.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "subsite agent stopped: %v\n", err)
		os.Exit(1)
	}
}
