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
	"durooma/internal/secrets"
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

	vault, err := secrets.LoadOrCreate(cfg.CredentialsKeyFile)
	if err != nil {
		return err
	}
	st := store.NewWithVault(pool, vault)
	if _, err = st.OpenRouterKey(ctx, store.LocalUserID); err != nil {
		return err
	}
	conv := fx.New(st, cfg.FXBaseURL)
	imp := importer.New(st, conv, cfg.BaseCurrency)

	provider := ai.NewUserProvider(st, store.LocalUserID)
	aiSvc := ai.NewConfiguredService(st, provider, func(ctx context.Context) (ai.RunConfig, error) {
		settings, err := st.AISettings(ctx, store.LocalUserID)
		if err != nil {
			return ai.RunConfig{}, err
		}
		if !settings.HasKey {
			return ai.RunConfig{}, ai.ErrAPIKeyRequired
		}
		return ai.RunConfig{BatchSize: settings.BatchSize, Concurrency: settings.Concurrency, Model: settings.Model}, nil
	})
	background := ai.NewBackgroundControl(func(ctx context.Context) (bool, error) {
		settings, err := st.AISettings(ctx, store.LocalUserID)
		return settings.HasKey && settings.Background, err
	}, func(ctx context.Context) { ai.RunBackground(ctx, st, aiSvc, cfg.AIPollInterval) }, cfg.AIPollInterval)

	srv, err := web.NewServer(cfg, st, imp, aiSvc, conv, background)
	if err != nil {
		return err
	}

	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); background.Run(workerCtx) }()
	defer func() { stopWorker(); <-workerDone }()

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s (base currency %s, AI provider %s)", cfg.HTTPAddr, cfg.BaseCurrency, "OpenRouter (user credentials)")
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
