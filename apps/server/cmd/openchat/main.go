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
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// The zone database, inside the binary. The production image is distroless
	// and has none, and without it TZ=Asia/Shanghai is silently read as UTC -
	// which would move the day and month a model's spending cap starts over at.
	_ "time/tzdata"

	"github.com/joho/godotenv"

	"github.com/qunqin/openchat/apps/server/internal/agent"
	"github.com/qunqin/openchat/apps/server/internal/api"
	"github.com/qunqin/openchat/apps/server/internal/auth"
	"github.com/qunqin/openchat/apps/server/internal/config"
	"github.com/qunqin/openchat/apps/server/internal/db"
	"github.com/qunqin/openchat/apps/server/internal/db/sqlc"
	"github.com/qunqin/openchat/apps/server/internal/logging"
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
	logTimeZone()

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

	// Where attachments go is a setting, read from the database and changeable
	// from the admin panel without a restart.
	store, err := api.OpenStorage(ctx, pool)
	if err != nil {
		return err
	}

	queries := sqlc.New(pool)

	// The agent layer: runs execute here, in the background, and outlive the
	// requests that start them.
	runner := agent.NewRunner(pool, agent.NewHub(), api.NewModelResolver(pool), api.NewToolSource(pool),
		agent.Options{SystemPrompt: api.NewSystemPrompt(pool), Blobs: store, Modalities: api.NewModalities(pool),
			Compression: api.NewCompression(pool),
			Capacity:    api.NewCapacityCheck(pool),
			Reserver:    api.NewSpendReserver(pool)})
	// Runs a previous process left unfinished pick up from their checkpoints.
	if err := runner.Resume(ctx); err != nil {
		return err
	}

	server := api.NewServer(cfg, pool, store, runner)
	// A conversation's first answer gets it a title, in the background.
	runner.AfterRun(server.TitleAfterRun)
	// Before the listener opens, so a server started with debug logging on logs
	// its own startup in detail — which is when you want it most.
	server.LoadSettings(ctx)
	// Deliberately not waited on: the model catalogue comes from a third party,
	// and a boot that blocked on it would be a boot somebody else can hold up.
	server.StartCatalogRefresh(ctx)
	// Temporary chats are deleted a day after they were made.
	server.StartCleanup(ctx)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(cfg, server, auth.NewMiddleware(queries)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// No WriteTimeout: an events stream stays open as long as its page.
		//
		// Requests inherit the signal context, so a shutdown ends the open
		// events streams too. Shutdown waits for connections to go idle, and
		// a stream never would.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
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
	httpErr := srv.Shutdown(shutdownCtx)
	// Runs are stopped, not finished: they stay `running` and the next process
	// resumes them, so a deploy does not cancel anyone's answer.
	if err := runner.Shutdown(shutdownCtx); err != nil {
		slog.Warn("runs did not stop in time", "error", err)
	}
	return httpErr
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
	logging.Setup(cfg.IsProduction(), logging.File{
		Path:     cfg.LogFile,
		MaxBytes: int64(cfg.LogFileMB) << 20,
		Keep:     cfg.LogFileKeep,
	})
}

// logTimeZone says which zone the day and month boundaries are in. A model's
// daily and monthly spending caps start over at the server's local midnight,
// and Go reads an unknown TZ as UTC without a word.
func logTimeZone() {
	_, offset := time.Now().Zone()
	tz := os.Getenv("TZ")
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			slog.Warn("TZ is not a known time zone; days and months (spending caps) are counted in UTC", "tz", tz, "error", err)
			return
		}
	}
	slog.Info("time zone", "tz", tz, "utc_offset_hours", float64(offset)/3600)
}
