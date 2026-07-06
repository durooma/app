package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"durooma/internal/ai"
	"durooma/internal/config"
	"durooma/internal/db"
	"durooma/internal/fx"
	"durooma/internal/importer"
	"durooma/internal/store"
	"durooma/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Printf("database ready, migrations applied")

	st := store.New(pool)
	conv := fx.New(st, cfg.FXBaseURL)
	imp := importer.New(st, conv, cfg.BaseCurrency)

	provider, err := ai.NewProvider(cfg.AIProvider, cfg.AIAPIKey, cfg.AIModel)
	if err != nil {
		return err
	}
	aiSvc := ai.NewService(st, provider)

	srv, err := web.NewServer(cfg, st, imp, aiSvc, conv)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s (base currency %s, AI provider %s)", cfg.HTTPAddr, cfg.BaseCurrency, cfg.AIProvider)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}
