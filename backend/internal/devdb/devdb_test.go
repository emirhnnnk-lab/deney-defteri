package devdb

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Reproduces the hard-kill case: a server is left running on a data dir and
// its owner is gone. A second Start on the same dir must recover, not fail
// with 'lock file "postmaster.pid" already exists'.
func TestStartRecoversFromOrphanedServer(t *testing.T) {
	if testing.Short() {
		t.Skip("starts real Postgres servers")
	}
	dir := t.TempDir() + "/pgdata"
	orphan, err := Start(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = orphan // deliberately never stopped by its owner

	db, err := Start(dir, 0)
	if err != nil {
		_ = orphan.Stop()
		t.Fatalf("second start on the same data dir failed: %v", err)
	}
	defer db.Stop() //nolint:errcheck

	conn, err := pgx.Connect(context.Background(), db.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var one int
	if err := conn.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatalf("query on recovered server: %v", err)
	}
}
