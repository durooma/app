package importer

import (
	"fmt"
	"strings"
)

// ParseUBS parses a UBS Switzerland CSV export. Ported from the original
// Apps Script parseUBS: it locates the "Trade date" header row, then reads
// signed amounts from the Debit/Credit columns. UBS statements are in CHF.
func ParseUBS(rows [][]string, account string) ([]ParsedTxn, error) {
	if account == "" {
		account = "UBS Main"
	}

	header := -1
	for i, r := range rows {
		if strings.EqualFold(cell(r, 0), "Trade date") {
			header = i
			break
		}
	}
	if header == -1 {
		return nil, fmt.Errorf(`could not find "Trade date" header row — is this a UBS export?`)
	}

	var out []ParsedTxn
	for i := header + 1; i < len(rows); i++ {
		r := rows[i]
		if cell(r, 0) == "" {
			continue
		}
		date, ok := parseDate(cell(r, 0))
		if !ok {
			continue
		}
		desc := strings.TrimSpace(cell(r, 10) + " " + cell(r, 11))
		debit := parseNumber(cell(r, 5))  // UBS stores debits as negative
		credit := parseNumber(cell(r, 6)) // credits positive
		amount := debit + credit
		if amount == 0 && desc == "" {
			continue
		}
		out = append(out, ParsedTxn{
			Date:     date,
			Desc:     desc,
			Amount:   amount,
			Currency: "CHF",
			Account:  account,
		})
	}
	return out, nil
}
