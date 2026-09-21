package eval

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"durooma/internal/ai"
)

type Prediction struct {
	ID         string `json:"id"`
	Source     string `json:"source,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Repeat     int    `json:"repeat"`
	Batch      int    `json:"batch"`
	Expected   string `json:"expected"`
	Raw        string `json:"raw_prediction"`
	Predicted  string `json:"predicted"`
	Correct    bool   `json:"correct"`
	Error      string `json:"error,omitempty"`
	Excluded   bool   `json:"excluded,omitempty"`
}

type Batch struct {
	Attempts       []Attempt `json:"attempts"`
	Excluded       bool      `json:"excluded,omitempty"`
	RetryExhausted bool      `json:"retry_exhausted,omitempty"`
	ElapsedMS      float64   `json:"elapsed_ms"`
	Repeat         int       `json:"repeat"`
	Batch          int       `json:"batch"`
	Size           int       `json:"size"`
	LatencyMS      float64   `json:"latency_ms"`
	Error          string    `json:"error,omitempty"`
}

type CategoryScore struct {
	Category  string  `json:"category"`
	Support   int     `json:"support"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

type Metrics struct {
	Attempted        int                       `json:"attempted"`
	Excluded         int                       `json:"excluded"`
	Coverage         float64                   `json:"coverage"`
	ScoreAvailable   bool                      `json:"score_available"`
	Evaluated        int                       `json:"evaluated"`
	Correct          int                       `json:"correct"`
	Errors           int                       `json:"errors"`
	Accuracy         float64                   `json:"accuracy"`
	MacroF1          float64                   `json:"macro_f1"`
	ErrorRate        float64                   `json:"error_rate"`
	Requests         int                       `json:"requests"`
	FailedRequests   int                       `json:"failed_requests"`
	MeanLatencyMS    float64                   `json:"mean_request_latency_ms"`
	P95LatencyMS     float64                   `json:"p95_request_latency_ms"`
	MSPerTransaction float64                   `json:"request_ms_per_transaction"`
	Categories       []CategoryScore           `json:"categories"`
	Confusion        map[string]map[string]int `json:"confusion"`
}

type Result struct {
	Usage            UsageTotals        `json:"usage"`
	Model            string             `json:"model"`
	Prompt           string             `json:"prompt"`
	BatchSize        int                `json:"batch_size"`
	Planned          int                `json:"planned"`
	Complete         bool               `json:"complete"`
	Requests         RequestStats       `json:"request_stats"`
	Metrics          Metrics            `json:"metrics"`
	Sources          map[string]Metrics `json:"sources"`
	ConfidenceScores map[string]Metrics `json:"confidence_scores"`
	Batches          []Batch            `json:"batches"`
	Predictions      []Prediction       `json:"predictions"`
}

type Report struct {
	Usage                 UsageTotals `json:"usage"`
	Version               int         `json:"version"`
	WallSeconds           float64     `json:"wall_seconds"`
	TransactionsPerSecond float64     `json:"transactions_per_second"`
	StartedAt             time.Time   `json:"started_at"`
	DatasetSHA256         string      `json:"dataset_sha256"`
	Dataset               Dataset     `json:"dataset"`
	Config                Config      `json:"config"`
	Results               []Result    `json:"results"`
}

// Factory allows more providers without coupling the scoring loop to an API.
type Factory func(Model, Prompt) (ai.Provider, error)

func NewProvider(m Model, p Prompt) (ai.Provider, error) {
	key := os.Getenv(m.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf("model %q: environment variable %s is empty", m.Name, m.APIKeyEnv)
	}
	switch m.Provider {
	case "gemini":
		return ai.NewGeminiWithPrompt(key, m.Model, p.Template), nil
	case "openrouter":
		return ai.NewOpenRouterWithRouting(key, m.Model, p.Template, m.Routing), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", m.Provider)
	}
}

// RequestCount counts initial batch calls. Retries add attempts up to the configured limit.
func RequestCount(c Config, examples int) int {
	n := 0
	for _, size := range c.BatchSizes {
		n += (examples + size - 1) / size
	}
	return n * len(c.Models) * len(c.Prompts) * c.Repeats
}

// Run shares a bounded worker pool across all variants. Providers must support
// concurrent calls. Only this goroutine mutates the report or checkpoints it.
func Run(ctx context.Context, c Config, d Dataset, hash string, factory Factory, checkpoint func(Report) error) (Report, error) {
	c.Retry = c.Retry.defaults()
	report := Report{Version: 4, StartedAt: time.Now().UTC(), DatasetSHA256: hash, Dataset: d, Config: c}
	if err := c.Validate(); err != nil {
		return report, err
	}
	if err := d.Validate(); err != nil {
		return report, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type job struct {
		variant, repeat, batch int
		indices                []int
		provider               ai.Provider
	}
	type outcome struct {
		job         job
		batch       Batch
		predictions []Prediction
	}
	var jobs []job
	var variants []Result
	var pending []int
	orders := make([][]int, c.Repeats)
	for r := range orders {
		orders[r] = rand.New(rand.NewSource(c.Seed + int64(r))).Perm(len(d.Examples))
	}
	// Preflight every provider before launching any paid work.
	for _, m := range c.Models {
		for _, p := range c.Prompts {
			provider, err := factory(m, p)
			if err != nil {
				return report, err
			}
			for _, size := range c.BatchSizes {
				v := len(variants)
				variants = append(variants, Result{Model: m.Name, Prompt: p.Name, BatchSize: size, Planned: len(d.Examples) * c.Repeats})
				n := 0
				for r, order := range orders {
					for start := 0; start < len(order); start += size {
						jobs = append(jobs, job{v, r + 1, start/size + 1, order[start:min(start+size, len(order))], provider})
						n++
					}
				}
				pending = append(pending, n)
			}
		}
	}
	valid := map[string]string{}
	for _, cat := range d.Categories {
		valid[normalize(cat.Name)] = cat.Name
	}
	defs := d.Definitions()
	timeout, _ := time.ParseDuration(c.Timeout)
	interval, _ := time.ParseDuration(c.RequestInterval)
	queue := make(chan job)
	outcomes := make(chan outcome)
	var workers sync.WaitGroup
	gate := &requestGate{interval: interval}
	beganRun := time.Now()
	for w := 0; w < min(c.Workers(), len(jobs)); w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range queue {
				items := make([]ai.Item, len(j.indices))
				for i, index := range j.indices {
					ex := d.Examples[index]
					items[i] = ai.Item{Desc: ex.Description, Amount: ex.Amount}
				}
				names, b, err := classifyWithRetry(ctx, j.provider, items, defs, timeout, c.Retry, gate)
				if len(b.Attempts) == 0 {
					return
				} // Never count work canceled before its first attempt.
				b.Repeat, b.Batch = j.repeat, j.batch
				o := outcome{job: j, batch: b}
				for i, index := range j.indices {
					ex := d.Examples[index]
					pred := Prediction{ID: ex.ID, Repeat: j.repeat, Batch: j.batch, Expected: valid[normalize(ex.Expected)], Source: ex.Source, Confidence: ex.Confidence, Excluded: b.Excluded}
					if err != nil {
						pred.Error = b.Error
					} else {
						pred.Raw = names[i]
						pred.Predicted = valid[normalize(names[i])]
						if pred.Predicted == "" {
							pred.Error = "unknown category"
						}
						pred.Correct = pred.Predicted == pred.Expected
					}
					o.predictions = append(o.predictions, pred)
				}
				outcomes <- o // Always drain attempted work, even on cancellation.
			}
		}()
	}
	go func() {
		defer close(queue)
		for _, j := range jobs {
			select {
			case <-ctx.Done():
				return
			case queue <- j:
			}
		}
	}()
	go func() { workers.Wait(); close(outcomes) }()
	var saveErr error
	saved := make([]bool, len(variants))
	updateTiming := func() {
		report.WallSeconds = time.Since(beganRun).Seconds()
		total := 0
		report.Usage = UsageTotals{}
		for _, v := range report.Results {
			report.Usage.addBatches(v.Batches)
			total += v.Metrics.Evaluated
		}
		if report.WallSeconds > 0 {
			report.TransactionsPerSecond = float64(total) / report.WallSeconds
		}
	}
	finish := func(v int) {
		r := &variants[v]
		// Restore shuffled input order independent of response completion order.
		sort.SliceStable(r.Batches, func(i, j int) bool {
			a, b := r.Batches[i], r.Batches[j]
			if a.Repeat != b.Repeat {
				return a.Repeat < b.Repeat
			}
			return a.Batch < b.Batch
		})
		sort.SliceStable(r.Predictions, func(i, j int) bool {
			a, b := r.Predictions[i], r.Predictions[j]
			if a.Repeat != b.Repeat {
				return a.Repeat < b.Repeat
			}
			return a.Batch < b.Batch
		})
		r.Complete = pending[v] == 0 && ctx.Err() == nil
		r.Requests = requestStats(r.Batches)
		r.Usage.addBatches(r.Batches)
		r.Metrics = score(r.Predictions, r.Batches, d.Categories)
		r.Sources = scoreGroups(r.Predictions, d.Categories, func(p Prediction) string { return p.Source })
		r.ConfidenceScores = scoreGroups(r.Predictions, d.Categories, func(p Prediction) string { return p.Confidence })
		report.Results = append(report.Results, *r)
		saved[v] = true
		updateTiming()
		if checkpoint != nil && saveErr == nil {
			if err := checkpoint(report); err != nil {
				saveErr = err
				cancel()
			}
		}
	}
	for o := range outcomes {
		v := o.job.variant
		variants[v].Batches = append(variants[v].Batches, o.batch)
		variants[v].Predictions = append(variants[v].Predictions, o.predictions...)
		pending[v]--
		if pending[v] == 0 {
			finish(v)
		}
	}
	for v := range variants {
		if !saved[v] && len(variants[v].Batches) > 0 {
			finish(v)
		}
	}
	updateTiming()
	if saveErr != nil {
		return report, saveErr
	}
	return report, ctx.Err()
}

