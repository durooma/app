package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"durooma/internal/models"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestPeriodWindow(t *testing.T) {
	aug := day(2026, time.August, 25)
	tests := []struct {
		label      string
		start, end time.Time
	}{
		{"month", day(2026, 8, 1), day(2026, 8, 1)},
		{"quarter", day(2026, 7, 1), day(2026, 9, 1)},
		{"year", day(2026, 1, 1), day(2026, 12, 1)},
		{"previous_month", day(2026, 7, 1), day(2026, 7, 1)},
		{"previous_quarter", day(2026, 4, 1), day(2026, 6, 1)},
		{"previous_year", day(2025, 1, 1), day(2025, 12, 1)},
		{"next_month", day(2026, 9, 1), day(2026, 9, 1)},
		{"next_quarter", day(2026, 10, 1), day(2026, 12, 1)},
		{" Next_Year ", day(2027, 1, 1), day(2027, 12, 1)},
	}
	for _, tc := range tests {
		start, end, ok := PeriodWindow(aug, tc.label)
		if !ok || !start.Equal(tc.start) || !end.Equal(tc.end) {
			t.Errorf("PeriodWindow(%q) = %v, %v, %v; want %v, %v", tc.label, start, end, ok, tc.start, tc.end)
		}
	}
	// Crossing a year boundary in both directions.
	if s, _, _ := PeriodWindow(day(2026, 12, 20), "next_month"); !s.Equal(day(2027, 1, 1)) {
		t.Errorf("next_month from December = %v", s)
	}
	if s, e, _ := PeriodWindow(day(2026, 2, 3), "previous_quarter"); !s.Equal(day(2025, 10, 1)) || !e.Equal(day(2025, 12, 1)) {
		t.Errorf("previous_quarter from February = %v–%v", s, e)
	}
	for _, bad := range []string{"", "week", "next_decade", "2026-Q1"} {
		if _, _, ok := PeriodWindow(aug, bad); ok {
			t.Errorf("PeriodWindow(%q) accepted", bad)
		}
	}
}

func TestRenderPeriodPrompt(t *testing.T) {
	p := RenderPeriodPrompt([]PeriodItem{
		{Date: day(2026, 9, 2), Desc: "Allianz Jahresprämie", Amount: -1240, Category: "Insurance"},
		{Date: day(2026, 9, 14), Desc: "Migros", Amount: -84.35},
	})
	for _, want := range []string{
		`[ID: 0] Date: 2026-09-02  Transaction: "Allianz Jahresprämie"  Amount: -1240.00  Category: "Insurance"`,
		`[ID: 1] Date: 2026-09-14  Transaction: "Migros"  Amount: -84.35` + "\n",
		`"next_year"`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "{{") {
		t.Errorf("unrendered placeholder in prompt:\n%s", p)
	}
}

// periodStoreFake keeps a fixed set of pending transactions and records both
// halves of the work, like the real store's gap-filling writes.
type periodStoreFake struct {
	fakeStore
	txns    map[int64]models.Transaction
	periods map[int64][2]time.Time
}

func (f *periodStoreFake) PendingTransactions(_ context.Context, ids []int64) ([]models.Transaction, error) {
	var out []models.Transaction
	for _, id := range ids {
		if t, ok := f.txns[id]; ok && (t.CategoryID == nil || t.PeriodSource == "") {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *periodStoreFake) SetUndecidedPeriod(_ context.Context, id int64, start, end time.Time) error {
	f.periods[id] = [2]time.Time{start, end}
	return nil
}

// periodFakeProvider answers categories with one name and periods from a map
// keyed by description, recording the items it was asked about.
type periodFakeProvider struct {
	fakeProvider
	labels map[string]string
	asked  []PeriodItem
}

func (p *periodFakeProvider) ClassifyPeriods(_ context.Context, items []PeriodItem) ([]string, error) {
	p.asked = append(p.asked, items...)
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = p.labels[it.Desc]
	}
	return out, nil
}

func TestCategorizeAssignsPeriods(t *testing.T) {
	insurance := int64(4)
	st := &periodStoreFake{
		fakeStore: fakeStore{cats: []models.Category{{ID: 3, Name: "Groceries"}, {ID: 4, Name: "Insurance"}}},
		txns: map[int64]models.Transaction{
			// Needs a category and a period.
			1: {ID: 1, Date: day(2026, 9, 14), Description: "Migros"},
			// Categorized by hand, period still undecided.
			2: {ID: 2, Date: day(2026, 9, 2), Description: "Allianz", CategoryID: &insurance, CategoryName: "Insurance"},
			// Period picked by hand: never sent for a period.
			3: {ID: 3, Date: day(2026, 9, 3), Description: "Serafe", PeriodSource: "user"},
			// An answer outside the vocabulary.
			4: {ID: 4, Date: day(2026, 9, 5), Description: "Mystery", PeriodSource: ""},
		},
		periods: map[int64][2]time.Time{},
	}
	pr := &periodFakeProvider{fakeProvider: fakeProvider{answer: "Groceries"},
		labels: map[string]string{"Migros": "month", "Allianz": "year", "Mystery": "fortnight"}}
	svc := NewService(st, pr)

	var last Progress
	rep, err := svc.CategorizeWithProgress(context.Background(),
		[]models.Transaction{st.txns[1], st.txns[2], st.txns[3], st.txns[4]},
		func(p Progress) { last = p })
	if err != nil {
		t.Fatal(err)
	}

	if rep.Total != 3 || rep.ByAI != 3 {
		t.Errorf("category report = %+v, want 3 categorized by AI", rep)
	}
	if rep.Periods != 3 || rep.PeriodsAssigned != 3 || rep.PeriodsMoved != 1 {
		t.Errorf("period report = %+v, want 3 decided, 1 moved", rep)
	}
	if last.Done != 6 || last.Total != 6 {
		t.Errorf("last progress = %+v, want both passes counted", last)
	}
	if _, asked := st.periods[3]; asked {
		t.Error("a hand-picked period was overwritten")
	}
	if got := st.periods[2]; !got[0].Equal(day(2026, 1, 1)) || !got[1].Equal(day(2026, 12, 1)) {
		t.Errorf("yearly premium window = %v", got)
	}
	if got := st.periods[4]; !got[0].Equal(day(2026, 9, 1)) || !got[1].Equal(day(2026, 9, 1)) {
		t.Errorf("unknown label should fall back to the own month, got %v", got)
	}
	// The period pass sees categories, including the one assigned moments ago.
	byDesc := map[string]string{}
	for _, it := range pr.asked {
		byDesc[it.Desc] = it.Category
	}
	if byDesc["Migros"] != "Groceries" || byDesc["Allianz"] != "Insurance" {
		t.Errorf("period items carried categories %v", byDesc)
	}
}
