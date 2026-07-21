package importer

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var headerNorm = regexp.MustCompile(`[^a-z0-9]`)

// normHeader lowercases a header and strips spaces/punctuation, mapping
// "&" -> "and" so that e.g. "Fees & Commissions" -> "feesandcommissions".
func normHeader(h string) string {
	s := strings.ToLower(strings.TrimSpace(h))
	s = strings.ReplaceAll(s, "&", "and")
	return headerNorm.ReplaceAllString(s, "")
}

// ParseSchwab parses Charles Schwab brokerage and Equity Awards CSV exports.
// Equity Awards rows receive specialized RSU vesting and per-lot sale gain/loss
// handling; ordinary brokerage rows retain Schwab's signed cash amount. Amounts
// are in USD.
func ParseSchwab(rows [][]string, account string) ([]ParsedTxn, error) {
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
	isEquityAwards := cVestFMV >= 0

	if account == "" && header > 0 {
		account = strings.TrimSpace(cell(rows[0], 0))
	}
	if isEquityAwards && (account == "" || account == "Equity Awards") {
		account = "Schwab Equity"
	} else if account == "" {
		account = "Schwab Brokerage"
	}

	if !isEquityAwards {
		return parseSchwabBrokerage(rows[header+1:], account, cDate, cAction, cSymbol, cDesc, cQty, cAmount), nil
	}

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

func parseSchwabBrokerage(rows [][]string, account string, cDate, cAction, cSymbol, cDesc, cQty, cAmount int) []ParsedTxn {
	var out []ParsedTxn
	for i, r := range rows {
		date, ok := parseDate(cell(r, cDate))
		if !ok || cell(r, cAmount) == "" {
			continue
		}

		action := cell(r, cAction)
		if schwabStockPlanSale(rows, i, date, action, cDate, cAction, cSymbol, cQty) {
			continue
		}
		out = append(out, ParsedTxn{
			Date:     date,
			Desc:     schwabBrokerageDescription(action, cell(r, cSymbol), cell(r, cDesc)),
			Amount:   parseNumber(cell(r, cAmount)),
			Currency: "USD",
			Category: schwabBrokerageCategory(action),
			Account:  account,
		})
	}
	return out
}

// schwabStockPlanSale identifies a compact export's informational stock-plan
// activity followed by its matching cash-proceeds row. The compact export has
// no vest value or cost basis, so importing the proceeds as income would violate
// the gain/loss-only treatment provided by the Equity Awards export.
func schwabStockPlanSale(rows [][]string, i int, date time.Time, action string, cDate, cAction, cSymbol, cQty int) bool {
	if i == 0 || !strings.EqualFold(action, "Sell") {
		return false
	}
	previous := rows[i-1]
	if !strings.EqualFold(cell(previous, cAction), "Stock Plan Activity") ||
		!strings.EqualFold(cell(previous, cSymbol), cell(rows[i], cSymbol)) {
		return false
	}
	previousDate, ok := parseDate(cell(previous, cDate))
	previousQty := parseNumber(cell(previous, cQty))
	return ok && previousDate.Equal(date) && previousQty > 0 && previousQty == parseNumber(cell(rows[i], cQty))
}

func schwabBrokerageDescription(action, symbol, description string) string {
	detail := description
	if symbol != "" {
		detail = symbol
		if description != "" {
			detail += " - " + description
		}
	}
	if action == "" {
		return detail
	}
	if detail == "" {
		return action
	}
	return action + ": " + detail
}

func schwabBrokerageCategory(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "cash dividend", "dividend":
		return "Dividend"
	case "nra tax adj", "tax withholding":
		return "Tax"
	case "credit interest":
		return "Other Income"
	default:
		return ""
	}
}
