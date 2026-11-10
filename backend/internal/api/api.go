// Package api is the JSON HTTP layer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emirhnnnk-lab/deney-defteri/internal/abtest"
	"github.com/emirhnnnk-lab/deney-defteri/internal/store"
	"github.com/emirhnnnk-lab/deney-defteri/internal/usagelog"
)

// Store is what the handlers need from the database. *store.Store implements it;
// tests use an in-memory fake.
type Store interface {
	ListExperiments(ctx context.Context) ([]store.Experiment, error)
	GetExperiment(ctx context.Context, id int64) (store.Experiment, error)
	CreateExperiment(ctx context.Context, e store.Experiment) (store.Experiment, error)
	UpdateExperiment(ctx context.Context, id int64, e store.Experiment) (store.Experiment, error)
	ImportEvents(ctx context.Context, events []usagelog.Event) (int, error)
	ListEvents(ctx context.Context, limit int) ([]usagelog.Event, error)
	ModelStats(ctx context.Context) ([]store.ModelStat, error)
	Arm(ctx context.Context, dim, value, experiment string) (abtest.Arm, error)
	DistinctValues(ctx context.Context, dim string) ([]string, error)
	ExperimentIDs(ctx context.Context) ([]string, error)
}

// Server holds the dependencies of the handlers.
type Server struct {
	Store Store
	// LogPath is the bridge's usage log. Import reads only this file: the path
	// is server configuration, never taken from the request.
	LogPath string
	// LogLocation is the time zone the bridge's timestamps were written in.
	LogLocation *time.Location
	Logger      *slog.Logger
}

// Routes returns the API mux. staticDir, when not empty, is served for every
// non-API path (the built React app), with index.html as the SPA fallback.
func (s *Server) Routes(staticDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/experiments", s.listExperiments)
	mux.HandleFunc("POST /api/experiments", s.createExperiment)
	mux.HandleFunc("GET /api/experiments/{id}", s.getExperiment)
	mux.HandleFunc("PUT /api/experiments/{id}", s.updateExperiment)
	mux.HandleFunc("POST /api/usage/import", s.importUsage)
	mux.HandleFunc("GET /api/usage/events", s.listEvents)
	mux.HandleFunc("GET /api/usage/models", s.modelStats)
	mux.HandleFunc("GET /api/ab/options", s.abOptions)
	mux.HandleFunc("GET /api/ab", s.abCompare)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	if staticDir != "" {
		mux.Handle("/", spa(staticDir))
	}
	return s.logRequests(mux)
}

const maxBody = 64 << 10

type experimentInput struct {
	Slug       *string `json:"slug"`
	Title      string  `json:"title"`
	Prediction string  `json:"prediction"`
	Action     string  `json:"action"`
	Result     string  `json:"result"`
	Lesson     string  `json:"lesson"`
	Outcome    string  `json:"outcome"`
	Source     string  `json:"source"`
}

func (in *experimentInput) validate(creating bool) error {
	in.Title = strings.TrimSpace(in.Title)
	in.Prediction = strings.TrimSpace(in.Prediction)
	if in.Title == "" || len(in.Title) > 200 {
		return errors.New("title is required (max 200 chars)")
	}
	if creating && in.Prediction == "" {
		return errors.New("prediction is required: write it before you run the experiment")
	}
	if in.Outcome == "" {
		in.Outcome = "pending"
	}
	for _, o := range store.Outcomes {
		if in.Outcome == o {
			return nil
		}
	}
	return errors.New("outcome must be one of pending, confirmed, refuted, inconclusive")
}

func (in experimentInput) toModel() store.Experiment {
	return store.Experiment{Slug: in.Slug, Title: in.Title, Prediction: in.Prediction, Action: in.Action,
		Result: in.Result, Lesson: in.Lesson, Outcome: in.Outcome, Source: in.Source}
}

func (s *Server) listExperiments(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.ListExperiments(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getExperiment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	e, err := s.Store.GetExperiment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "experiment not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) createExperiment(w http.ResponseWriter, r *http.Request) {
	var in experimentInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.validate(true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.Store.CreateExperiment(r.Context(), in.toModel())
	if store.IsConstraintError(err) {
		writeError(w, http.StatusConflict, "experiment conflicts with an existing one (duplicate slug?)")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) updateExperiment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in experimentInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.validate(false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := s.Store.UpdateExperiment(r.Context(), id, in.toModel())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "experiment not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

type importResult struct {
	Read     int                  `json:"read"`
	Inserted int                  `json:"inserted"`
	Skipped  []usagelog.LineError `json:"skipped"`
}

func (s *Server) importUsage(w http.ResponseWriter, r *http.Request) {
	if s.LogPath == "" {
		writeError(w, http.StatusServiceUnavailable, "no usage log configured (start the server with -log)")
		return
	}
	f, err := os.Open(s.LogPath)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "usage log not readable")
		s.Logger.Error("open usage log", "path", s.LogPath, "err", err)
		return
	}
	defer f.Close()
	res, err := usagelog.Parse(f, s.LogLocation)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	n, err := s.Store.ImportEvents(r.Context(), res.Events)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	skipped := res.Skipped
	if skipped == nil {
		skipped = []usagelog.LineError{}
	}
	writeJSON(w, http.StatusOK, importResult{Read: len(res.Events), Inserted: n, Skipped: skipped})
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be 1..1000")
			return
		}
		limit = n
	}
	out, err := s.Store.ListEvents(r.Context(), limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) modelStats(w http.ResponseWriter, r *http.Request) {
	out, err := s.Store.ModelStats(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func dimension(w http.ResponseWriter, r *http.Request) (string, bool) {
	dim := r.URL.Query().Get("dim")
	if dim == "" {
		dim = "model"
	}
	if _, ok := store.Dimensions[dim]; !ok {
		writeError(w, http.StatusBadRequest, "dim must be model, provider or task")
		return "", false
	}
	return dim, true
}

func (s *Server) abOptions(w http.ResponseWriter, r *http.Request) {
	dim, ok := dimension(w, r)
	if !ok {
		return
	}
	values, err := s.Store.DistinctValues(r.Context(), dim)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	exps, err := s.Store.ExperimentIDs(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"values": values, "experiments": exps})
}

func (s *Server) abCompare(w http.ResponseWriter, r *http.Request) {
	dim, ok := dimension(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	a, b, exp := q.Get("a"), q.Get("b"), q.Get("experiment")
	if a == "" || b == "" {
		writeError(w, http.StatusBadRequest, "a and b are required")
		return
	}
	if a == b {
		writeError(w, http.StatusBadRequest, "a and b must differ")
		return
	}
	armA, err := s.Store.Arm(r.Context(), dim, a, exp)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	armB, err := s.Store.Arm(r.Context(), dim, b, exp)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, abtest.Compare(armA, armB))
}

// ---- helpers ----

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internal logs the real error and returns a generic message: database
// details do not leak to the client.
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.Logger.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
				"ms", time.Since(start).Milliseconds())
		}
	})
}

func spa(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+r.URL.Path)))
		if _, err := os.Stat(p); r.URL.Path != "/" && err != nil {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
}
