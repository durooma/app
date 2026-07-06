package importer

import (
	"math"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.001 }

func TestParseNumber(t *testing.T) {
	cases := map[string]float64{
		"1'234.56":  1234.56,
		"1.234,56":  1234.56,
		"1,234.56":  1234.56,
		"1234,56":   1234.56,
		"($5.00)":   -5.00,
		"-$3.20":    -3.20,
		"$12.34":    12.34,
		"":          0,
		"-":         0,
		"100":       100,
		"1'000'000": 1000000,
	}
	for in, want := range cases {
		if got := parseNumber(in); !approx(got, want) {
			t.Errorf("parseNumber(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseDate(t *testing.T) {
	cases := map[string]string{
		"31.12.2023":                  "2023-12-31",
		"01/15/2024":                  "2024-01-15",
		"2024-03-02":                  "2024-03-02",
		"03/01/2024 as of 02/28/2024": "2024-03-01",
	}
	for in, want := range cases {
		got, ok := parseDate(in)
		if !ok {
			t.Errorf("parseDate(%q) failed", in)
			continue
		}
		if got.Format("2006-01-02") != want {
			t.Errorf("parseDate(%q) = %s, want %s", in, got.Format("2006-01-02"), want)
		}
	}
}

func TestGenerateHashStable(t *testing.T) {
	d := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	a := GenerateHash(d, "Migros", -50.0)
	b := GenerateHash(d, "Migros", -50.0)
	c := GenerateHash(d, "Migros", -50.01)
	if a != b {
		t.Error("hash not stable for identical input")
	}
	if a == c {
		t.Error("hash collision for different amounts")
	}
}
