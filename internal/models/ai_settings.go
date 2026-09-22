package models

import (
	"errors"
	"regexp"
)

const DefaultAIModel = "deepseek/deepseek-v4.1-flash"

// AISettings is safe for rendering: it never contains a plaintext or encrypted key.
type AISettings struct {
	HasKey      bool
	Background  bool
	Model       string
	IntervalMS  int
	Concurrency int
	BatchSize   int
	Revision    int64
}

func DefaultAISettings() AISettings {
	return AISettings{Model: DefaultAIModel, IntervalMS: 250, Concurrency: 8, BatchSize: 15}
}

var modelSlug = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*/[a-zA-Z0-9][a-zA-Z0-9._:/-]*$`)

func (s AISettings) Validate() error {
	if len(s.Model) > 200 || !modelSlug.MatchString(s.Model) {
		return errors.New("enter an OpenRouter model ID, for example deepseek/deepseek-v4.1-flash")
	}
	if s.IntervalMS < 10 || s.IntervalMS > 86400000 {
		return errors.New("request spacing must be between 10 and 86400000 milliseconds")
	}
	if s.Concurrency < 1 || s.Concurrency > 32 {
		return errors.New("concurrent requests must be between 1 and 32")
	}
	if s.BatchSize < 1 || s.BatchSize > 100 {
		return errors.New("batch size must be between 1 and 100")
	}
	return nil
}
