package models

import (
	"fmt"
	"strconv"
	"time"
)

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

// AllocatedFor returns the portion of BaseAmount that falls within the inclusive
// [start,end] month window — the per-period share used by the amortizing reports.
// For a single-month transaction (or a period covering its whole window) this is
// the full BaseAmount; for a transaction spread across N months it is
// BaseAmount/N times the number of its months inside the period.
func (t Transaction) AllocatedFor(start, end time.Time) float64 {
	o := overlapMonths(t.StartMonth, t.EndMonth, start, end)
	if o == 0 {
		return 0
	}
	return t.BaseAmount / float64(t.MonthSpan()) * float64(o)
}

func monthsBetween(start, end time.Time) int {
	m := (end.Year()-start.Year())*12 + int(end.Month()) - int(start.Month()) + 1
	if m < 1 {
		return 1
	}
	return m
}

// monthIndex maps a date to a monotonic month number for range arithmetic.
func monthIndex(t time.Time) int { return t.Year()*12 + int(t.Month()) - 1 }

// overlapMonths returns the count of inclusive months shared by the two month
// windows [aStart,aEnd] and [bStart,bEnd] (0 if they don't overlap).
func overlapMonths(aStart, aEnd, bStart, bEnd time.Time) int {
	lo := monthIndex(aStart)
	if s := monthIndex(bStart); s > lo {
		lo = s
	}
	hi := monthIndex(aEnd)
	if e := monthIndex(bEnd); e < hi {
		hi = e
	}
	if hi < lo {
		return 0
	}
	return hi - lo + 1
}

// CategoryTotal is a per-category aggregate used by the report breakdown. The
// two sides are summed separately rather than netted, so a category holding both
// (an expense with a refund, a mixed uncategorized bucket) is counted in full on
// each side and the breakdown reconciles with the period's totals.
type CategoryTotal struct {
	CategoryID   *int64
	CategoryName string
	Kind         string
	Income       float64 // sum of the positive allocations, in base currency
	Expense      float64 // sum of the negative allocations (negative or zero)
	Net          float64
}

// Period is a reporting scope: all time (Year 0), one whole year (Month 0) or a
// single month. The report view drills from all time into a year into a month.
type Period struct {
	Year  int
	Month int // 1-12
}

func (p Period) IsAll() bool   { return p.Year == 0 }
func (p Period) IsYear() bool  { return p.Year != 0 && p.Month == 0 }
func (p Period) IsMonth() bool { return p.Year != 0 && p.Month != 0 }

// Parent returns the scope one level up (a month's year, a year's all time).
func (p Period) Parent() Period {
	if p.IsMonth() {
		return Period{Year: p.Year}
	}
	return Period{}
}

// Param renders the period the way the transactions view's ?period= filter
// expects it: "" for all time, "2024" for a year, "2024-06" for a month.
func (p Period) Param() string {
	switch {
	case p.IsMonth():
		return fmt.Sprintf("%d-%02d", p.Year, p.Month)
	case p.IsYear():
		return strconv.Itoa(p.Year)
	}
	return ""
}

// Label names the period in full ("All time", "2024", "Jun 2024").
func (p Period) Label() string {
	switch {
	case p.IsMonth():
		return fmt.Sprintf("%s %d", MonthName(p.Month), p.Year)
	case p.IsYear():
		return strconv.Itoa(p.Year)
	}
	return "All time"
}

// ShortLabel names the period within its parent ("Jun" rather than "Jun 2024"),
// for use in the sub-period list where the enclosing scope is already known.
func (p Period) ShortLabel() string {
	if p.IsMonth() {
		return MonthName(p.Month)
	}
	return p.Label()
}

// Bounds returns the inclusive first-of-month range the period covers. All time
// yields zero times, meaning "unbounded".
func (p Period) Bounds() (start, end time.Time) {
	switch {
	case p.IsMonth():
		s := time.Date(p.Year, time.Month(p.Month), 1, 0, 0, 0, 0, time.UTC)
		return s, s
	case p.IsYear():
		return time.Date(p.Year, time.January, 1, 0, 0, 0, 0, time.UTC),
			time.Date(p.Year, time.December, 1, 0, 0, 0, 0, time.UTC)
	}
	return
}

// PeriodTotal aggregates one period: the report scope itself, or one of the
// sub-periods listed inside it (a year when looking at all time, a month when
// looking at a year).
type PeriodTotal struct {
	Period  Period
	Income  float64
	Expense float64
	Net     float64
}

// PeriodCategoryCell is one cell of the breakdown grid: a category's amounts
// within one sub-period, split by sign like CategoryTotal.
type PeriodCategoryCell struct {
	Period       Period
	CategoryID   *int64
	CategoryName string
	Income       float64
	Expense      float64
}

var monthNames = [...]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun",
	"Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// MonthName returns the abbreviated name of month 1-12 ("" for anything else).
func MonthName(m int) string {
	if m >= 1 && m <= 12 {
		return monthNames[m]
	}
	return ""
}
