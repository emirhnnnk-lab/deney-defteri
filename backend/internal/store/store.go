// Package store is the PostgreSQL layer.
package store

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/emirhnnnk-lab/deney-defteri/internal/abtest"
	"github.com/emirhnnnk-lab/deney-defteri/internal/usagelog"
)

//go:embed schema.sql
var schema string

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// Outcomes allowed for an experiment (mirrors the CHECK constraint).
var Outcomes = []string{"pending", "confirmed", "refuted", "inconclusive"}

// Experiment is one row of the notebook: prediction -> action -> result -> lesson.
type Experiment struct {
	ID         int64      `json:"id"`
	Slug       *string    `json:"slug"`
	Title      string     `json:"title"`
	Prediction string     `json:"prediction"`
	Action     string     `json:"action"`
	Result     string     `json:"result"`
	Lesson     string     `json:"lesson"`
	Outcome    string     `json:"outcome"`
	Source     string     `json:"source"`
	CreatedAt  time.Time  `json:"created_at"`
	DecidedAt  *time.Time `json:"decided_at"`
}

// ModelStat is the per-model summary in the usage view.
type ModelStat struct {
	Model       string   `json:"model"`
	Provider    string   `json:"provider"`
	Calls       int      `json:"calls"`
	Successes   int      `json:"successes"`
	SuccessRate *float64 `json:"success_rate"`
	MeanSec     *float64 `json:"mean_sec"`
	MedianSec   *float64 `json:"median_sec"`
	Graded      int      `json:"graded"`
	Correct     int      `json:"correct"`
	Accuracy    *float64 `json:"accuracy"`
}

// Store wraps a connection pool.
type Store struct{ pool *pgxpool.Pool }

// Open connects and applies the schema.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

const experimentCols = `id, slug, title, prediction, action, result, lesson, outcome, source, created_at, decided_at`

