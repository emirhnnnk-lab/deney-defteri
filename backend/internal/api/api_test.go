package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emirhnnnk-lab/deney-defteri/internal/abtest"
	"github.com/emirhnnnk-lab/deney-defteri/internal/store"
	"github.com/emirhnnnk-lab/deney-defteri/internal/usagelog"
)

// fakeStore is an in-memory Store for handler tests. The SQL itself is covered
// by the integration tests in internal/store.
type fakeStore struct {
	exps     []store.Experiment
	imported []usagelog.Event
	armCalls []string
	failList bool
}

func (f *fakeStore) ListExperiments(context.Context) ([]store.Experiment, error) {
	if f.failList {
		return nil, errors.New("pq: connection refused at 10.0.0.5")
	}
	return f.exps, nil
}
func (f *fakeStore) GetExperiment(_ context.Context, id int64) (store.Experiment, error) {
	for _, e := range f.exps {
		if e.ID == id {
			return e, nil
		}
	}
	return store.Experiment{}, store.ErrNotFound
}
func (f *fakeStore) CreateExperiment(_ context.Context, e store.Experiment) (store.Experiment, error) {
	e.ID = int64(len(f.exps) + 1)
	f.exps = append(f.exps, e)
	return e, nil
}
func (f *fakeStore) UpdateExperiment(_ context.Context, id int64, e store.Experiment) (store.Experiment, error) {
	for i := range f.exps {
		if f.exps[i].ID == id {
			e.ID, e.Prediction = id, f.exps[i].Prediction
			f.exps[i] = e
			return e, nil
		}
	}
	return store.Experiment{}, store.ErrNotFound
}
func (f *fakeStore) ImportEvents(_ context.Context, ev []usagelog.Event) (int, error) {
	f.imported = append(f.imported, ev...)
	return len(ev), nil
}
func (f *fakeStore) ListEvents(context.Context, int) ([]usagelog.Event, error) {
	return f.imported, nil
}
func (f *fakeStore) ModelStats(context.Context) ([]store.ModelStat, error) { return nil, nil }
func (f *fakeStore) Arm(_ context.Context, dim, value, exp string) (abtest.Arm, error) {
	f.armCalls = append(f.armCalls, dim+"|"+value+"|"+exp)
	mean := 1.0
	return abtest.Arm{Label: value, Calls: 40, Successes: 40, Graded: 40, Correct: 30, MeanSec: &mean}, nil
}
func (f *fakeStore) DistinctValues(context.Context, string) ([]string, error) {
	return []string{"a", "b"}, nil
}
func (f *fakeStore) ExperimentIDs(context.Context) ([]string, error) { return []string{}, nil }

func newServer(t *testing.T, fs *fakeStore) http.Handler {
	t.Helper()
	s := &Server{Store: fs, LogLocation: time.UTC, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return s.Routes("")
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCreateExperimentValidation(t *testing.T) {
	h := newServer(t, &fakeStore{})
	cases := []struct {
		name, body string
		want       int
	}{
		{"ok", `{"title":"t","prediction":"p"}`, http.StatusCreated},
		{"missing prediction", `{"title":"t"}`, http.StatusBadRequest},
		{"blank title", `{"title":"   ","prediction":"p"}`, http.StatusBadRequest},
		{"bad outcome", `{"title":"t","prediction":"p","outcome":"maybe"}`, http.StatusBadRequest},
		{"unknown field", `{"title":"t","prediction":"p","admin":true}`, http.StatusBadRequest},
		{"not json", `title=t`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := do(t, h, "POST", "/api/experiments", c.body); rec.Code != c.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, c.want, rec.Body)
			}
		})
	}
}

func TestCreateDefaultsOutcomeToPending(t *testing.T) {
	fs := &fakeStore{}
	h := newServer(t, fs)
	do(t, h, "POST", "/api/experiments", `{"title":"t","prediction":"p"}`)
	if fs.exps[0].Outcome != "pending" {
		t.Fatalf("outcome = %q, want pending", fs.exps[0].Outcome)
	}
}

func TestGetAndUpdateExperiment(t *testing.T) {
	fs := &fakeStore{exps: []store.Experiment{{ID: 1, Title: "t", Prediction: "p", Outcome: "pending"}}}
	h := newServer(t, fs)
	if rec := do(t, h, "GET", "/api/experiments/1", ""); rec.Code != 200 {
		t.Fatalf("get = %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/api/experiments/42", ""); rec.Code != 404 {
		t.Fatalf("missing = %d, want 404", rec.Code)
	}
	if rec := do(t, h, "GET", "/api/experiments/abc", ""); rec.Code != 400 {
		t.Fatalf("bad id = %d, want 400", rec.Code)
	}
	rec := do(t, h, "PUT", "/api/experiments/1", `{"title":"t","result":"16/16","outcome":"confirmed"}`)
	if rec.Code != 200 {
		t.Fatalf("update = %d %s", rec.Code, rec.Body)
	}
}

func TestInternalErrorsDoNotLeakDetails(t *testing.T) {
	h := newServer(t, &fakeStore{failList: true})
	rec := do(t, h, "GET", "/api/experiments", "")
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Fatalf("got %d %s; want 500 without the database error", rec.Code, rec.Body)
	}
}

func TestImportReadsOnlyConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kullanim_log.jsonl")
	content := `{"gorev":"a","saglayici":"ollama","model":"m","basarili":true,"sure_sn":1,"zaman":"2026-11-10T10:00:00"}
broken
`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := &fakeStore{}
	s := &Server{Store: fs, LogPath: p, LogLocation: time.UTC, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.Routes("")

	// A path in the query or body is ignored: the server only reads LogPath.
	rec := do(t, h, "POST", "/api/usage/import?path=C:/Windows/win.ini", `{"path":"/etc/passwd"}`)
	if rec.Code != 200 {
		t.Fatalf("import = %d %s", rec.Code, rec.Body)
	}
	var res importResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Read != 1 || res.Inserted != 1 || len(res.Skipped) != 1 || res.Skipped[0].Line != 2 {
		t.Fatalf("import result wrong: %+v", res)
	}

	noLog := newServer(t, &fakeStore{})
	if rec := do(t, noLog, "POST", "/api/usage/import", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no log configured = %d, want 503", rec.Code)
	}
}

func TestABCompare(t *testing.T) {
	fs := &fakeStore{}
	h := newServer(t, fs)
	rec := do(t, h, "GET", "/api/ab?dim=model&a=x&b=y&experiment=e1", "")
	if rec.Code != 200 {
		t.Fatalf("ab = %d %s", rec.Code, rec.Body)
	}
	if len(fs.armCalls) != 2 || fs.armCalls[0] != "model|x|e1" || fs.armCalls[1] != "model|y|e1" {
		t.Fatalf("arm calls = %v", fs.armCalls)
	}
	for _, bad := range []string{"/api/ab?a=x", "/api/ab?a=x&b=x", "/api/ab?dim=password&a=x&b=y"} {
		if rec := do(t, h, "GET", bad, ""); rec.Code != 400 {
			t.Errorf("%s = %d, want 400", bad, rec.Code)
		}
	}
}

func TestUnknownAPIPathIs404JSON(t *testing.T) {
	rec := do(t, newServer(t, &fakeStore{}), "GET", "/api/nope", "")
	if rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
