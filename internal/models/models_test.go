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
