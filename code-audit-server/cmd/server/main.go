package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/config"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/code-audit-server/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("server ")
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		if !readyCheck() {
			os.Exit(1)
		}
		return
	}
	if err := config.ValidateEnvironment(os.Getenv); err != nil {
		log.Fatal(err)
	}

	dbPath := config.LegacyDatabasePath(os.Getenv)
	dataDir := os.Getenv("AXIOM_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Dir(dbPath)
	}
	datasource := config.DatabaseSource(os.Getenv)
	st, err := store.Open(datasource)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	secret := os.Getenv("AXIOM_JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-change-me"
	}
	adminUser := os.Getenv("AXIOM_ADMIN_USER")
	if adminUser == "" {
		adminUser = "admin"
	}
	hash, err := auth.NewPasswordHash(httpserver.DefaultAdminPassword())
	if err != nil {
		log.Fatal("administrator credential initialization failed")
	}
	if _, err = st.EnsureAdministrator(context.Background(), adminUser, hash); err != nil {
		log.Fatal(err)
	}

	snapshotsDir := os.Getenv("AXIOM_SNAPSHOT_DIR")
	if snapshotsDir == "" {
		snapshotsDir = filepath.Join(dataDir, "snapshots")
	}
	if err := os.MkdirAll(snapshotsDir, 0o700); err != nil {
		log.Fatal(err)
	}
	srv := &httpserver.Server{
		Store:                st,
		AdminUsername:        adminUser,
		Secret:               secret,
		LegacyPasswordSecret: os.Getenv("AXIOM_LEGACY_PASSWORD_SECRET"),
		SnapshotDir:          snapshotsDir,
		Limits:               snapshot.DefaultLimits(),
		AgentToken:           os.Getenv("AXIOM_AGENT_TOKEN"),
		QueueLimits:          store.ClaimLimits{Global: positiveEnv("AXIOM_MAX_ACTIVE_SCANS", 2), PerModel: positiveEnv("AXIOM_MAX_MODEL_SCANS", 2), PerProject: positiveEnv("AXIOM_MAX_PROJECT_SCANS", 2), Lease: 60 * time.Second, OutputRoot: filepath.Join(dataDir, "scans"), MaxRecoveries: config.MaxRecoveries(os.Getenv)},
	}
	addr := os.Getenv("AXIOM_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	log.Printf("listen %s postgres=%t snapshots=%s", addr, st.IsPostgres(), snapshotsDir)
	httpSrv := &http.Server{Addr: addr, Handler: srv.Routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func readyCheck() bool {
	addr := os.Getenv("AXIOM_HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + "/readyz")
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func positiveEnv(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n < 1 || n > 64 {
		return fallback
	}
	return n
}
