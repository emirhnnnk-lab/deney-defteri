// Package devdb starts a real PostgreSQL server as a child process, so the app
// and its integration tests run without Docker or a system-wide install.
// The binaries are downloaded once (Maven Central, zonky.io builds) and cached.
package devdb

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

// DB is a running embedded Postgres.
type DB struct {
	pg      *embeddedpostgres.EmbeddedPostgres
	URL     string
	cleanup string // throwaway directory removed on Stop (tests only)
}

// Start launches Postgres 16 with its data in dataDir. An empty dataDir means
// a throwaway directory (used by tests). port 0 picks a free port.
func Start(dataDir string, port uint32) (*DB, error) {
	if port == 0 {
		p, err := freePort()
		if err != nil {
			return nil, err
		}
		port = p
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	// A persistent data dir gets a fixed runtime dir next to it, so a later
	// start can find pg_ctl and shut down a server orphaned by a hard kill.
	// Tests (empty dataDir) get a throwaway dir that Stop removes.
	var runtime, cleanup string
	var absData string
	if dataDir != "" {
		absData, err = filepath.Abs(dataDir)
		if err != nil {
			return nil, err
		}
		runtime = absData + "-runtime"
		stopOrphan(runtime, absData)
	} else {
		runtime, err = os.MkdirTemp("", "deney-defteri-pg-")
		if err != nil {
			return nil, err
		}
		cleanup = runtime
	}
	cfg := embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V16).
		Port(port).
		Database("deney_defteri").
		Username("deney").
		Password("deney").
		RuntimePath(runtime).
		CachePath(filepath.Join(cache, "deney-defteri", "pg-binaries")).
		StartTimeout(60 * time.Second).
		// initdb crashes (0xC0000409) under the Turkish Windows locale; pin a neutral one.
		Locale("C").
		Encoding("UTF8").
		Logger(nil)
	if absData != "" {
		cfg = cfg.DataPath(absData)
	}
	pg := embeddedpostgres.NewDatabase(cfg)
	if err := pg.Start(); err != nil {
		return nil, fmt.Errorf("start embedded postgres: %w", err)
	}
	return &DB{
		pg:      pg,
		URL:     fmt.Sprintf("postgres://deney:deney@localhost:%d/deney_defteri?sslmode=disable", port),
		cleanup: cleanup,
	}, nil
}

// Stop shuts the server down.
func (d *DB) Stop() error {
	err := d.pg.Stop()
	if d.cleanup != "" {
		_ = os.RemoveAll(d.cleanup)
	}
	return err
}

// stopOrphan shuts down a Postgres left running on dataDir by a previous
// process that was killed before it could stop it (seen in practice: the
// next start failed with 'lock file "postmaster.pid" already exists').
// Errors are ignored: if nothing is running, Start proceeds as normal, and
// if something else holds the lock, Start reports it.
func stopOrphan(runtime, dataDir string) {
	if _, err := os.Stat(filepath.Join(dataDir, "postmaster.pid")); err != nil {
		return
	}
	pgCtl := filepath.Join(runtime, "bin", "pg_ctl")
	if goruntime.GOOS == "windows" {
		pgCtl += ".exe"
	}
	if _, err := os.Stat(pgCtl); err != nil {
		return
	}
	_ = exec.Command(pgCtl, "stop", "-D", dataDir, "-m", "fast", "-w").Run()
}

func freePort() (uint32, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return uint32(l.Addr().(*net.TCPAddr).Port), nil
}