func scoreGroups(predictions []Prediction, categories []Category, key func(Prediction) string) map[string]Metrics {
	groups := map[string][]Prediction{}
	for _, p := range predictions {
		k := key(p)
		if k == "" {
			k = "unspecified"
		}
		groups[k] = append(groups[k], p)
	}
	scores := map[string]Metrics{}
	for k, ps := range groups {
		scores[k] = score(ps, nil, categories)
	}
	return scores
}

func wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func score(predictions []Prediction, batches []Batch, categories []Category) Metrics {
	m := Metrics{Attempted: len(predictions), Requests: len(batches), Confusion: map[string]map[string]int{}}
	support, predicted, correct := map[string]int{}, map[string]int{}, map[string]int{}
	for _, p := range predictions {
		if p.Excluded {
			m.Excluded++
			continue
		}
		m.Evaluated++
		support[p.Expected]++
		predicted[p.Predicted]++
		if p.Correct {
			m.Correct++
			correct[p.Expected]++
		}
		if p.Error != "" {
			m.Errors++
		}
		if m.Confusion[p.Expected] == nil {
			m.Confusion[p.Expected] = map[string]int{}
		}
		// Empty predicted category is reserved for failed/invalid responses.
		m.Confusion[p.Expected][p.Predicted]++
	}
	m.Coverage = ratio(m.Evaluated, m.Attempted)
	m.ScoreAvailable = m.Evaluated > 0
	m.Accuracy, m.ErrorRate = ratio(m.Correct, m.Evaluated), ratio(m.Errors, m.Evaluated)
	supported := 0
	for _, c := range categories {
		cs := CategoryScore{Category: c.Name, Support: support[c.Name], Precision: ratio(correct[c.Name], predicted[c.Name]), Recall: ratio(correct[c.Name], support[c.Name])}
		cs.F1 = ratio(2*correct[c.Name], support[c.Name]+predicted[c.Name])
		m.Categories = append(m.Categories, cs)
		if cs.Support > 0 {
			m.MacroF1 += cs.F1
			supported++
		}
	}
	if supported > 0 {
		m.MacroF1 /= float64(supported)
	}
	var latencies []float64
	for _, b := range batches {
		if len(b.Attempts) == 0 {
			latencies = append(latencies, b.LatencyMS)
			m.MeanLatencyMS += b.LatencyMS
		} else {
			for _, a := range b.Attempts {
				latencies = append(latencies, a.LatencyMS)
				m.MeanLatencyMS += a.LatencyMS
			}
		}
		if b.Error != "" {
			m.FailedRequests++
		}
	}
	if m.Attempted > 0 {
		m.MSPerTransaction = m.MeanLatencyMS / float64(m.Attempted)
	}
	if len(latencies) > 0 {
		m.MeanLatencyMS /= float64(len(latencies))
		sort.Float64s(latencies)
		m.P95LatencyMS = latencies[(95*len(latencies)+99)/100-1]
	}
	return m
}

// RequestStats keeps transport reliability separate from classification scores.
type RequestStats struct {
	Attempts          int `json:"attempts"`
	Retries           int `json:"retries"`
	TransientFailures int `json:"transient_failures"`
	RecoveredBatches  int `json:"recovered_batches"`
	ExcludedBatches   int `json:"excluded_batches"`
	ExhaustedBatches  int `json:"exhausted_batches"`
}

func requestStats(batches []Batch) RequestStats {
	var s RequestStats
	for _, b := range batches {
		s.Attempts += len(b.Attempts)
		s.Retries += max(0, len(b.Attempts)-1)
		for _, a := range b.Attempts {
			if a.Retryable {
				s.TransientFailures++
			}
		}
		if len(b.Attempts) > 1 && b.Error == "" {
			s.RecoveredBatches++
		}
		if b.Excluded {
			s.ExcludedBatches++
		}
		if b.RetryExhausted {
			s.ExhaustedBatches++
		}
	}
	return s
}
