// Package ai provides provider-agnostic transaction categorization. A rules
// pass runs first (cheap, deterministic); anything left is delegated to a
// pluggable LLM Provider. The app uses user-provided OpenRouter credentials.
package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"durooma/internal/models"
)

// Item is a single transaction to classify.
type Item struct {
	Desc   string
	Amount float64
}

// CategoryDef is a category name plus its description, used as LLM context.
type CategoryDef struct {
	Name        string
	Description string
}

// Provider is the swappable LLM backend. Implementations return one category
// name per item, in order. Swap Gemini for OpenAI/Claude by adding a Provider.
type Provider interface {
	Name() string
	// Classify returns len(items) category names, aligned to items.
	Classify(ctx context.Context, items []Item, categories []CategoryDef) ([]string, error)
}

// storeIface is the subset of *store.Store the categorization service needs.
type storeIface interface {
	ListCategories(ctx context.Context) ([]models.Category, error)
	ListRules(ctx context.Context) ([]models.Rule, error)
	SetUncategorizedCategory(ctx context.Context, id int64, categoryID *int64) error
	PendingTransactions(ctx context.Context, ids []int64) ([]models.Transaction, error)
}

type Service struct {
	store         storeIface
	provider      Provider
	batchSize     int
	gate          chan struct{}
	configuration func(context.Context) (RunConfig, error)
	cancelMu      sync.Mutex
	activeCancel  context.CancelFunc
	resetting     bool
	generation    uint64
}

func NewService(store storeIface, provider Provider) *Service {
	return NewServiceWithBatchSize(store, provider, itemsPerPrompt)
}

// NewServiceWithBatchSize configures the number of transactions per model request.
func NewServiceWithBatchSize(store storeIface, provider Provider, batchSize int) *Service {
	return &Service{store: store, provider: provider, batchSize: max(1, batchSize), gate: make(chan struct{}, 1)}
}

// RunConfig is resolved after acquiring the run lock, so queued work never
// uses a stale credential requirement or concurrency setting.
type RunConfig struct {
	BatchSize, Concurrency int
	Model                  string
}

func NewConfiguredService(st storeIface, p Provider, load func(context.Context) (RunConfig, error)) *Service {
	s := NewService(st, p)
	s.configuration = load
	return s
}
func (s *Service) runConfig(ctx context.Context) (RunConfig, error) {
	c := RunConfig{BatchSize: s.batchSize, Concurrency: 1}
	var err error
	if s.configuration != nil {
		c, err = s.configuration(ctx)
	}
	if err == nil && (c.BatchSize < 1 || c.BatchSize > 100 || c.Concurrency < 1 || c.Concurrency > 32) {
		err = errors.New("invalid categorization settings")
	}
	return c, err
}
func (s *Service) CancelActive() {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	if s.activeCancel != nil {
		s.activeCancel()
	}
}

// Reset stops active work, rejects queued work from the previous generation,
// and holds the run gate until the caller's atomic database reset is complete.
func (s *Service) Reset(ctx context.Context, reset func(context.Context) error) error {
	s.cancelMu.Lock()
	if s.resetting {
		s.cancelMu.Unlock()
		return errors.New("categorization reset already in progress")
	}
	s.resetting = true
	s.generation++
	if s.activeCancel != nil {
		s.activeCancel()
	}
	s.cancelMu.Unlock()
	defer func() { s.cancelMu.Lock(); s.resetting = false; s.cancelMu.Unlock() }()
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return reset(ctx)
}

// Report summarises a categorization run. Total and the counts after it are
// about categories; the Period counts are about the transactions whose
// reporting period was still undecided.
type Report struct {
	Total      int
	ByRules    int
	ByAI       int
	Unresolved int
	Provider   string

	Periods           int // transactions whose period was undecided
	PeriodsAssigned   int // decided by the provider, including "its own month"
	PeriodsMoved      int // of those, moved away from the transaction's own month
	PeriodsUnresolved int
}

// Progress is emitted as a run advances so callers can show a progress bar.
// Done counts decisions already resolved one way or another (a category or a
// period assigned, or given up on); it reaches Total on a run that is allowed
// to finish.
type Progress struct {
	Done  int
	Total int
}

// ProviderName reports the configured backend, for display before a run has
// produced a Report.
func (s *Service) ProviderName() string { return s.provider.Name() }

const itemsPerPrompt = 15

// Categorize applies rules first, then the AI provider, to the supplied
// transactions, writing results back. When the provider supports it, it then
// suggests the month, quarter or year each transaction counts towards.
// Categories and periods that are already set are skipped so existing
// (possibly hand-picked) choices are never overwritten. Every transaction handed in is processed: bounding the size of a
// run is the caller's job, since only the caller knows how it selected them.
func (s *Service) Categorize(ctx context.Context, txns []models.Transaction) (Report, error) {
	return s.CategorizeWithProgress(ctx, txns, nil)
}