func scanExperiment(row pgx.Row) (Experiment, error) {
	var e Experiment
	err := row.Scan(&e.ID, &e.Slug, &e.Title, &e.Prediction, &e.Action, &e.Result,
		&e.Lesson, &e.Outcome, &e.Source, &e.CreatedAt, &e.DecidedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// ListExperiments returns all experiments, newest first.
func (s *Store) ListExperiments(ctx context.Context) ([]Experiment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+experimentCols+` FROM experiments ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Experiment{}
	for rows.Next() {
		e, err := scanExperiment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetExperiment returns one experiment.
func (s *Store) GetExperiment(ctx context.Context, id int64) (Experiment, error) {
	return scanExperiment(s.pool.QueryRow(ctx, `SELECT `+experimentCols+` FROM experiments WHERE id = $1`, id))
}

// CreateExperiment inserts a new experiment. decided_at is set when the outcome is not pending.
func (s *Store) CreateExperiment(ctx context.Context, e Experiment) (Experiment, error) {
	return scanExperiment(s.pool.QueryRow(ctx, `
		INSERT INTO experiments (slug, title, prediction, action, result, lesson, outcome, source, decided_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CASE WHEN $7::text = 'pending' THEN NULL ELSE now() END)
		RETURNING `+experimentCols,
		e.Slug, e.Title, e.Prediction, e.Action, e.Result, e.Lesson, e.Outcome, e.Source))
}

// UpdateExperiment replaces the editable fields. The prediction is deliberately
// not editable once written: changing it after the result would defeat the point.
func (s *Store) UpdateExperiment(ctx context.Context, id int64, e Experiment) (Experiment, error) {
	return scanExperiment(s.pool.QueryRow(ctx, `
		UPDATE experiments SET
			title = $2, action = $3, result = $4, lesson = $5, outcome = $6, source = $7,
			decided_at = CASE
				WHEN $6 = 'pending' THEN NULL
				WHEN outcome = 'pending' THEN now()
				ELSE decided_at END
		WHERE id = $1
		RETURNING `+experimentCols,
		id, e.Title, e.Action, e.Result, e.Lesson, e.Outcome, e.Source))
}

// UpsertExperimentBySlug is used by the seed loader: it inserts a new row or
// refreshes an existing one with the same slug.
func (s *Store) UpsertExperimentBySlug(ctx context.Context, e Experiment, createdAt time.Time) error {
	if e.Slug == nil {
		return errors.New("seed experiment needs a slug")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO experiments (slug, title, prediction, action, result, lesson, outcome, source, created_at, decided_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, CASE WHEN $7::text = 'pending' THEN NULL ELSE $9::timestamptz END)
		ON CONFLICT (slug) DO UPDATE SET
			title = EXCLUDED.title, prediction = EXCLUDED.prediction, action = EXCLUDED.action,
			result = EXCLUDED.result, lesson = EXCLUDED.lesson, outcome = EXCLUDED.outcome,
			source = EXCLUDED.source`,
		e.Slug, e.Title, e.Prediction, e.Action, e.Result, e.Lesson, e.Outcome, e.Source, createdAt)
	return err
}

// ImportEvents inserts usage events in one transaction and returns how many
// were new. Lines already imported (same source hash) are skipped.
func (s *Store) ImportEvents(ctx context.Context, events []usagelog.Event) (inserted int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	for _, ev := range events {
		tag, err := tx.Exec(ctx, `
			INSERT INTO usage_events (source_hash, task, provider, model, success, duration_sec,
				input_chars, output_chars, prompt_tokens, completion_tokens, error, experiment, correct, occurred_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (source_hash) DO NOTHING`,
			ev.SourceHash, ev.Task, ev.Provider, ev.Model, ev.Success, ev.DurationSec,
			ev.InputChars, ev.OutputChars, ev.PromptTokens, ev.CompletionTokens,
			ev.Error, ev.Experiment, ev.Correct, ev.OccurredAt)
		if err != nil {
			return 0, fmt.Errorf("insert event: %w", err)
		}
		inserted += int(tag.RowsAffected())
	}
	return inserted, tx.Commit(ctx)
}

// ListEvents returns the most recent events.
func (s *Store) ListEvents(ctx context.Context, limit int) ([]usagelog.Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT task, provider, model, success, duration_sec, input_chars, output_chars,
		       prompt_tokens, completion_tokens, error, experiment, correct, occurred_at
		FROM usage_events ORDER BY occurred_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []usagelog.Event{}
	for rows.Next() {
		var e usagelog.Event
		if err := rows.Scan(&e.Task, &e.Provider, &e.Model, &e.Success, &e.DurationSec, &e.InputChars,
			&e.OutputChars, &e.PromptTokens, &e.CompletionTokens, &e.Error, &e.Experiment,
			&e.Correct, &e.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ModelStats aggregates usage per model and provider. Durations only count successful calls.
func (s *Store) ModelStats(ctx context.Context) ([]ModelStat, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT model, provider, count(*), count(*) FILTER (WHERE success),
		       avg(duration_sec) FILTER (WHERE success),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_sec) FILTER (WHERE success),
		       count(correct), count(*) FILTER (WHERE correct)
		FROM usage_events GROUP BY model, provider ORDER BY count(*) DESC, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ModelStat{}
	for rows.Next() {
		var m ModelStat
		if err := rows.Scan(&m.Model, &m.Provider, &m.Calls, &m.Successes, &m.MeanSec, &m.MedianSec,
			&m.Graded, &m.Correct); err != nil {
			return nil, err
		}
		m.SuccessRate = abtest.Rate(m.Successes, m.Calls)
		m.Accuracy = abtest.Rate(m.Correct, m.Graded)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Dimensions that can be compared in the A/B view, mapped to their column.
var Dimensions = map[string]string{"model": "model", "provider": "provider", "task": "task"}

// Arm aggregates one side of an A/B comparison. dim must be a key of Dimensions;
// experiment, when not empty, restricts the rows to that experiment.
func (s *Store) Arm(ctx context.Context, dim, value, experiment string) (abtest.Arm, error) {
	col, ok := Dimensions[dim]
	if !ok {
		return abtest.Arm{}, fmt.Errorf("unknown dimension %q", dim)
	}
	// col comes from the whitelist above, never from the request.
	q := `SELECT count(*), count(*) FILTER (WHERE success), count(correct), count(*) FILTER (WHERE correct),
	             avg(duration_sec) FILTER (WHERE success),
	             percentile_cont(0.5) WITHIN GROUP (ORDER BY duration_sec) FILTER (WHERE success)
	      FROM usage_events WHERE ` + col + ` = $1 AND ($2 = '' OR experiment = $2)`
	a := abtest.Arm{Label: value}
	err := s.pool.QueryRow(ctx, q, value, experiment).Scan(&a.Calls, &a.Successes, &a.Graded, &a.Correct, &a.MeanSec, &a.MedianSec)
	return a, err
}

// DistinctValues lists the values of a dimension, for the A/B dropdowns.
func (s *Store) DistinctValues(ctx context.Context, dim string) ([]string, error) {
	col, ok := Dimensions[dim]
	if !ok {
		return nil, fmt.Errorf("unknown dimension %q", dim)
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT `+col+` FROM usage_events ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ExperimentIDs lists the experiment ids that appear in the usage log.
func (s *Store) ExperimentIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT experiment FROM usage_events WHERE experiment <> '' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// IsConstraintError reports whether err is a check/unique violation from Postgres,
// so the API can answer 400/409 instead of 500.
func IsConstraintError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "23514" || pgErr.Code == "23505")
}
