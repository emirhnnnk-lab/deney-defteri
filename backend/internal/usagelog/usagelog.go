// Package usagelog parses the JSONL usage log written by the free-model bridge
// (araclar/belas-kopru/kopru.py). The bridge writes one JSON object per call.
// Failed calls carry no model or duration, so those fields are optional here.
package usagelog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Event is one model call as recorded by the bridge.
type Event struct {
	Task             string    `json:"task"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	Success          bool      `json:"success"`
	DurationSec      *float64  `json:"duration_sec"`
	InputChars       *int      `json:"input_chars"`
	OutputChars      *int      `json:"output_chars"`
	PromptTokens     *int      `json:"prompt_tokens"`
	CompletionTokens *int      `json:"completion_tokens"`
	Error            string    `json:"error,omitempty"`
	Experiment       string    `json:"experiment,omitempty"`
	Correct          *bool     `json:"correct"`
	OccurredAt       time.Time `json:"occurred_at"`
	// SourceHash identifies the raw line, so importing the same file twice
	// does not create duplicates.
	SourceHash string `json:"-"`
}

// raw mirrors the bridge's Turkish field names.
type raw struct {
	Gorev         string   `json:"gorev"`
	Saglayici     string   `json:"saglayici"`
	Model         string   `json:"model"`
	Basarili      *bool    `json:"basarili"`
	SureSn        *float64 `json:"sure_sn"`
	GirdiKarakter *int     `json:"girdi_karakter"`
	CiktiKarakter *int     `json:"cikti_karakter"`
	Hata          string   `json:"hata"`
	Deney         string   `json:"deney"`
	Dogru         *bool    `json:"dogru"`
	Zaman         string   `json:"zaman"`
	Kullanim      *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
	} `json:"kullanim"`
}

// LineError describes a line that could not be imported.
type LineError struct {
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Result is what Parse returns: good events plus a record of what was skipped.
type Result struct {
	Events  []Event     `json:"events"`
	Skipped []LineError `json:"skipped"`
}

// The bridge writes local time without a zone (Python isoformat, seconds).
const bridgeTimeLayout = "2006-01-02T15:04:05"

// Parse reads a JSONL stream. It never fails on a single bad line: bad lines
// are reported in Result.Skipped, so one corrupt write cannot block an import.
// loc is the time zone the bridge ran in (its timestamps carry no zone).
func Parse(r io.Reader, loc *time.Location) (Result, error) {
	var res Result
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ev, err := parseLine(line, loc)
		if err != nil {
			res.Skipped = append(res.Skipped, LineError{Line: n, Reason: err.Error()})
			continue
		}
		res.Events = append(res.Events, ev)
	}
	if err := sc.Err(); err != nil {
		return res, fmt.Errorf("read log: %w", err)
	}
	return res, nil
}

func parseLine(line string, loc *time.Location) (Event, error) {
	var r raw
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return Event{}, fmt.Errorf("invalid json: %v", err)
	}
	if r.Basarili == nil {
		return Event{}, fmt.Errorf("missing field basarili")
	}
	if r.Zaman == "" {
		return Event{}, fmt.Errorf("missing field zaman")
	}
	t, err := time.ParseInLocation(bridgeTimeLayout, r.Zaman, loc)
	if err != nil {
		return Event{}, fmt.Errorf("bad zaman %q", r.Zaman)
	}
	if r.SureSn != nil && *r.SureSn < 0 {
		return Event{}, fmt.Errorf("negative sure_sn")
	}
	sum := sha256.Sum256([]byte(line))
	ev := Event{
		Task:        r.Gorev,
		Provider:    r.Saglayici,
		Model:       r.Model,
		Success:     *r.Basarili,
		DurationSec: r.SureSn,
		InputChars:  r.GirdiKarakter,
		OutputChars: r.CiktiKarakter,
		Error:       r.Hata,
		Experiment:  r.Deney,
		Correct:     r.Dogru,
		OccurredAt:  t,
		SourceHash:  hex.EncodeToString(sum[:]),
	}
	if ev.Model == "" {
		ev.Model = "unknown"
	}
	if r.Kullanim != nil {
		ev.PromptTokens = r.Kullanim.PromptTokens
		ev.CompletionTokens = r.Kullanim.CompletionTokens
	}
	return ev, nil
}
