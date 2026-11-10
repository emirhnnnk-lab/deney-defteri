package abtest

import (
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestTwoProportionPValue(t *testing.T) {
	// Reference value: 45/100 vs 60/100 -> z ≈ 2.1232, two-sided p ≈ 0.0337
	// (same as statsmodels proportions_ztest with pooled variance).
	p := TwoProportionPValue(45, 100, 60, 100)
	if p == nil || math.Abs(*p-0.0337) > 0.0005 {
		t.Fatalf("p = %v, want ≈0.0337", p)
	}
	if TwoProportionPValue(0, 0, 5, 10) != nil {
		t.Error("empty arm must give nil, not a number")
	}
	if TwoProportionPValue(10, 10, 10, 10) != nil {
		t.Error("both 100% has zero variance; p must be nil")
	}
	if p := TwoProportionPValue(5, 10, 5, 10); p == nil || math.Abs(*p-1) > 1e-9 {
		t.Errorf("identical rates must give p = 1, got %v", p)
	}
}

func TestCompare(t *testing.T) {
	a := Arm{Label: "3b", Calls: 60, Successes: 60, Graded: 60, Correct: 42, MeanSec: f(0.5)}
	b := Arm{Label: "7b", Calls: 60, Successes: 60, Graded: 60, Correct: 54, MeanSec: f(1.0)}
	c := Compare(a, b)
	if math.Abs(*c.AccuracyDiff-0.2) > 1e-9 {
		t.Errorf("diff = %v, want 0.2", *c.AccuracyDiff)
	}
	if math.Abs(*c.SpeedRatio-0.5) > 1e-9 {
		t.Errorf("speed ratio = %v, want 0.5 (B is slower)", *c.SpeedRatio)
	}
	if len(c.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", c.Warnings)
	}
}

func TestCompareWarnsOnSmallOrUngradedSamples(t *testing.T) {
	c := Compare(Arm{Calls: 5, Successes: 5}, Arm{Calls: 40, Successes: 39})
	if len(c.Warnings) != 2 {
		t.Fatalf("want small-sample and ungraded warnings, got %v", c.Warnings)
	}
	if c.AccuracyA != nil || c.AccuracyDiff != nil {
		t.Error("accuracy must stay nil when nothing is graded, not 0")
	}
}
