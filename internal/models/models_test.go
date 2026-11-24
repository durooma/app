package models

import (
	"testing"
	"time"
)

func month(y int, m time.Month) time.Time {
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

func TestAllocatedFor(t *testing.T) {
	jan := month(2024, 1)
	jun := month(2024, 6)
	dec := month(2024, 12)

	tests := []struct {
		name             string
		start, end       time.Time // amortization window
		pStart, pEnd     time.Time // queried period
		base             float64
		want             float64
	}{
		{"single month, whole amount", jun, jun, jun, jun, -120, -120},
		{"single month, outside period", jun, jun, jan, jan, -120, 0},
		{"yearly spread, one month share", jan, dec, jun, jun, -1200, -100},
		{"yearly spread, full-year period", jan, dec, jan, dec, -1200, -1200},
		{"yearly spread, quarter overlap", jan, dec, jan, month(2024, 3), -1200, -300},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := Transaction{BaseAmount: tc.base, StartMonth: tc.start, EndMonth: tc.end}
			got := tx.AllocatedFor(tc.pStart, tc.pEnd)
			if got != tc.want {
				t.Errorf("AllocatedFor = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseAssignment(t *testing.T) {
	tests := []struct {
		in         string
		start, end time.Time
		ok         bool
	}{
		{"2026-03", month(2026, 3), month(2026, 3), true},
		{"2026-Q1", month(2026, 1), month(2026, 3), true},
		{"2026-q4", month(2026, 10), month(2026, 12), true},
		{"2026", month(2026, 1), month(2026, 12), true},
		{"2026-13", time.Time{}, time.Time{}, false},
		{"2026-Q5", time.Time{}, time.Time{}, false},
		{"2026-Q0", time.Time{}, time.Time{}, false},
		{"+026", time.Time{}, time.Time{}, false},
		{"", time.Time{}, time.Time{}, false},
		{"2026-03-01", time.Time{}, time.Time{}, false},
	}
	for _, tc := range tests {
		start, end, ok := ParseAssignment(tc.in)
		if ok != tc.ok || !start.Equal(tc.start) || !end.Equal(tc.end) {
			t.Errorf("ParseAssignment(%q) = %v, %v, %v; want %v, %v, %v", tc.in, start, end, ok, tc.start, tc.end, tc.ok)
		}
	}
}

func TestPeriodPresentation(t *testing.T) {
	tests := []struct {
		start, end         time.Time
		kind, value, label string
	}{
		{month(2026, 3), month(2026, 3), PeriodMonth, "2026-03", "Mar 2026"},
		{month(2026, 4), month(2026, 6), PeriodQuarter, "2026-Q2", "Q2 2026"},
		{month(2026, 1), month(2026, 12), PeriodYear, "2026", "2026"},
		// Arbitrary ranges saved before assignment was limited stay readable.
		{month(2026, 2), month(2026, 4), "", "", "Feb 2026 – Apr 2026"},
		{month(2026, 7), month(2027, 6), "", "", "Jul 2026 – Jun 2027"},
	}
	for _, tc := range tests {
		tx := Transaction{StartMonth: tc.start, EndMonth: tc.end}
		if got := tx.PeriodKind(); got != tc.kind {
			t.Errorf("%v–%v kind = %q, want %q", tc.start, tc.end, got, tc.kind)
		}
		if got := tx.PeriodValue(); got != tc.value {
			t.Errorf("%v–%v value = %q, want %q", tc.start, tc.end, got, tc.value)
		}
		if got := tx.PeriodLabel(); got != tc.label {
			t.Errorf("%v–%v label = %q, want %q", tc.start, tc.end, got, tc.label)
		}
		if tc.value != "" {
			s, e, ok := ParseAssignment(tc.value)
			if !ok || !s.Equal(tc.start) || !e.Equal(tc.end) {
				t.Errorf("round trip of %q = %v, %v, %v", tc.value, s, e, ok)
			}
		}
	}
	moved := Transaction{Date: time.Date(2026, 3, 14, 0, 0, 0, 0, time.UTC), StartMonth: month(2026, 3), EndMonth: month(2026, 3)}
	if !moved.InOwnMonth() {
		t.Error("a transaction in its own month should report InOwnMonth")
	}
	moved.StartMonth, moved.EndMonth = month(2026, 4), month(2026, 4)
	if moved.InOwnMonth() {
		t.Error("a transaction moved to another month should not report InOwnMonth")
	}
}
