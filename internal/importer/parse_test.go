package importer

import (
	"math"
	"strings"
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

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name string
		csv  string
		want string
	}{
		{
			name: "UBS semicolon export with preamble",
			csv: "Account statement\n" +
				"Trade date;Trade time;Booking date;Value date;Ccy;Debit;Credit;Sub;Bal;X;Description1;Description2\n",
			want: "UBS",
		},
		{
			name: "Schwab brokerage export",
			csv:  "Date,Action,Symbol,Description,Quantity,Price,Fees & Comm,Amount\n",
			want: "Schwab",
		},
		{
			name: "Schwab equity export with preamble",
			csv: "Equity Awards\n" +
				"Date,Action,Symbol,Description,Amount,Vest Fair Market Value\n",
			want: "Schwab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DetectProvider([]byte(tt.csv))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("DetectProvider() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A header row longer than bufio.Scanner's 64 KB token cap must still sniff as
// semicolon-delimited rather than silently falling back to comma.
func TestSniffDelimiterHandlesVeryLongLines(t *testing.T) {
	padding := strings.Repeat("Description;", 8000) // ~96 KB
	csv := "Account statement\n" +
		"Trade date;Trade time;" + padding + "Debit;Credit\n"

	if got := sniffDelimiter([]byte(csv)); got != ';' {
		t.Fatalf("sniffDelimiter() = %q, want ';'", got)
	}
	if got, err := DetectProvider([]byte(csv)); err != nil || got != "UBS" {
		t.Fatalf("DetectProvider() = %q, %v; want UBS", got, err)
	}
}

// The final line counts even without a trailing newline.
func TestSniffDelimiterUnterminatedLastLine(t *testing.T) {
	if got := sniffDelimiter([]byte("Trade date;Debit;Credit")); got != ';' {
		t.Fatalf("sniffDelimiter() = %q, want ';'", got)
	}
}

func TestDetectProviderRejectsGenericCSV(t *testing.T) {
	_, err := DetectProvider([]byte("Date,Description,Amount\n2026-01-01,Lunch,-20\n"))
	if err == nil {
		t.Fatal("expected unsupported CSV error")
	}
	if !strings.Contains(err.Error(), "unsupported CSV structure") {
		t.Fatalf("unexpected error: %v", err)
	}
}
