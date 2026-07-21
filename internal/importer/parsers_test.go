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

func TestParseSchwabBrokerage(t *testing.T) {
	rows := [][]string{
		{"Date", "Action", "Symbol", "Description", "Quantity", "Price", "Fees & Comm", "Amount"},
		{"12/30/2025", "Credit Interest", "", "SCHWAB1 INT", "", "", "", "$2.67"},
		{"12/30/2025 as of 12/28/2025", "Visa Purchase", "", "MOUNTAIN SHOP", "", "", "", "-$42.40"},
		{"12/24/2025", "Cash Dividend", "VOO", "VANGUARD S&P 500 ETF", "", "", "", "$17.71"},
		{"12/24/2025", "NRA Tax Adj", "VOO", "VANGUARD S&P 500 ETF", "", "", "", "-$5.31"},
		{"01/29/2025", "Stock Plan Activity", "GOOG", "ALPHABET INC CLASS C", "21.073", "", "", ""},
		{"01/29/2025 as of 01/28/2025", "Sell", "GOOG", "ALPHABET INC CLASS C", "21.073", "$196.033", "$0.11", "$4130.89"},
	}

	got, err := ParseSchwab(rows, "Schwab Joint Tenant")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 transactions, got %d: %+v", len(got), got)
	}

	want := []struct {
		desc     string
		amount   float64
		category string
	}{
		{"Credit Interest: SCHWAB1 INT", 2.67, "Other Income"},
		{"Visa Purchase: MOUNTAIN SHOP", -42.40, ""},
		{"Cash Dividend: VOO - VANGUARD S&P 500 ETF", 17.71, "Dividend"},
		{"NRA Tax Adj: VOO - VANGUARD S&P 500 ETF", -5.31, "Tax"},
	}
	for i, w := range want {
		if got[i].Desc != w.desc || !approx(got[i].Amount, w.amount) || got[i].Category != w.category {
			t.Errorf("txn %d = %+v, want desc=%q amount=%.2f category=%q", i, got[i], w.desc, w.amount, w.category)
		}
		if got[i].Account != "Schwab Joint Tenant" || got[i].Currency != "USD" {
			t.Errorf("txn %d account/currency wrong: %+v", i, got[i])
		}
	}
}

func TestParseSchwabBrokerageKeepsOrdinarySale(t *testing.T) {
	rows := [][]string{
		{"Date", "Action", "Symbol", "Description", "Quantity", "Price", "Fees & Comm", "Amount"},
		{"01/29/2025", "Sell", "VOO", "VANGUARD S&P 500 ETF", "2", "$600.00", "$0.01", "$1199.99"},
	}

	got, err := ParseSchwab(rows, "Schwab Individual")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !approx(got[0].Amount, 1199.99) {
		t.Fatalf("ordinary brokerage sale should retain its signed amount: %+v", got)
	}
}

func TestParseSchwabBrokerageDefaultAccount(t *testing.T) {
	rows := [][]string{
		{"Date", "Action", "Symbol", "Description", "Quantity", "Price", "Fees & Comm", "Amount"},
		{"12/30/2025", "Credit Interest", "", "SCHWAB1 INT", "", "", "", "$2.67"},
	}

	got, err := ParseSchwab(rows, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Account != "Schwab Brokerage" {
		t.Fatalf("default brokerage account wrong: %+v", got)
	}
}

func TestParseSchwabEquitySaleLoss(t *testing.T) {
	rows := [][]string{
		{"Date", "Action", "Symbol", "Description", "Quantity", "FeesAndCommissions", "DisbursementElection", "Amount", "AwardDate", "AwardId", "VestDate", "VestFairMarketValue", "Type", "Shares", "SalePrice"},
		{"03/01/2025", "Sale", "ABC", "Share Sale", "5", "$1.00", "", "$399.00", "", "", "", "", "", "", ""},
		{"", "", "", "", "", "", "", "", "", "", "02/01/2025", "$100.00", "RS", "5", "$80.00"},
	}

	got, err := ParseSchwab(rows, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one sale transaction, got %d: %+v", len(got), got)
	}
	if got[0].Category != "Investment-Sell" || !approx(got[0].Amount, -101) {
		t.Errorf("stock sale loss wrong (want -101.0): %+v", got[0])
	}
	if got[0].Account != "Schwab Equity" {
		t.Errorf("default equity account = %q, want Schwab Equity", got[0].Account)
	}
}
