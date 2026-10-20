package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"axiom/code-audit-agent/internal/config"
	"axiom/code-audit-agent/internal/worker"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("agent ")

	if len(os.Args) > 1 && os.Args[1] == "enqueue" {
		log.Fatal("enqueue moved to the Server administrator tool: code-audit-server/cmd/enqueue; use Web to create scans")
	}

	cfg := config.FromEnv()
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		if err := worker.CheckHealth(cfg.HealthFile); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	once := flag.Bool("once", false, "claim and run at most one task then exit")
	flag.Parse()
	cfg.Once = *once

	absScanner, err := filepath.Abs(cfg.ScannerDir)
	if err != nil {
		log.Fatal(err)
	}
	cfg.ScannerDir = absScanner
	absData, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	cfg.DataDir = absData

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := &worker.Worker{Cfg: cfg}
	log.Printf("id=%s dispatch=unbounded scanner=%s", cfg.AgentID, cfg.ScannerDir)
	if err := w.Run(ctx); err != nil && err != context.Canceled {
		log.Fatal(err)
	}
}
