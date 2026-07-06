package importer

import "testing"

func TestParseUBS(t *testing.T) {
	rows := [][]string{
		{"UBS Statement", "", "", "", "", "", "", "", "", "", "", ""},
		{"Trade date", "Trade time", "Booking date", "Value date", "Ccy", "Debit", "Credit", "", "", "", "Desc1", "Desc2"},
		{"31.12.2023", "10:00", "31.12.2023", "31.12.2023", "CHF", "-100.50", "", "", "", "", "Migros", "Zurich"},
		{"02.01.2024", "09:00", "02.01.2024", "02.01.2024", "CHF", "", "2500.00", "", "", "", "Salary", "ACME"},
		{"", "", "", "", "", "", "", "", "", "", "", ""}, // trailing blank
	}
	got, err := ParseUBS(rows, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(got))
	}
	if got[0].Desc != "Migros Zurich" || !approx(got[0].Amount, -100.50) || got[0].Currency != "CHF" {
		t.Errorf("txn0 wrong: %+v", got[0])
	}
	if !approx(got[1].Amount, 2500.00) {
		t.Errorf("txn1 amount wrong: %+v", got[1])
	}
	if got[0].Account != "UBS Main" {
		t.Errorf("default account = %q", got[0].Account)
	}
}

func TestParseSchwab(t *testing.T) {
	// Columns: Date, Action, Symbol, Description, Quantity,
	//          Fees & Commissions, Amount, Shares, Sale Price, Vest Fair Market Value
	rows := [][]string{
		{"Equity Awards"},
		{"Date", "Action", "Symbol", "Description", "Quantity", "Fees & Commissions", "Amount", "Shares", "Sale Price", "Vest Fair Market Value"},
		{"01/15/2024", "Dividend", "ABC", "", "", "", "$12.34", "", "", ""},
		{"02/01/2024", "Deposit", "ABC", "RS", "10", "", "", "", "", ""},
		{"", "", "", "", "", "", "", "", "", "$100.00"}, // vest sub-row
		{"03/01/2024", "Sale", "ABC", "", "", "$1.00", "", "", "", ""},
		{"", "", "", "", "", "", "", "5", "$120.00", "$100.00"}, // sale lot sub-row
	}
	got, err := ParseSchwab(rows, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 transactions, got %d: %+v", len(got), got)
	}
	if got[0].Category != "Dividend" || !approx(got[0].Amount, 12.34) {
		t.Errorf("dividend wrong: %+v", got[0])
	}
	if got[1].Category != "Stock Grant" || !approx(got[1].Amount, 1000.0) {
		t.Errorf("stock grant wrong: %+v", got[1])
	}
	if got[2].Category != "Investment-Sell" || !approx(got[2].Amount, 99.0) {
		t.Errorf("stock sale wrong (want 99.0): %+v", got[2])
	}
	if got[0].Account != "Schwab Equity" || got[0].Currency != "USD" {
		t.Errorf("account/currency wrong: %+v", got[0])
	}
}
