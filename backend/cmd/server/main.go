// Command server runs the Deney Defteri API (and serves the built web app).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emirhnnnk-lab/deney-defteri/internal/api"
	"github.com/emirhnnnk-lab/deney-defteri/internal/devdb"
	"github.com/emirhnnnk-lab/deney-defteri/internal/store"
	"github.com/emirhnnnk-lab/deney-defteri/internal/usagelog"
)

type seedExperiment struct {
	store.Experiment
	CreatedAt string `json:"created_at"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL URL; empty starts an embedded Postgres")
	dataDir := flag.String("data", ".pgdata", "data directory for the embedded Postgres")
	logPath := flag.String("log", "", "path to the bridge's kullanim_log.jsonl")
	tz := flag.String("tz", "Europe/Istanbul", "time zone the bridge wrote its timestamps in")
	static := flag.String("static", "../web/dist", "built web app to serve (empty to disable)")
	seed := flag.String("seed", "seed/experiments.json", "experiments to upsert on start (empty to skip)")
	importOnStart := flag.Bool("import", true, "import the usage log on start")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger, *addr, *dbURL, *dataDir, *logPath, *tz, *static, *seed, *importOnStart); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, addr, dbURL, dataDir, logPath, tz, static, seed string, importOnStart bool) error {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("time zone %q: %w", tz, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if dbURL == "" {
		logger.Info("starting embedded postgres", "data", dataDir)
		db, err := devdb.Start(dataDir, 0)
		if err != nil {
			return err
		}
		defer func() {
			if err := db.Stop(); err != nil {
				logger.Error("stop postgres", "err", err)
			}
		}()
		dbURL = db.URL
	}

	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if seed != "" {
		n, err := loadSeed(ctx, st, seed)
		if err != nil {
			return fmt.Errorf("seed: %w", err)
		}
		logger.Info("seeded experiments", "count", n)
	}
	if logPath != "" && importOnStart {
		if err := importLog(ctx, st, logPath, loc, logger); err != nil {
			logger.Warn("usage log import failed; continuing", "err", err)
		}
	}

	if static != "" {
		if _, err := os.Stat(static); err != nil {
			logger.Warn("web build not found; serving API only", "static", static)
			static = ""
		}
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           (&api.Server{Store: st, LogPath: logPath, LogLocation: loc, Logger: logger}).Routes(static),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "url", "http://"+addr)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

func loadSeed(ctx context.Context, st *store.Store, path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var items []seedExperiment
	if err := json.Unmarshal(b, &items); err != nil {
		return 0, err
	}
	for _, it := range items {
		created, err := time.Parse("2006-01-02", it.CreatedAt)
		if err != nil {
			return 0, fmt.Errorf("%v: bad created_at %q", it.Slug, it.CreatedAt)
		}
		if err := st.UpsertExperimentBySlug(ctx, it.Experiment, created); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func importLog(ctx context.Context, st *store.Store, path string, loc *time.Location, logger *slog.Logger) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	res, err := usagelog.Parse(f, loc)
	if err != nil {
		return err
	}
	n, err := st.ImportEvents(ctx, res.Events)
	if err != nil {
		return err
	}
	logger.Info("imported usage log", "read", len(res.Events), "new", n, "skipped", len(res.Skipped))
	return nil
}
