package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"durooma/internal/models"
)

// TxnFilter describes the unified transaction view filters. Zero values mean
// "no filter".
type TxnFilter struct {
	AccountID     int64
	InstitutionID int64
	CategoryID    int64
	Uncategorized bool
	From          time.Time
	To            time.Time
	// PeriodStart/PeriodEnd select transactions whose amortization window overlaps
	// the given inclusive month range (both first-of-month). This matches how the
	// reports allocate amounts across months, so drilling into a report figure
	// yields exactly the transactions that produced it.
	PeriodStart time.Time
	PeriodEnd   time.Time
	// Sign restricts to income ("income": base_amount > 0) or expenses
	// ("expense": base_amount < 0). Empty means both.
	Sign   string
	Search string
	Limit  int
	Offset int
}

const txnSelect = `
	SELECT t.id, t.account_id, a.name, i.name, t.txn_date, t.description,
	       t.amount, t.currency, t.base_amount, t.base_currency,
	       t.category_id, COALESCE(c.name, ''), t.start_month, t.end_month,
	       t.external_hash, t.source, t.note
	FROM transactions t
	JOIN accounts a ON a.id = t.account_id
	JOIN institutions i ON i.id = a.institution_id
	LEFT JOIN categories c ON c.id = t.category_id`

func scanTxn(rows interface {
	Scan(...any) error
}) (models.Transaction, error) {
	var t models.Transaction
	err := rows.Scan(&t.ID, &t.AccountID, &t.AccountName, &t.Institution, &t.Date,
		&t.Description, &t.Amount, &t.Currency, &t.BaseAmount, &t.BaseCurrency,
		&t.CategoryID, &t.CategoryName, &t.StartMonth, &t.EndMonth,
		&t.ExternalHash, &t.Source, &t.Note)
	return t, err
}

func (s *Store) ListTransactions(ctx context.Context, f TxnFilter) ([]models.Transaction, error) {
	var where []string
	var args []any
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.AccountID != 0 {
		add("t.account_id = $%d", f.AccountID)
	}
	if f.InstitutionID != 0 {
		add("a.institution_id = $%d", f.InstitutionID)
	}
	if f.CategoryID != 0 {
		add("t.category_id = $%d", f.CategoryID)
	}
	if f.Uncategorized {
		where = append(where, "t.category_id IS NULL")
	}
	if !f.From.IsZero() {
		add("t.txn_date >= $%d", f.From)
	}
	if !f.To.IsZero() {
		add("t.txn_date <= $%d", f.To)
	}
	if !f.PeriodStart.IsZero() && !f.PeriodEnd.IsZero() {
		add("t.start_month <= $%d", f.PeriodEnd)
		add("t.end_month >= $%d", f.PeriodStart)
	}
	switch f.Sign {
	case "income":
		where = append(where, "t.base_amount > 0")
	case "expense":
		where = append(where, "t.base_amount < 0")
	}
	if strings.TrimSpace(f.Search) != "" {
		add("t.description ILIKE '%%' || $%d || '%%'", strings.TrimSpace(f.Search))
	}

	q := txnSelect
	if len(where) > 0 {
		q += "\nWHERE " + strings.Join(where, " AND ")
	}
	q += "\nORDER BY t.txn_date DESC, t.id DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += fmt.Sprintf("\nLIMIT $%d", len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += fmt.Sprintf("\nOFFSET $%d", len(args))
	}

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Transaction
	for rows.Next() {
		t, err := scanTxn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTransaction(ctx context.Context, id int64) (models.Transaction, error) {
	row := s.pool.QueryRow(ctx, txnSelect+"\nWHERE t.id = $1", id)
	return scanTxn(row)
}

// ExistingHashes returns the subset of the given hashes that already exist,
// enabling import deduplication without loading the whole table.
func (s *Store) ExistingHashes(ctx context.Context, hashes []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(hashes) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT external_hash FROM transactions WHERE external_hash = ANY($1)`, hashes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out[h] = true
	}
	return out, rows.Err()
}

// InsertTransactions bulk-inserts new transactions in one transaction, skipping
// any that collide on external_hash within the same account. Returns the number
// actually inserted.
func (s *Store) InsertTransactions(ctx context.Context, txns []models.Transaction) (int, error) {
	if len(txns) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	inserted := 0
	for _, t := range txns {
		ct, err := tx.Exec(ctx, `
			INSERT INTO transactions
			  (account_id, txn_date, description, amount, currency, base_amount,
			   base_currency, category_id, start_month, end_month, external_hash, source, note)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (account_id, external_hash) DO NOTHING`,
			t.AccountID, t.Date, t.Description, t.Amount, t.Currency, t.BaseAmount,
			t.BaseCurrency, t.CategoryID, t.StartMonth, t.EndMonth, t.ExternalHash, t.Source, t.Note)
		if err != nil {
			return 0, err
		}
		inserted += int(ct.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return inserted, nil
}

// UpdateTransactionCategory sets (or clears) a transaction's category.
func (s *Store) UpdateTransactionCategory(ctx context.Context, id int64, categoryID *int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE transactions SET category_id = $2 WHERE id = $1`, id, categoryID)
	return err
}

// UpdateTransactionMonths sets the amortization window for a transaction.
func (s *Store) UpdateTransactionMonths(ctx context.Context, id int64, start, end time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE transactions SET start_month = $2, end_month = $3 WHERE id = $1`,
		id, firstOfMonth(start), firstOfMonth(end))
	return err
}

func (s *Store) UpdateTransactionNote(ctx context.Context, id int64, note string) error {
	_, err := s.pool.Exec(ctx, `UPDATE transactions SET note = $2 WHERE id = $1`, id, note)
	return err
}

func (s *Store) DeleteTransaction(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM transactions WHERE id = $1`, id)
	return err
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}
