package importer

import (
	"fmt"
	"regexp"
	"strings"
)

var headerNorm = regexp.MustCompile(`[^a-z0-9]`)

// normHeader lowercases a header and strips spaces/punctuation, mapping
// "&" -> "and" so that e.g. "Fees & Commissions" -> "feesandcommissions".
func normHeader(h string) string {
	s := strings.ToLower(strings.TrimSpace(h))
	s = strings.ReplaceAll(s, "&", "and")
	return headerNorm.ReplaceAllString(s, "")
}

// ParseSchwab parses a Charles Schwab Equity Awards CSV export. Ported from the
// original Apps Script parseSchwab: handles RSU vesting (Deposit/RS), sales
// (with per-lot sub-rows), dividends and tax withholding. Amounts are in USD.
func ParseSchwab(rows [][]string, account string) ([]ParsedTxn, error) {
	if account == "" && len(rows) > 0 {
		account = strings.TrimSpace(cell(rows[0], 0))
	}
	if account == "" || account == "Equity Awards" {
		account = "Schwab Equity"
	}

	header := -1
	for i := 0; i < len(rows) && i < 10; i++ {
		if strings.EqualFold(cell(rows[i], 0), "Date") {
			header = i
			break
		}
	}
	if header == -1 {
		return nil, fmt.Errorf(`invalid Schwab data: missing "Date" header row`)
	}

	idx := map[string]int{}
	for j, h := range rows[header] {
		idx[normHeader(h)] = j
	}
	col := func(name string) int {
		if v, ok := idx[name]; ok {
			return v
		}
		return -1
	}
	cDate := col("date")
	cAction := col("action")
	cDesc := col("description")
	cQty := col("quantity")
	cFees := col("feesandcommissions")
	cAmount := col("amount")
	cVestFMV := col("vestfairmarketvalue")
	cShares := col("shares")
	cSalePrice := col("saleprice")
	cSymbol := col("symbol")

	var out []ParsedTxn
	for i := header + 1; i < len(rows); i++ {
		r := rows[i]
		if cell(r, cDate) == "" {
			continue // sub-rows are consumed inline below
		}
		date, ok := parseDate(cell(r, cDate))
		if !ok {
			continue
		}
		action := cell(r, cAction)
		desc := cell(r, cDesc)
		symbol := cell(r, cSymbol)

		switch {
		case action == "Deposit" && desc == "RS":
			qty := parseNumber(cell(r, cQty))
			vestPrice := 0.0
			if i+1 < len(rows) {
				vestPrice = parseNumber(cell(rows[i+1], cVestFMV))
			}
			out = append(out, ParsedTxn{
				Date:     date,
				Desc:     fmt.Sprintf("Stock Grant: %s (%.3f @ %.2f)", symbol, qty, vestPrice),
				Amount:   qty * vestPrice,
				Currency: "USD",
				Category: "Stock Grant",
				Account:  account,
			})

		case action == "Sale":
			fees := parseNumber(cell(r, cFees))
			var totalGain, totalShares float64
			for j := i + 1; j < len(rows) && cell(rows[j], cDate) == ""; j++ {
				sub := rows[j]
				shares := parseNumber(cell(sub, cShares))
				salePrice := parseNumber(cell(sub, cSalePrice))
				fmv := parseNumber(cell(sub, cVestFMV))
				if shares > 0 {
					totalGain += (salePrice - fmv) * shares
					totalShares += shares
				}
			}
			out = append(out, ParsedTxn{
				Date:     date,
				Desc:     fmt.Sprintf("Stock Sale: %s (%.3f shares)", symbol, totalShares),
				Amount:   totalGain - fees,
				Currency: "USD",
				Category: "Investment-Sell",
				Account:  account,
			})

		case action == "Dividend":
			out = append(out, ParsedTxn{
				Date:     date,
				Desc:     "Dividend: " + symbol,
				Amount:   parseNumber(cell(r, cAmount)),
				Currency: "USD",
				Category: "Dividend",
				Account:  account,
			})

		case action == "Tax Withholding":
			out = append(out, ParsedTxn{
				Date:     date,
				Desc:     "Tax Withholding: " + symbol,
				Amount:   parseNumber(cell(r, cAmount)),
				Currency: "USD",
				Category: "Tax",
				Account:  account,
			})
		}
	}
	return out, nil
}
