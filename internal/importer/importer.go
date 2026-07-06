package importer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"durooma/internal/models"
)

// storeIface is the subset of *store.Store the importer needs.
type storeIface interface {
	CreateAccount(ctx context.Context, institutionName, accountName, currency string) (int64, error)
	CategoryByName(ctx context.Context, name string) (int64, bool, error)
	InsertTransactions(ctx context.Context, txns []models.Transaction) (int, error)
}

// fxIface converts an amount into the base currency on a given date.
type fxIface interface {
	Convert(ctx context.Context, date time.Time, amount float64, from, to string) (float64, error)
}

type Importer struct {
	store        storeIface
	fx           fxIface
	baseCurrency string
}

func New(store storeIface, fx fxIface, baseCurrency string) *Importer {
	return &Importer{store: store, fx: fx, baseCurrency: baseCurrency}
}

// Result summarises an import run for display.
type Result struct {
	Parsed     int
	Inserted   int
	Duplicates int
	Warnings   []string
}

// Import parses a CSV blob for the given provider ("UBS" or "Schwab"), converts
// to the base currency, and inserts new (deduplicated) transactions.
func (im *Importer) Import(ctx context.Context, provider, accountName string, data []byte) (Result, error) {
	rows, err := readCSV(data)
	if err != nil {
		return Result{}, fmt.Errorf("read csv: %w", err)
	}

	var parsed []ParsedTxn
	var institution string
	switch strings.ToLower(provider) {
	case "ubs":
		institution = "UBS"
		parsed, err = ParseUBS(rows, accountName)
	case "schwab":
		institution = "Charles Schwab"
		parsed, err = ParseSchwab(rows, accountName)
	default:
		return Result{}, fmt.Errorf("unknown provider %q", provider)
	}
	if err != nil {
		return Result{}, err
	}

	res := Result{Parsed: len(parsed)}
	if len(parsed) == 0 {
		return res, nil
	}

	accountCache := map[string]int64{}
	catCache := map[string]*int64{}
	var txns []models.Transaction

	for _, p := range parsed {
		acctKey := institution + "\x00" + p.Account
		accountID, ok := accountCache[acctKey]
		if !ok {
			accountID, err = im.store.CreateAccount(ctx, institution, p.Account, p.Currency)
			if err != nil {
				return res, fmt.Errorf("resolve account %q: %w", p.Account, err)
			}
			accountCache[acctKey] = accountID
		}

		baseAmount := p.Amount
		if p.Currency != im.baseCurrency {
			converted, cerr := im.fx.Convert(ctx, p.Date, p.Amount, p.Currency, im.baseCurrency)
			if cerr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"FX rate %s→%s on %s unavailable (kept original amount): %v",
					p.Currency, im.baseCurrency, p.Date.Format("2006-01-02"), cerr))
			} else {
				baseAmount = converted
			}
		}

		var categoryID *int64
		if p.Category != "" {
			if id, cok := catCache[p.Category]; cok {
				categoryID = id
			} else if id, found, cerr := im.store.CategoryByName(ctx, p.Category); cerr == nil && found {
				idCopy := id
				categoryID = &idCopy
				catCache[p.Category] = &idCopy
			} else {
				catCache[p.Category] = nil
			}
		}

		month := firstOfMonth(p.Date)
		txns = append(txns, models.Transaction{
			AccountID:    accountID,
			Date:         p.Date,
			Description:  p.Desc,
			Amount:       p.Amount,
			Currency:     p.Currency,
			BaseAmount:   baseAmount,
			BaseCurrency: im.baseCurrency,
			CategoryID:   categoryID,
			StartMonth:   month,
			EndMonth:     month,
			ExternalHash: GenerateHash(p.Date, p.Desc, p.Amount),
			Source:       institution,
		})
	}

	inserted, err := im.store.InsertTransactions(ctx, txns)
	if err != nil {
		return res, fmt.Errorf("insert: %w", err)
	}
	res.Inserted = inserted
	res.Duplicates = len(txns) - inserted
	return res, nil
}

// readCSV reads a CSV blob, sniffing the delimiter (comma vs semicolon) and
// tolerating a UTF-8 BOM and ragged rows (UBS exports have a metadata preamble).
func readCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	delim := ','
	if line, err := bufio.NewReader(bytes.NewReader(data)).ReadString('\n'); err == nil {
		if strings.Count(line, ";") > strings.Count(line, ",") {
			delim = ';'
		}
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = delim
	r.FieldsPerRecord = -1 // allow ragged rows
	r.LazyQuotes = true

	var rows [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip malformed lines rather than aborting the whole import.
			continue
		}
		rows = append(rows, rec)
	}
	return rows, nil
}
