package importer

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParsedTxn is a provider-agnostic transaction produced by a parser, before FX
// conversion and persistence.
type ParsedTxn struct {
	Date     time.Time
	Desc     string
	Amount   float64
	Currency string
	Category string // optional suggested category (e.g. "Dividend")
	Account  string
}

// GenerateHash builds a stable dedup key from date, description and amount —
// the port of the original script's generateID.
func GenerateHash(date time.Time, desc string, amount float64) string {
	key := fmt.Sprintf("%d-%d-%d%s%s",
		date.Year(), int(date.Month()), date.Day(),
		strings.TrimSpace(desc),
		strconv.FormatFloat(amount, 'f', -1, 64))
	sum := sha256.Sum256([]byte(key))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

var nonNumeric = regexp.MustCompile(`[^0-9.,\-]`)

// parseNumber parses money/quantity values across US and Swiss formats:
// "1'234.56", "1.234,56", "1,234.56", "1234,56", "($5.00)", "-$3.20".
func parseNumber(raw string) float64 {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0
	}
	negative := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		negative = true
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, "'", "") // Swiss thousands separator
	s = strings.ReplaceAll(s, " ", "")
	s = nonNumeric.ReplaceAllString(s, "")
	if s == "" || s == "-" {
		return 0
	}

	lastComma := strings.LastIndex(s, ",")
	lastDot := strings.LastIndex(s, ".")
	switch {
	case lastComma >= 0 && lastDot >= 0:
		// Whichever separator appears last is the decimal separator.
		if lastComma > lastDot {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.Replace(s, ",", ".", 1)
		} else {
			s = strings.ReplaceAll(s, ",", "")
		}
	case lastComma >= 0:
		// Only commas: treat as decimal separator (European) unless it groups.
		if len(s)-lastComma-1 == 3 && strings.Count(s, ",") == 1 && !strings.Contains(s, "-") {
			// Ambiguous "1,234" — treat as thousands.
			s = strings.ReplaceAll(s, ",", "")
		} else {
			s = strings.Replace(s, ",", ".", 1)
			s = strings.ReplaceAll(s, ",", "")
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if negative {
		v = -v
	}
	return v
}

var dateLayouts = []string{
	"02.01.2006", // Swiss DD.MM.YYYY (UBS)
	"01/02/2006", // US MM/DD/YYYY (Schwab)
	"1/2/2006",
	"2006-01-02",
	"02-01-2006",
	"02.01.06",
}

// parseDate parses a date string, tolerating Schwab's "MM/DD/YYYY as of ..." form.
func parseDate(raw string) (time.Time, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, false
	}
	if i := strings.Index(strings.ToLower(s), " as of "); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func cell(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}