// CategorizeWithProgress is Categorize with a callback invoked as the run
// advances, and honouring cancellation of ctx between units of work. A
// cancelled run returns what it managed to finish alongside ctx.Err(); writes
// already committed are kept, so aborting is safe rather than destructive.
// onProgress may be nil, and is called from the calling goroutine.
func (s *Service) CategorizeWithProgress(ctx context.Context, txns []models.Transaction, onProgress func(Progress)) (Report, error) {
	return s.categorizeWithProgress(ctx, txns, onProgress, nil)
}

func (s *Service) categorizeWithProgress(ctx context.Context, txns []models.Transaction, onProgress func(Progress), finish func(context.Context)) (Report, error) {
	rep := Report{Provider: s.provider.Name()}
	s.cancelMu.Lock()
	generation := s.generation
	resetting := s.resetting
	s.cancelMu.Unlock()
	if resetting {
		return rep, context.Canceled
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		return rep, ctx.Err()
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancelMu.Lock()
	if s.resetting || generation != s.generation {
		s.cancelMu.Unlock()
		cancel()
		return rep, context.Canceled
	}
	s.activeCancel = cancel
	s.cancelMu.Unlock()
	defer func() { cancel(); s.cancelMu.Lock(); s.activeCancel = nil; s.cancelMu.Unlock() }()
	// Retry bookkeeping must finish under the same gate as label writes, so a
	// completed old batch cannot restore retry delays after a reset.
	if finish != nil {
		defer func() {
			if ctx.Err() == nil {
				finish(ctx)
			}
		}()
	}
	options, err := s.runConfig(ctx)
	if err != nil {
		return rep, err
	}
	if options.Model != "" {
		rep.Provider = "openrouter/" + options.Model
	}
	ids := make([]int64, 0, len(txns))
	for _, t := range txns {
		if needsWork(t) {
			ids = append(ids, t.ID)
		}
	}
	txns, err = s.store.PendingTransactions(ctx, ids)
	if err != nil {
		return rep, err
	}

	// Never touch a category or period that is already set: the refreshed
	// snapshot says which half of the work each transaction still needs.
	periods, _ := s.provider.(PeriodProvider)
	var needCategory, needPeriod []models.Transaction
	for _, t := range txns {
		if t.CategoryID == nil {
			needCategory = append(needCategory, t)
		}
		if periods != nil && t.PeriodSource == "" {
			needPeriod = append(needPeriod, t)
		}
	}
	rep.Total, rep.Periods = len(needCategory), len(needPeriod)
	done := 0
	progress := func() {
		if onProgress != nil {
			onProgress(Progress{Done: done, Total: rep.Total + rep.Periods})
		}
	}
	progress() // publish the denominator before the first slow call

	// Categories first, so the period pass can use the ones just assigned.
	assigned := map[int64]string{}
	if len(needCategory) > 0 {
		if err := s.assignCategories(ctx, cancel, options, needCategory, &rep, &done, progress, assigned); err != nil {
			return rep, err
		}
	}
	if len(needPeriod) > 0 {
		if err := s.assignPeriods(ctx, cancel, options, periods, needPeriod, &rep, &done, progress, assigned); err != nil {
			return rep, err
		}
	}
	return rep, ctx.Err()
}

// needsWork reports whether a run has anything left to decide for t.
func needsWork(t models.Transaction) bool { return t.CategoryID == nil || t.PeriodSource == "" }

// assignCategories applies rules, then the provider, to uncategorized
// transactions, recording each assigned category's name in assigned.
func (s *Service) assignCategories(ctx context.Context, cancel context.CancelFunc, options RunConfig, txns []models.Transaction,
	rep *Report, done *int, progress func(), assigned map[int64]string) error {
	if tracker, ok := s.store.(interface{ BeginCategorization(context.Context) error }); ok {
		if err := tracker.BeginCategorization(ctx); err != nil {
			return err
		}
	}

	cats, err := s.store.ListCategories(ctx)
	if err != nil {
		return err
	}
	validName := map[string]int64{}
	nameByID := map[int64]string{}
	var defs []CategoryDef
	fallback := int64(0)
	for _, c := range cats {
		validName[strings.ToLower(c.Name)] = c.ID
		nameByID[c.ID] = c.Name
		if strings.EqualFold(c.Name, "General") {
			fallback = c.ID
		}
		desc := c.Name
		if c.Description != "" {
			desc = c.Name + ": " + c.Description
		}
		defs = append(defs, CategoryDef{Name: c.Name, Description: desc})
	}

	rules, err := s.store.ListRules(ctx)
	if err != nil {
		return err
	}

	// Pass 1: substring rules.
	var remaining []models.Transaction
	for _, t := range txns {
		if err := ctx.Err(); err != nil {
			return err
		}
		matched := false
		lower := strings.ToLower(t.Description)
		for _, r := range rules {
			if r.Pattern != "" && strings.Contains(lower, strings.ToLower(r.Pattern)) {
				cid := r.CategoryID
				if err := s.store.SetUncategorizedCategory(ctx, t.ID, &cid); err != nil {
					return err
				}
				assigned[t.ID] = nameByID[cid]
				rep.ByRules++
				*done++
				matched = true
				break
			}
		}
		if !matched {
			remaining = append(remaining, t)
		}
	}
	progress()

	// Pass 2: the provider.
	return runBatches(ctx, cancel, options, remaining,
		func(ctx context.Context, batch []models.Transaction) ([]string, error) {
			items := make([]Item, len(batch))
			for i, t := range batch {
				items[i] = Item{Desc: t.Description, Amount: t.BaseAmount}
			}
			return s.provider.Classify(ctx, items, defs)
		},
		func(batch []models.Transaction, names []string, err error) error {
			if err != nil {
				rep.Unresolved += len(batch)
				*done += len(batch)
				progress()
				return nil
			}
			for j, t := range batch {
				name := ""
				if j < len(names) {
					name = strings.TrimSpace(names[j])
				}
				cid, ok := validName[strings.ToLower(name)]
				if !ok {
					if fallback == 0 {
						rep.Unresolved++
						continue
					}
					cid = fallback
				}
				c := cid
				if err := s.store.SetUncategorizedCategory(ctx, t.ID, &c); err != nil {
					return err
				}
				assigned[t.ID] = nameByID[cid]
				rep.ByAI++
			}
			*done += len(batch)
			progress()
			return nil
		})
}

// assignPeriods asks the provider which month, quarter or year each
// transaction counts towards. An answer outside the vocabulary falls back to
// the transaction's own month, which is also what nearly every answer will
// be; either way the period counts as decided and leaves the queue.
func (s *Service) assignPeriods(ctx context.Context, cancel context.CancelFunc, options RunConfig, periods PeriodProvider,
	txns []models.Transaction, rep *Report, done *int, progress func(), assigned map[int64]string) error {
	store, ok := s.store.(periodStore)
	if !ok {
		return nil
	}
	return runBatches(ctx, cancel, options, txns,
		func(ctx context.Context, batch []models.Transaction) ([]string, error) {
			items := make([]PeriodItem, len(batch))
			for i, t := range batch {
				category := t.CategoryName
				if name, ok := assigned[t.ID]; ok {
					category = name
				}
				items[i] = PeriodItem{Date: t.Date, Desc: t.Description, Amount: t.BaseAmount, Category: category}
			}
			return periods.ClassifyPeriods(ctx, items)
		},
		func(batch []models.Transaction, labels []string, err error) error {
			if err != nil {
				rep.PeriodsUnresolved += len(batch)
				*done += len(batch)
				progress()
				return nil
			}
			for j, t := range batch {
				label := ""
				if j < len(labels) {
					label = labels[j]
				}
				start, end, ok := PeriodWindow(t.Date, label)
				if !ok {
					start, end, _ = PeriodWindow(t.Date, "month")
				}
				if err := store.SetUndecidedPeriod(ctx, t.ID, start, end); err != nil {
					return err
				}
				rep.PeriodsAssigned++
				if own, _, _ := PeriodWindow(t.Date, "month"); !start.Equal(own) || !end.Equal(own) {
					rep.PeriodsMoved++
				}
			}
			*done += len(batch)
			progress()
			return nil
		})
}

// periodStore is the store half of period suggestions. It is optional so
// category-only test stores need not implement it.
type periodStore interface {
	SetUndecidedPeriod(ctx context.Context, id int64, start, end time.Time) error
}

// runBatches sends txns to classify in batches of options.BatchSize, with up
// to options.Concurrency calls in flight, and hands each answer to apply. apply
// runs on the calling goroutine only, so it may update counters and publish
// progress freely. A failed call reaches apply with its error, except failures
// no later call can recover from (a missing or rejected key, changed settings),
// which stop the run, as does an error returned by apply.
func runBatches(ctx context.Context, cancel context.CancelFunc, options RunConfig, txns []models.Transaction,
	classify func(context.Context, []models.Transaction) ([]string, error),
	apply func(batch []models.Transaction, answers []string, err error) error) error {
	type response struct {
		batch   []models.Transaction
		answers []string
		err     error
		applied chan struct{}
	}
	jobs := make(chan []models.Transaction)
	results := make(chan response)
	var workers sync.WaitGroup
	for n := 0; n < min(options.Concurrency, (len(txns)+options.BatchSize-1)/options.BatchSize); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for batch := range jobs {
				answers, err := classify(ctx, batch)
				applied := make(chan struct{})
				results <- response{batch, answers, err, applied}
				<-applied
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < len(txns); i += options.BatchSize {
			select {
			case <-ctx.Done():
				return
			case jobs <- txns[i:min(i+options.BatchSize, len(txns))]:
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	var runErr error
	for result := range results {
		func() {
			defer close(result.applied)
			if ctx.Err() != nil {
				if runErr == nil {
					runErr = ctx.Err()
				}
				return
			}
			var apiErr *APIError
			rejected := errors.As(result.err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 402 || apiErr.Status == 403)
			if errors.Is(result.err, ErrAPIKeyRequired) || errors.Is(result.err, ErrSettingsChanged) || rejected {
				runErr = result.err
				cancel()
				return
			}
			if err := apply(result.batch, result.answers, result.err); err != nil {
				runErr = err
				cancel()
			}
		}()
	}
	if runErr != nil {
		return runErr
	}
	return ctx.Err()
}
