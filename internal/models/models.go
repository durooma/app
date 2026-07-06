package models

import "time"

type Institution struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

type Account struct {
	ID              int64
	InstitutionID   int64
	InstitutionName string // joined for display
	Name            string
	Currency        string
	CreatedAt       time.Time
}

type Category struct {
	ID          int64
	Name        string
	Description string
	Kind        string // "income", "expense", or ""
	SortOrder   int
}

type Rule struct {
	ID           int64
	Pattern      string
	CategoryID   int64
	CategoryName string // joined for display
	Priority     int
}

type Transaction struct {
	ID           int64
	AccountID    int64
	AccountName  string // joined
	Institution  string // joined
	Date         time.Time
	Description  string
	Amount       float64
	Currency     string
	BaseAmount   float64
	BaseCurrency string
	CategoryID   *int64
	CategoryName string // joined ("" if uncategorized)
	StartMonth   time.Time
	EndMonth     time.Time
	ExternalHash string
	Source       string
	Note         string
}

// MonthSpan returns the number of inclusive months the transaction is amortized
// over (1 for a normal single-month transaction).
func (t Transaction) MonthSpan() int {
	return monthsBetween(t.StartMonth, t.EndMonth)
}

// CatID returns the category id or 0 when uncategorized (convenient for
// templates comparing against option values).
func (t Transaction) CatID() int64 {
	if t.CategoryID == nil {
		return 0
	}
	return *t.CategoryID
}

// Amortized reports whether the transaction spans more than one month.
func (t Transaction) Amortized() bool { return t.MonthSpan() > 1 }

func monthsBetween(start, end time.Time) int {
	m := (end.Year()-start.Year())*12 + int(end.Month()) - int(start.Month()) + 1
	if m < 1 {
		return 1
	}
	return m
}

// CategoryTotal is a per-category aggregate used by the deep-dive reports.
type CategoryTotal struct {
	CategoryID   *int64
	CategoryName string
	Kind         string
	Amount       float64 // signed, in base currency
}

// MonthCategoryCell is one cell of the year matrix (a category's amount in a month).
type MonthCategoryCell struct {
	Month        int // 1-12
	CategoryID   *int64
	CategoryName string
	Amount       float64
}

// YearTotal aggregates a single year for the multi-year overview.
type YearTotal struct {
	Year    int
	Income  float64
	Expense float64
	Net     float64
}

// MonthTotal aggregates a single month for the year overview.
type MonthTotal struct {
	Month   int
	Income  float64
	Expense float64
	Net     float64
}
