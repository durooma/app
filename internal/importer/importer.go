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
// to the base currency, and inserts new (deduplicated) transactions. New callers
// should generally use ImportAuto so the provider is inferred from the CSV.
func (im *Importer) Import(ctx context.Context, provider, accountName string, data []byte) (Result, error) {
	rows, err := readCSV(data)
	if err != nil {
		return Result{}, fmt.Errorf("read csv: %w", err)
	}
	return im.importRows(ctx, provider, accountName, rows)
}

// ImportAuto infers the provider from the CSV's header structure and imports it.
func (im *Importer) ImportAuto(ctx context.Context, accountName string, data []byte) (Result, error) {
	rows, err := readCSV(data)
	if err != nil {
		return Result{}, fmt.Errorf("read csv: %w", err)
	}
	provider, err := detectProvider(rows)
	if err != nil {
		return Result{}, err
	}
	return im.importRows(ctx, provider, accountName, rows)
}

// DetectProvider returns the importer name matching the CSV's header structure.
func DetectProvider(data []byte) (string, error) {
	rows, err := readCSV(data)
	if err != nil {
		return "", fmt.Errorf("read csv: %w", err)
	}
	return detectProvider(rows)
}

func detectProvider(rows [][]string) (string, error) {
	for _, row := range rows {
		if normHeader(cell(row, 0)) == "tradedate" && rowHasHeaders(row, "debit", "credit") {
			return "UBS", nil
		}
	}

	for i := 0; i < len(rows) && i < 10; i++ {
		row := rows[i]
		if normHeader(cell(row, 0)) == "date" && rowHasHeaders(row, "action", "amount") {
			return "Schwab", nil
		}
	}

	return "", fmt.Errorf("unsupported CSV structure: expected a UBS or Charles Schwab export")
}

func rowHasHeaders(row []string, required ...string) bool {
	found := make(map[string]bool, len(row))
	for _, value := range row {
		found[normHeader(value)] = true
	}
	for _, header := range required {
		if !found[header] {
			return false
		}
	}
	return true
}

func (im *Importer) importRows(ctx context.Context, provider, accountName string, rows [][]string) (Result, error) {
	var parsed []ParsedTxn
	var institution string
	var err error
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

	delim := sniffDelimiter(data)

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

// sniffDelimiter examines several lines because exports may put an
// undelimited account name or report title before the actual header row.
// It reads with bufio.Reader rather than bufio.Scanner so that a header line
// longer than the scanner's 64 KB token cap cannot silently sniff as a comma.
func sniffDelimiter(data []byte) rune {
	maxCommas, maxSemicolons := 0, 0
	r := bufio.NewReader(bytes.NewReader(data))
	for lines := 0; lines < 10; lines++ {
		line, err := r.ReadString('\n')
		maxCommas = max(maxCommas, strings.Count(line, ","))
		maxSemicolons = max(maxSemicolons, strings.Count(line, ";"))
		if err != nil { // trailing line without a newline, or EOF
			break
		}
	}
	if maxSemicolons > maxCommas {
		return ';'
	}
	return ','
}
