// Command openchat runs the OpenChat API server.
//
// Usage:
//
//	openchat            start the HTTP server (migrating on boot)
//	openchat migrate    apply pending migrations and exit
//	openchat rollback   revert the most recent migration and exit
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/qunqin/openchat/apps/server/internal/api"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/chat"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/logging"
	"github.com/qunqin/openchat/apps/server/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Loaded one at a time on purpose: godotenv.Load returns at the first
	// missing file, so passing both paths at once would skip the repo-root .env
	// whenever the server is started from apps/server (which `make ser` does).
	for _, path := range []string{".env", "../../.env"} {
		_ = godotenv.Load(path)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	setupLogging(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch command() {
	case "migrate":
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
		slog.Info("migrations applied")
		return nil
	case "rollback":
		if err := db.Rollback(ctx, pool); err != nil {
			return err
		}
		slog.Info("migration rolled back")
		return nil
	}

	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	store, err := storage.New(ctx, cfg)
	if err != nil {
		return err
	}

	// Swap this for a real model client when one is wired up.
	provider := chat.Echo{Delay: 25 * time.Millisecond}

	queries := sqlc.New(pool)

	server := api.NewServer(cfg, pool, store, provider)
	// Before the listener opens, so a server started with debug logging on logs
	// its own startup in detail — which is when you want it most.
	server.LoadSettings(ctx)
	// Deliberately not waited on: the model catalogue comes from a third party,
	// and a boot that blocked on it would be a boot somebody else can hold up.
	server.StartCatalogRefresh(ctx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(cfg, server, auth.NewMiddleware(queries)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No WriteTimeout: SSE responses stay open for the length of a reply.
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", cfg.HTTPAddr, "env", cfg.Env, "provider", provider.Name())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func command() string {
	if len(os.Args) < 2 {
		return ""
	}
	return os.Args[1]
}

func setupLogging(cfg *config.Config) {
	// Development used to start at Debug. It starts at Info now, in both
	// environments, so that the switch in the admin panel is what turns detail
	// on — a toggle that changes nothing where you develop is not a toggle.
	logging.Setup(cfg.IsProduction())
}
