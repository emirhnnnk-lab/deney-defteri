// Package abtest compares two arms of model calls. It keeps the statistics
// deliberately simple and says out loud when the sample is too small to trust.
package abtest

import "math"

// Arm is the aggregate of one side of the comparison.
type Arm struct {
	Label     string   `json:"label"`
	Calls     int      `json:"calls"`
	Successes int      `json:"successes"`
	Graded    int      `json:"graded"`  // calls with a correct/incorrect label
	Correct   int      `json:"correct"` // of Graded
	MeanSec   *float64 `json:"mean_sec"`
	MedianSec *float64 `json:"median_sec"`
}

// Rate returns k/n, or nil when n is zero (no data is not 0%).
func Rate(k, n int) *float64 {
	if n == 0 {
		return nil
	}
	r := float64(k) / float64(n)
	return &r
}

// Comparison is the result shown in the A/B view.
type Comparison struct {
	A              Arm      `json:"a"`
	B              Arm      `json:"b"`
	SuccessRateA   *float64 `json:"success_rate_a"`
	SuccessRateB   *float64 `json:"success_rate_b"`
	AccuracyA      *float64 `json:"accuracy_a"`
	AccuracyB      *float64 `json:"accuracy_b"`
	AccuracyDiff   *float64 `json:"accuracy_diff"` // B - A
	AccuracyPValue *float64 `json:"accuracy_p_value"`
	SpeedRatio     *float64 `json:"speed_ratio"` // mean A / mean B; >1 means B is faster
	Warnings       []string `json:"warnings"`
}

// MinPerArm is the sample size below which the result is labelled a direction, not evidence.
const MinPerArm = 30

// Compare builds the comparison for two arms.
func Compare(a, b Arm) Comparison {
	c := Comparison{
		A: a, B: b,
		SuccessRateA: Rate(a.Successes, a.Calls),
		SuccessRateB: Rate(b.Successes, b.Calls),
		AccuracyA:    Rate(a.Correct, a.Graded),
		AccuracyB:    Rate(b.Correct, b.Graded),
		Warnings:     []string{},
	}
	if c.AccuracyA != nil && c.AccuracyB != nil {
		d := *c.AccuracyB - *c.AccuracyA
		c.AccuracyDiff = &d
		c.AccuracyPValue = TwoProportionPValue(a.Correct, a.Graded, b.Correct, b.Graded)
	}
	if a.MeanSec != nil && b.MeanSec != nil && *b.MeanSec > 0 {
		r := *a.MeanSec / *b.MeanSec
		c.SpeedRatio = &r
	}
	if a.Calls < MinPerArm || b.Calls < MinPerArm {
		c.Warnings = append(c.Warnings, "fewer than 30 calls in an arm: treat the result as a direction, not evidence")
	}
	if a.Graded == 0 || b.Graded == 0 {
		c.Warnings = append(c.Warnings, "no graded calls in an arm: accuracy cannot be compared")
	}
	return c
}

// TwoProportionPValue is the two-sided p-value of a pooled two-proportion z-test.
// It returns nil when the test is undefined (empty arm, or both rates 0 or 1).
func TwoProportionPValue(k1, n1, k2, n2 int) *float64 {
	if n1 == 0 || n2 == 0 {
		return nil
	}
	p := float64(k1+k2) / float64(n1+n2)
	se := math.Sqrt(p * (1 - p) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		return nil
	}
	z := (float64(k2)/float64(n2) - float64(k1)/float64(n1)) / se
	pv := math.Erfc(math.Abs(z) / math.Sqrt2)
	return &pv
}
