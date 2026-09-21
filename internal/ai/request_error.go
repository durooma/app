package ai

import (
	"net/http"
	"strconv"
	"time"
)

// NetworkError distinguishes transport/envelope failures from invalid model
// output. Kind is an adapter-owned description, never the raw request or body.
type NetworkError struct{ Kind string }

func (e *NetworkError) Error() string { return e.Kind }

func retryAfter(header string) time.Duration {
	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		// Avoid overflow on an untrusted header. Long waits remain cancellable.
		if seconds <= 0 {
			return 0
		}
		if seconds > int64((1<<63-1)/time.Second) {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(header); err == nil {
		return max(0, time.Until(date))
	}
	return 0
}
