package usagelog

import (
	"strings"
	"testing"
	"time"
)

// First line is copied verbatim from the bridge's real log; the failure line
// matches what kopru.py writes when a provider errors (no model, no duration).
const sample = `{"gorev": "calistir", "saglayici": "ollama", "model": "qwen2.5:7b", "basarili": true, "sure_sn": 16.5, "girdi_karakter": 10, "cikti_karakter": 35, "kullanim": {"prompt_tokens": 37, "prompt_tokens_details": {"cached_tokens": 0}, "completion_tokens": 11, "total_tokens": 48}, "zaman": "2026-11-10T19:46:08"}
{"gorev": "ozetle", "saglayici": "gemini", "basarili": false, "hata": "429", "zaman": "2026-11-10T20:01:00"}

not json at all
{"gorev": "x", "saglayici": "ollama", "zaman": "2026-11-10T20:02:00"}
{"gorev": "ab-siniflama", "saglayici": "ollama", "model": "qwen2.5:3b", "basarili": true, "sure_sn": 0.8, "deney": "ab-1", "dogru": false, "zaman": "2026-11-10T20:03:00"}
{"gorev": "x", "saglayici": "ollama", "basarili": true, "sure_sn": -1, "zaman": "2026-11-10T20:04:00"}
`

func TestParse(t *testing.T) {
	loc := time.FixedZone("TRT", 3*3600)
	res, err := Parse(strings.NewReader(sample), loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.Events); got != 3 {
		t.Fatalf("events = %d, want 3", got)
	}
	if got := len(res.Skipped); got != 3 {
		t.Fatalf("skipped = %d, want 3: %+v", got, res.Skipped)
	}
	// Line numbers must point at the real file lines (blank line 3 is not an error).
	wantLines := []int{4, 5, 7}
	for i, s := range res.Skipped {
		if s.Line != wantLines[i] {
			t.Errorf("skipped[%d].Line = %d, want %d (%s)", i, s.Line, wantLines[i], s.Reason)
		}
	}

	ok := res.Events[0]
	if ok.Model != "qwen2.5:7b" || !ok.Success || *ok.DurationSec != 16.5 {
		t.Errorf("first event parsed wrong: %+v", ok)
	}
	if *ok.PromptTokens != 37 || *ok.CompletionTokens != 11 {
		t.Errorf("tokens parsed wrong: %d/%d", *ok.PromptTokens, *ok.CompletionTokens)
	}
	if want := time.Date(2026, 11, 10, 16, 46, 8, 0, time.UTC); !ok.OccurredAt.Equal(want) {
		t.Errorf("time = %v, want %v (bridge writes local time)", ok.OccurredAt.UTC(), want)
	}

	fail := res.Events[1]
	if fail.Success || fail.Model != "unknown" || fail.DurationSec != nil || fail.Error != "429" {
		t.Errorf("failed call parsed wrong: %+v", fail)
	}

	exp := res.Events[2]
	if exp.Experiment != "ab-1" || exp.Correct == nil || *exp.Correct {
		t.Errorf("experiment fields parsed wrong: %+v", exp)
	}
}

func TestSourceHashIsStable(t *testing.T) {
	line := `{"gorev": "a", "saglayici": "b", "basarili": true, "zaman": "2026-11-10T10:00:00"}`
	a, _ := Parse(strings.NewReader(line), time.UTC)
	b, _ := Parse(strings.NewReader(line+"\n"), time.UTC)
	if a.Events[0].SourceHash != b.Events[0].SourceHash {
		t.Fatal("same line must give the same hash, or re-imports duplicate rows")
	}
}
