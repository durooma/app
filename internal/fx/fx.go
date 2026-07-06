// Package fx converts amounts into the base currency, caching historical rates
// in the database. It replaces the GOOGLEFINANCE formula from the original
// Google Sheets script.
package fx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// rateStore is the subset of the store needed for caching rates.
type rateStore interface {
	CachedRate(ctx context.Context, date time.Time, base, quote string) (float64, bool, error)
	StoreRate(ctx context.Context, date time.Time, base, quote string, rate float64) error
}

type Converter struct {
	store   rateStore
	baseURL string
	client  *http.Client
}

func New(store rateStore, baseURL string) *Converter {
	return &Converter{
		store:   store,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Rate returns the conversion rate from -> to on the given date, using the
// cache first and the FX API as a fallback.
func (c *Converter) Rate(ctx context.Context, date time.Time, from, to string) (float64, error) {
	if from == to {
		return 1, nil
	}
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	if r, ok, err := c.store.CachedRate(ctx, day, from, to); err != nil {
		return 0, err
	} else if ok {
		return r, nil
	}

	rate, err := c.fetch(ctx, day, from, to)
	if err != nil {
		return 0, err
	}
	// Best-effort cache; ignore write errors.
	_ = c.store.StoreRate(ctx, day, from, to, rate)
	return rate, nil
}

// Convert converts an amount from one currency to another on a date.
func (c *Converter) Convert(ctx context.Context, date time.Time, amount float64, from, to string) (float64, error) {
	rate, err := c.Rate(ctx, date, from, to)
	if err != nil {
		return 0, err
	}
	return amount * rate, nil
}

type frankfurterResp struct {
	Rates map[string]float64 `json:"rates"`
}

func (c *Converter) fetch(ctx context.Context, date time.Time, from, to string) (float64, error) {
	url := fmt.Sprintf("%s/%s?from=%s&to=%s", c.baseURL, date.Format("2006-01-02"), from, to)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("fx api status %d for %s->%s on %s", resp.StatusCode, from, to, date.Format("2006-01-02"))
	}
	var fr frankfurterResp
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return 0, err
	}
	rate, ok := fr.Rates[to]
	if !ok || rate == 0 {
		return 0, fmt.Errorf("no rate for %s in fx response", to)
	}
	return rate, nil
}
