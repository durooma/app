// Command eval compares categorization models, prompts, and batch sizes.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"durooma/internal/eval"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "evals/config.json", "matrix configuration JSON")
	datasetPath := flag.String("dataset", "", "single labeled dataset JSON (overrides suite)")
	suitePath := flag.String("suite", "evals/suite.json", "combined suite manifest")
	concurrency := flag.Int("concurrency", 0, "override concurrent request limit")
	bundle := flag.String("bundle", "", "write the resolved combined dataset to a new JSON file")
	out := flag.String("out", "", "new output directory (defaults to evals/runs/<timestamp>)")
	dry := flag.Bool("dry-run", false, "validate inputs and print planned requests without calling an API")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flag.Args())
	}
	c, err := eval.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if *concurrency != 0 {
		c.Concurrency = *concurrency
		if err := c.Validate(); err != nil {
			return err
		}
	}
	var d eval.Dataset
	var hash string
	if *datasetPath != "" {
		d, hash, err = eval.LoadDataset(*datasetPath)
	} else {
		d, hash, err = eval.LoadSuite(*suitePath)
	}
	if err != nil {
		return err
	}
	if *bundle != "" {
		b, e := json.MarshalIndent(d, "", "  ")
		if e != nil {
			return e
		}
		f, e := os.OpenFile(*bundle, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(append(b, '\n'))
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Printf("Workers: %d; request interval: %s; max attempts/batch: %d\n", c.Workers(), c.RequestInterval, c.Retry.MaxAttempts)
	fmt.Printf("Dataset: %s (%d examples, SHA256 %s)\n", d.Name, len(d.Examples), hash)
	fmt.Printf("Matrix: %d models × %d prompts × %d batch sizes × %d repeats; %d initial API requests (retries extra)\n", len(c.Models), len(c.Prompts), len(c.BatchSizes), c.Repeats, eval.RequestCount(c, len(d.Examples)))
	for _, m := range c.Models {
		for _, p := range c.Prompts {
			for _, size := range c.BatchSizes {
				fmt.Printf("  %s (%s/%s), prompt=%s, batch=%d: %d requests\n", m.Name, m.Provider, m.Model, p.Name, size, ((len(d.Examples)+size-1)/size)*c.Repeats)
			}
		}
	}
	if *dry {
		return nil
	}
	// Preflight credentials before creating an output directory.
	for _, m := range c.Models {
		if _, err := eval.NewProvider(m, c.Prompts[0]); err != nil {
			return err
		}
	}
	if *out == "" {
		*out = filepath.Join("evals", "runs", time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0700); err != nil {
		return err
	}
	// Refuse to overwrite a previous experiment.
	if err := os.Mkdir(*out, 0700); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	fmt.Printf("Results: %s\n", *out)
	report, err := eval.Run(ctx, c, d, hash, eval.NewProvider, func(r eval.Report) error {
		if err := eval.WriteReport(*out, r); err != nil {
			return err
		}
		last := r.Results[len(r.Results)-1]
		m := last.Metrics
		accuracy, f1 := "n/a", "n/a"
		if m.ScoreAvailable {
			accuracy = fmt.Sprintf("%.1f%%", 100*m.Accuracy)
			f1 = fmt.Sprintf("%.3f", m.MacroF1)
		}
		fmt.Printf("%s / %s / batch=%d: accuracy %s, macro F1 %s, scored %d/%d, excluded %d, model errors %d, retries %d, complete=%t\n", last.Model, last.Prompt, last.BatchSize, accuracy, f1, m.Evaluated, last.Planned, m.Excluded, m.Errors, last.Requests.Retries, last.Complete)
		fmt.Printf("  %s\n", last.Usage)
		return nil
	})
	if len(report.Results) > 0 {
		fmt.Printf("Run total — %s\n", report.Usage)
	}
	return err
}
