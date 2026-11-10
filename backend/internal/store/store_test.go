package store

import (
	"context"
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emirhnnnk-lab/deney-defteri/internal/devdb"
	"github.com/emirhnnnk-lab/deney-defteri/internal/usagelog"
)

// Integration tests against a real Postgres (embedded, throwaway data dir).
// Set DATABASE_URL to run them against an existing server instead.
// `go test -short` skips them.

var testStore *Store

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	url := os.Getenv("DATABASE_URL")
	var db *devdb.DB
	if url == "" {
		var err error
		db, err = devdb.Start("", 0)
		if err != nil {
			panic(err)
		}
		url = db.URL
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		panic(err)
	}
	testStore = s
	code := m.Run()
	s.Close()
	if db != nil {
		_ = db.Stop()
	}
	os.Exit(code)
}

func need(t *testing.T) *Store {
	t.Helper()
	if testStore == nil {
		t.Skip("integration test (run without -short)")
	}
	return testStore
}

func TestExperimentLifecycle(t *testing.T) {
	s := need(t)
	ctx := context.Background()

	e, err := s.CreateExperiment(ctx, Experiment{Title: "t1", Prediction: "p", Outcome: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if e.DecidedAt != nil {
		t.Fatal("pending experiment must not have decided_at")
	}

	e.Result, e.Outcome = "16/16", "confirmed"
	e.Prediction = "rewritten after the fact"
	u, err := s.UpdateExperiment(ctx, e.ID, e)
	if err != nil {
		t.Fatal(err)
	}
	if u.DecidedAt == nil || u.Outcome != "confirmed" {
		t.Fatalf("decided_at should be set when the outcome is decided: %+v", u)
	}
	if u.Prediction != "p" {
		t.Fatalf("prediction must not change after creation, got %q", u.Prediction)
	}

	if _, err := s.GetExperiment(ctx, 999999); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	_, err = s.CreateExperiment(ctx, Experiment{Title: "bad", Prediction: "p", Outcome: "maybe"})
	if !IsConstraintError(err) {
		t.Fatalf("invalid outcome must hit the CHECK constraint, got %v", err)
	}
}

func TestImportIsIdempotentAndStatsAreRight(t *testing.T) {
	s := need(t)
	ctx := context.Background()
	log := strings.Join([]string{
		`{"gorev":"ab","saglayici":"ollama","model":"m-a","basarili":true,"sure_sn":1.0,"deney":"exp-x","dogru":true,"zaman":"2026-11-10T10:00:00"}`,
		`{"gorev":"ab","saglayici":"ollama","model":"m-a","basarili":true,"sure_sn":3.0,"deney":"exp-x","dogru":false,"zaman":"2026-11-10T10:00:01"}`,
		`{"gorev":"ab","saglayici":"ollama","model":"m-a","basarili":false,"hata":"Timeout","deney":"exp-x","zaman":"2026-11-10T10:00:02"}`,
		`{"gorev":"ab","saglayici":"ollama","model":"m-b","basarili":true,"sure_sn":0.5,"deney":"exp-x","dogru":true,"zaman":"2026-11-10T10:00:03"}`,
		`{"gorev":"other","saglayici":"ollama","model":"m-b","basarili":true,"sure_sn":9.0,"zaman":"2026-11-10T10:00:04"}`,
	}, "\n")
	res, err := usagelog.Parse(strings.NewReader(log), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.ImportEvents(ctx, res.Events)
	if err != nil || n != 5 {
		t.Fatalf("first import: n=%d err=%v", n, err)
	}
	n, err = s.ImportEvents(ctx, res.Events)
	if err != nil || n != 0 {
		t.Fatalf("second import must insert nothing: n=%d err=%v", n, err)
	}

	a, err := s.Arm(ctx, "model", "m-a", "exp-x")
	if err != nil {
		t.Fatal(err)
	}
	if a.Calls != 3 || a.Successes != 2 || a.Graded != 2 || a.Correct != 1 {
		t.Fatalf("arm counts wrong: %+v", a)
	}
	if a.MeanSec == nil || *a.MeanSec != 2.0 {
		t.Fatalf("mean must only use successful calls: %v", a.MeanSec)
	}

	// Without the experiment filter m-b includes the unrelated 9s call.
	b, _ := s.Arm(ctx, "model", "m-b", "")
	bx, _ := s.Arm(ctx, "model", "m-b", "exp-x")
	if b.Calls != 2 || bx.Calls != 1 {
		t.Fatalf("experiment filter wrong: all=%d exp=%d", b.Calls, bx.Calls)
	}

	if _, err := s.Arm(ctx, "model; DROP TABLE usage_events", "x", ""); err == nil {
		t.Fatal("unknown dimension must be rejected")
	}
}

// Added after the first end-to-end run: the seed query failed on a real
// Postgres with "inconsistent types deduced for parameter $9" while every
// other test was green, because no test exercised the upsert.
func TestUpsertExperimentBySlug(t *testing.T) {
	s := need(t)
	ctx := context.Background()
	slug := "seed-x"
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	e := Experiment{Slug: &slug, Title: "v1", Prediction: "p", Outcome: "refuted"}
	if err := s.UpsertExperimentBySlug(ctx, e, day); err != nil {
		t.Fatal(err)
	}
	e.Title = "v2"
	if err := s.UpsertExperimentBySlug(ctx, e, day); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListExperiments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, x := range all {
		if x.Slug != nil && *x.Slug == slug {
			n++
			if x.Title != "v2" || x.DecidedAt == nil || !x.CreatedAt.Equal(day) {
				t.Fatalf("upsert row wrong: %+v", x)
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one row for the slug, got %d", n)
	}
	pending := "seed-pending"
	if err := s.UpsertExperimentBySlug(ctx, Experiment{Slug: &pending, Title: "t", Prediction: "p", Outcome: "pending"}, day); err != nil {
		t.Fatal(err)
	}
}
