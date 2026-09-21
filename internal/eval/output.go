package eval

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// WriteReport replaces checkpoints atomically. Outputs contain transaction data
// and are private to the current user, just like the source dataset should be.
func WriteReport(dir string, r Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := writeAtomic(dir, "results.json", append(b, '\n')); err != nil {
		return err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"model", "prompt", "batch_size", "complete", "planned", "attempted", "evaluated", "excluded", "coverage", "accuracy", "macro_f1", "error_rate", "requests", "failed_requests", "mean_request_ms", "p95_request_ms", "request_ms_per_transaction", "attempts", "retries", "transient_failures", "recovered_batches", "excluded_batches", "exhausted_batches", "input_tokens", "output_tokens", "total_tokens", "reasoning_tokens", "cached_tokens", "cache_write_tokens", "cost_usd", "token_reported_attempts", "cost_reported_attempts"})
	for _, result := range r.Results {
		m := result.Metrics
		_ = w.Write([]string{result.Model, result.Prompt, strconv.Itoa(result.BatchSize), strconv.FormatBool(result.Complete), strconv.Itoa(result.Planned), strconv.Itoa(m.Attempted), strconv.Itoa(m.Evaluated), strconv.Itoa(m.Excluded), fmt.Sprintf("%.6f", ratio(m.Evaluated, result.Planned)), scoreCell(m.Accuracy, m), scoreCell(m.MacroF1, m), scoreCell(m.ErrorRate, m), strconv.Itoa(m.Requests), strconv.Itoa(m.FailedRequests), fmt.Sprintf("%.3f", m.MeanLatencyMS), fmt.Sprintf("%.3f", m.P95LatencyMS), fmt.Sprintf("%.3f", m.MSPerTransaction), strconv.Itoa(result.Requests.Attempts), strconv.Itoa(result.Requests.Retries), strconv.Itoa(result.Requests.TransientFailures), strconv.Itoa(result.Requests.RecoveredBatches), strconv.Itoa(result.Requests.ExcludedBatches), strconv.Itoa(result.Requests.ExhaustedBatches), usageCell(result.Usage.InputTokens), usageCell(result.Usage.OutputTokens), usageCell(result.Usage.TotalTokens), usageCell(result.Usage.ReasoningTokens), usageCell(result.Usage.CachedTokens), usageCell(result.Usage.CacheWriteTokens), usageCell(result.Usage.CostUSD), strconv.Itoa(result.Usage.TokenReportedAttempts), strconv.Itoa(result.Usage.CostReportedAttempts)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if err := writeAtomic(dir, "summary.csv", buf.Bytes()); err != nil {
		return err
	}
	buf.Reset()
	w = csv.NewWriter(&buf)
	_ = w.Write([]string{"model", "prompt", "batch_size", "complete", "slice_type", "slice", "attempted", "evaluated", "excluded", "coverage", "accuracy", "macro_f1", "error_rate"})
	for _, result := range r.Results {
		for _, group := range []struct {
			name   string
			values map[string]Metrics
		}{{"source", result.Sources}, {"confidence", result.ConfidenceScores}} {
			keys := make([]string, 0, len(group.values))
			for key := range group.values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				m := group.values[key]
				_ = w.Write([]string{result.Model, result.Prompt, strconv.Itoa(result.BatchSize), strconv.FormatBool(result.Complete), group.name, key, strconv.Itoa(m.Attempted), strconv.Itoa(m.Evaluated), strconv.Itoa(m.Excluded), fmt.Sprintf("%.6f", m.Coverage), scoreCell(m.Accuracy, m), scoreCell(m.MacroF1, m), scoreCell(m.ErrorRate, m)})
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if err := writeAtomic(dir, "slices.csv", buf.Bytes()); err != nil {
		return err
	}
	buf.Reset()
	w = csv.NewWriter(&buf)
	_ = w.Write([]string{"model", "prompt", "batch_size", "repeat", "batch", "id", "source", "confidence", "reason"})
	for _, v := range r.Results {
		for _, p := range v.Predictions {
			if p.Excluded {
				_ = w.Write([]string{v.Model, v.Prompt, strconv.Itoa(v.BatchSize), strconv.Itoa(p.Repeat), strconv.Itoa(p.Batch), p.ID, p.Source, p.Confidence, p.Error})
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if err := writeAtomic(dir, "excluded.csv", buf.Bytes()); err != nil {
		return err
	}
	buf.Reset()
	w = csv.NewWriter(&buf)
	_ = w.Write([]string{"model", "prompt", "batch_size", "repeat", "batch", "attempt", "http_status", "retryable", "error", "batch_excluded", "retry_exhausted"})
	for _, v := range r.Results {
		for _, b := range v.Batches {
			for _, a := range b.Attempts {
				if a.Error != "" {
					_ = w.Write([]string{v.Model, v.Prompt, strconv.Itoa(v.BatchSize), strconv.Itoa(b.Repeat), strconv.Itoa(b.Batch), strconv.Itoa(a.Number), strconv.Itoa(a.Status), strconv.FormatBool(a.Retryable), a.Error, strconv.FormatBool(b.Excluded), strconv.FormatBool(b.RetryExhausted)})
				}
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return writeAtomic(dir, "request_errors.csv", buf.Bytes())
}

func writeAtomic(dir, name string, data []byte) error {
	f, err := os.CreateTemp(dir, ".eval-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}

func scoreCell(value float64, m Metrics) string {
	if m.Evaluated == 0 {
		return ""
	}
	return fmt.Sprintf("%.6f", value)
}

// Missing usage remains blank; explicitly reported zero remains zero.
func usageCell[T int64 | float64](value *T) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(*value)
}
